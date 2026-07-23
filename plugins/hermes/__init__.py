"""Mimir capture plugin for Hermes.

Reports completed turns, heartbeats, and session ends to the Mimir session
object. Capture happens above provider transport, so providers the Mimir
proxy cannot reach (Nous portal account, direct providers) are covered.

Two modes, decided once at startup:

- Full mode: turn + heartbeat + end events. Used when Hermes talks to
  providers directly.
- Liveness-only mode: heartbeat + end events, no turn events. Used when the
  Mimir-managed OpenRouter redirect is active (OPENROUTER_BASE_URL points at
  the Mimir Worker), because the proxy already captures those turns with
  token usage and full exchange archives; reporting them here would double
  count the session.

Install: copy this directory to the plugins directory under your Hermes home
(~/.hermes/plugins/ or %LOCALAPPDATA%/hermes/plugins on Windows). Uninstall:
delete the directory.

No credentials live here. Connection resolves from, in order:
  1. MIMIR_URL + MIMIR_TOKEN environment variables
  2. $MIMIR_HOME/config + $MIMIR_HOME/token
  3. ~/.mimir/config + ~/.mimir/token (written by `mimir setup`/`mimir login`)

Delivery is best-effort and never blocks or raises into Hermes. Session ends
are reported by on_session_end/on_session_reset/on_session_finalize; if the
process dies first, the server-side silence timer finalizes the session
within ~10 minutes regardless.
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


def build_turn_event(session_id, model, user_message, repo):
    return {
        "version": 1,
        "kind": "turn",
        "session_id": session_id,
        "harness": "hermes",
        "repo": repo,
        "ts": _now(),
        "turn": {
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


def liveness_only(env, connection_url):
    """True when the Mimir-managed OpenRouter redirect is active."""
    base = (env.get("OPENROUTER_BASE_URL") or "").strip()
    return bool(base and connection_url and connection_url in base)


class _Reporter:
    def __init__(self, connection, repo):
        self._connection = connection
        self._repo = repo
        self._reported = []
        self._reported_set = set()
        self._last_session = None
        self._last_activity = 0.0
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

    def _touch(self, session_id):
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
        if session_id:
            self._touch(session_id)
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
                pass
        except Exception:
            # Best-effort: capture must never interrupt the harness.
            pass

    def heartbeat_if_active(self):
        session_id = self.active_session()
        if session_id:
            self.post(build_simple_event("heartbeat", session_id, self._repo))


def register(ctx):
    connection = load_connection()
    if not connection:
        return
    try:
        repo = repo_name(os.getcwd())
    except Exception:
        repo = None
    reporter = _Reporter(connection, repo)
    turns_enabled = not liveness_only(os.environ, connection["url"])

    def heartbeat_loop():
        while True:
            threading.Event().wait(HEARTBEAT_SECONDS)
            reporter.heartbeat_if_active()

    threading.Thread(target=heartbeat_loop, daemon=True).start()

    def on_turn(session_id=None, turn_id=None, user_message=None, model=None, **_kwargs):
        if not turns_enabled or not session_id:
            return
        if turn_id and not reporter._dedup(f"turn:{turn_id}"):
            return
        reporter.post(build_turn_event(session_id, model, user_message, repo))

    def on_start(session_id=None, **_kwargs):
        if session_id:
            reporter.post(build_simple_event("heartbeat", session_id, repo))

    def on_end(reason):
        def handler(session_id=None, **_kwargs):
            if session_id:
                reporter.post(build_simple_event("end", session_id, repo, reason=reason))
        return handler

    ctx.register_hook("post_llm_call", on_turn)
    ctx.register_hook("on_session_start", on_start)
    ctx.register_hook("on_session_end", on_end("harness exit"))
    ctx.register_hook("on_session_reset", on_end("session reset"))
    ctx.register_hook("on_session_finalize", on_end("session finalized"))


# Test surface.
__testing = {
    "parse_mimir_config": parse_mimir_config,
    "resolve_connection": resolve_connection,
    "repo_name": repo_name,
    "build_turn_event": build_turn_event,
    "build_simple_event": build_simple_event,
    "liveness_only": liveness_only,
}
