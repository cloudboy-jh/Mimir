"""Mimir capture plugin for Hermes.

Reports completed turns, heartbeats, and session ends to the Mimir session
object. Capture happens above provider transport, so providers the Mimir
proxy cannot reach (Nous portal account, direct providers) are covered.

Each turn is classified from Hermes' pre_api_request transport metadata.
Turns routed through the Mimir OpenRouter redirect are liveness-only to avoid
double counting; direct-provider turns are reported in full.

Install: copy this directory to the plugins directory under your Hermes home
(~/.hermes/plugins/ or %LOCALAPPDATA%/hermes/plugins on Windows). Uninstall:
delete the directory.

No credentials live here. Connection resolves from, in order:
  1. MIMIR_URL + MIMIR_TOKEN environment variables
  2. $MIMIR_HOME/config + $MIMIR_HOME/token
  3. ~/.mimir/config + ~/.mimir/token (written by `mimir setup`/`mimir login`)

Delivery is best-effort and never blocks or raises into Hermes. Session ends
are reported by on_session_finalize; if the process dies first, the
server-side silence timer finalizes the session within ~10 minutes.
"""

import json
import os
import re
import threading
import time
import urllib.parse
import urllib.request
from datetime import datetime, timezone

HEARTBEAT_SECONDS = 60
ACTIVITY_WINDOW_SECONDS = 5 * 60
MAX_REPORTED_IDS = 1000

_UTC = timezone.utc


def _now() -> str:
    return datetime.now(_UTC).isoformat().replace("+00:00", "Z")


def parse_mimir_config(text: str) -> dict:
    match = re.search(r'^\s*url\s*=\s*"?([^"\n]+?)"?\s*$', text, re.MULTILINE)
    return {"url": match.group(1).rstrip("/")} if match else {}


def resolve_connection(env, read_file, home):
    """Return {"url", "token"} or None. Injected for tests."""
    env_url = (env.get("MIMIR_URL") or "").strip()
    env_token = (env.get("MIMIR_TOKEN") or "").strip()
    if env_url and env_token:
        return {"url": env_url.rstrip("/"), "token": env_token}
    directory = (env.get("MIMIR_HOME") or "").strip() or (os.path.join(home, ".mimir") if home else None)
    if not directory:
        return None
    config = read_file(os.path.join(directory, "config"))
    token = (read_file(os.path.join(directory, "token")) or "").strip()
    url = parse_mimir_config(config).get("url") if config else None
    return {"url": url, "token": token} if url and token else None


def _read_file(path):
    try:
        with open(path, "r", encoding="utf-8") as handle:
            return handle.read()
    except OSError:
        return None


def load_connection():
    home = None
    try:
        home = os.path.expanduser("~")
    except Exception:
        pass
    return resolve_connection(os.environ, _read_file, home)


def repo_name(directory):
    if not directory:
        return None
    parts = [part for part in re.split(r"[\\/]", directory.rstrip("/\\")) if part]
    return parts[-1] if parts else None


def build_turn_event(session_id, turn_id, model, user_message, repo):
    return {
        "version": 1,
        "kind": "turn",
        "session_id": session_id,
        "harness": "hermes",
        "repo": repo,
        "ts": _now(),
        "turn": {
            "exchange_id": turn_id if isinstance(turn_id, str) and turn_id else None,
            "model": model if isinstance(model, str) and model else None,
            "request_kind": "primary",
            "excerpt": (user_message or "")[:500] if isinstance(user_message, str) else None,
        },
    }


def build_simple_event(kind, session_id, repo, reason=None):
    event = {
        "version": 1,
        "kind": kind,
        "session_id": session_id,
        "harness": "hermes",
        "repo": repo,
        "ts": _now(),
    }
    if reason:
        event["reason"] = reason
    return event


def _uses_connection_url(base_url, connection_url):
    if not isinstance(base_url, str) or not base_url.strip() or not connection_url:
        return False
    try:
        target = urllib.parse.urlsplit(base_url.strip())
        connection = urllib.parse.urlsplit(connection_url.strip())
    except ValueError:
        return False
    if target.scheme.lower() != connection.scheme.lower() or target.netloc.lower() != connection.netloc.lower():
        return False
    root = connection.path.rstrip("/")
    path = target.path.rstrip("/")
    return path == root or path.startswith(root + "/")


def liveness_only(env, connection_url):
    """True when the Mimir-managed OpenRouter redirect is active."""
    base = (env.get("OPENROUTER_BASE_URL") or "").strip()
    return _uses_connection_url(base, connection_url)


def turn_uses_proxy(provider, base_url, env, connection_url):
    """True only when this turn used the Mimir-managed provider route."""
    if isinstance(base_url, str) and base_url.strip():
        return _uses_connection_url(base_url, connection_url)
    if isinstance(provider, str) and provider and provider.lower() != "openrouter":
        return False
    return liveness_only(env, connection_url)


class _Reporter:
    def __init__(self, connection, repo):
        self._connection = connection
        self._repo = repo
        self._reported = []
        self._reported_set = set()
        self._last_session = None
        self._last_activity = 0.0
        self._turn_routes = {}
        self._lock = threading.Lock()

    def _dedup(self, key):
        with self._lock:
            if key in self._reported_set:
                return False
            self._reported_set.add(key)
            self._reported.append(key)
            if len(self._reported) > MAX_REPORTED_IDS:
                self._reported_set.discard(self._reported.pop(0))
            return True

    def _forget(self, key):
        with self._lock:
            self._reported_set.discard(key)
            try:
                self._reported.remove(key)
            except ValueError:
                pass

    def touch(self, session_id):
        with self._lock:
            self._last_session = session_id
            self._last_activity = time.monotonic()

    def active_session(self):
        with self._lock:
            if self._last_session and time.monotonic() - self._last_activity < ACTIVITY_WINDOW_SECONDS:
                return self._last_session
            return None

    def post(self, event):
        session_id = event.get("session_id")
        url = f"{self._connection['url']}/sessions/{urllib.parse.quote(session_id or '', safe='')}/events"
        body = json.dumps(event).encode("utf-8")
        request = urllib.request.Request(
            url,
            data=body,
            headers={
                "authorization": f"Bearer {self._connection['token']}",
                "content-type": "application/json",
            },
            method="POST",
        )
        try:
            with urllib.request.urlopen(request, timeout=10):
                return True
        except Exception:
            # Best-effort: capture must never interrupt the harness.
            return False

    def _deliver_attempt(self, event, key, attempt, wait_on_exit):
        if self.post(event):
            return
        if attempt >= 4:
            if key:
                self._forget(key)
            return
        timer = threading.Timer(0.25 * (2 ** (attempt - 1)), self._deliver_attempt, args=(event, key, attempt + 1, wait_on_exit))
        timer.daemon = not wait_on_exit
        timer.start()

    def deliver(self, event, key=None, wait_on_exit=False):
        if key and not self._dedup(key):
            return
        worker = threading.Thread(target=self._deliver_attempt, args=(event, key, 1, wait_on_exit), daemon=not wait_on_exit)
        worker.start()

    def record_turn_route(self, session_id, turn_id, provider, base_url):
        if not session_id or not turn_id:
            return
        proxied = turn_uses_proxy(provider, base_url, os.environ, self._connection["url"])
        with self._lock:
            self._turn_routes[(session_id, turn_id)] = proxied

    def take_turn_route(self, session_id, turn_id):
        if not session_id or not turn_id:
            return None
        with self._lock:
            return self._turn_routes.pop((session_id, turn_id), None)

    def end(self, session_id, reason):
        with self._lock:
            if self._last_session == session_id:
                self._last_session = None
                self._last_activity = 0.0
        self.deliver(build_simple_event("end", session_id, self._repo, reason=reason), wait_on_exit=True)

    def heartbeat_if_active(self):
        session_id = self.active_session()
        if session_id:
            self.deliver(build_simple_event("heartbeat", session_id, self._repo))


def register(ctx):
    connection = load_connection()
    if not connection:
        return
    try:
        repo = repo_name(os.getcwd())
    except Exception:
        repo = None
    reporter = _Reporter(connection, repo)
    def heartbeat_loop():
        while True:
            threading.Event().wait(HEARTBEAT_SECONDS)
            reporter.heartbeat_if_active()

    threading.Thread(target=heartbeat_loop, daemon=True).start()

    def on_transport(session_id=None, turn_id=None, provider=None, base_url=None, **_kwargs):
        reporter.record_turn_route(session_id, turn_id, provider, base_url)

    def on_turn(session_id=None, turn_id=None, user_message=None, model=None, **_kwargs):
        if not session_id:
            return
        reporter.touch(session_id)
        proxied = reporter.take_turn_route(session_id, turn_id)
        if proxied is None:
            proxied = liveness_only(os.environ, connection["url"])
        if proxied:
            return
        key = f"turn:{turn_id}" if turn_id else None
        reporter.deliver(build_turn_event(session_id, turn_id, model, user_message, repo), key=key)

    def on_start(session_id=None, **_kwargs):
        if session_id:
            reporter.touch(session_id)
            reporter.deliver(build_simple_event("heartbeat", session_id, repo))

    def on_finalize(session_id=None, reason=None, **_kwargs):
        if session_id:
            reporter.end(session_id, reason or "session finalized")

    ctx.register_hook("pre_api_request", on_transport)
    ctx.register_hook("post_llm_call", on_turn)
    ctx.register_hook("on_session_start", on_start)
    ctx.register_hook("on_session_finalize", on_finalize)


# Test surface.
__testing = {
    "parse_mimir_config": parse_mimir_config,
    "resolve_connection": resolve_connection,
    "repo_name": repo_name,
    "build_turn_event": build_turn_event,
    "build_simple_event": build_simple_event,
    "uses_connection_url": _uses_connection_url,
    "liveness_only": liveness_only,
    "turn_uses_proxy": turn_uses_proxy,
}
