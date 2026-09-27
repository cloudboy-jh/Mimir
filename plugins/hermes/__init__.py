"""Mimir capture plugin for Hermes.

Reports Hermes-observed direct-provider API exchanges, heartbeats, and session
ends to Mimir. The Worker redacts exchanges before persistence. The managed
OpenRouter proxy remains the canonical exchange source.

Each request is classified from Hermes' pre_api_request transport metadata.
Requests routed through the Mimir OpenRouter redirect emit only an exact-ID
heartbeat with no repo identity, allowing the proxy exchange to adopt that
session instead of creating a duplicate heuristic row.

Install: copy this directory to the plugins directory under your Hermes home
(~/.hermes/plugins/ or %LOCALAPPDATA%/hermes/plugins on Windows). Uninstall:
delete the directory.

No credentials live here. Connection resolves from, in order:
  1. MIMIR_URL + MIMIR_TOKEN environment variables
  2. $MIMIR_HOME/config + $MIMIR_HOME/token
  3. ~/.mimir/config + ~/.mimir/token (written by `mimir setup`/`mimir login`)

Exchange payloads are durably queued before asynchronous upload; event delivery
remains best-effort. Session finalization is reported by on_session_finalize;
the server silence timer handles an abrupt exit.
"""

import hashlib
import json
import os
import re
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone

HEARTBEAT_SECONDS = 60
ACTIVITY_WINDOW_SECONDS = 5 * 60
MAX_REPORTED_IDS = 1000
HARNESS_LOAD_ATTEMPTS = 4
USER_AGENT = "mimir-hermes/1.0"
MAX_PENDING_REQUESTS = 128
MAX_PENDING_EXCHANGES = 256
MAX_EXCHANGE_BYTES = 1024 * 1024

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


def build_harness_load(source, receipt_text=None):
    load = {
        "version": 1,
        "harness": "hermes",
        "source_sha256": hashlib.sha256(source).hexdigest(),
    }
    if not receipt_text:
        return load
    try:
        receipt = json.loads(receipt_text)
        if not isinstance(receipt, dict):
            return load
        cli = receipt.get("cli") if isinstance(receipt.get("cli"), dict) else {}
        fields = {
            "bundle_version": receipt.get("bundle_version"),
            "cli_version": cli.get("version"),
            "cli_commit": cli.get("commit"),
            "installation_id": receipt.get("installation_id"),
        }
        load.update({key: value for key, value in fields.items() if isinstance(value, str) and value})
    except (TypeError, ValueError):
        pass
    return load


def load_harness_load(env, read_file, home, source_path=__file__):
    try:
        with open(source_path, "rb") as handle:
            source = handle.read()
    except OSError:
        return None
    directory = (env.get("MIMIR_HOME") or "").strip() or (os.path.join(home, ".mimir") if home else None)
    receipt = read_file(os.path.join(directory, "install-receipt.json")) if directory else None
    return build_harness_load(source, receipt)


def post_harness_load(connection, load):
    try:
        request = urllib.request.Request(
            f"{connection['url']}/integrations/harness-loads",
            data=json.dumps(load).encode("utf-8"),
            headers={
                "authorization": f"Bearer {connection['token']}",
                "content-type": "application/json",
                "user-agent": USER_AGENT,
            },
            method="POST",
        )
        with urllib.request.urlopen(request, timeout=10) as response:
            status = response.getcode()
            return isinstance(status, int) and 200 <= status < 300
    except Exception:
        return False


def report_harness_load(connection, load):
    for attempt in range(HARNESS_LOAD_ATTEMPTS):
        if post_harness_load(connection, load):
            return
        if attempt + 1 < HARNESS_LOAD_ATTEMPTS:
            threading.Event().wait(0.25 * (2 ** attempt))


def repo_name(directory):
    if not directory:
        return None
    parts = [part for part in re.split(r"[\\/]", directory.rstrip("/\\")) if part]
    return parts[-1] if parts else None



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
    """True only when this request used the Mimir-managed provider route."""
    if isinstance(base_url, str) and base_url.strip():
        return _uses_connection_url(base_url, connection_url)
    if isinstance(provider, str) and provider and provider.lower() != "openrouter":
        return False
    return liveness_only(env, connection_url)


def _exchange_id(session_id, api_request_id):
    return "hermes-" + hashlib.sha256(f"{session_id}\0{api_request_id}".encode()).hexdigest()


def _tokens(value):
    return value if type(value) is int and 0 <= value <= (1 << 53) - 1 else 0


def _truncated_hook(value):
    stack = [value]
    while stack:
        item = stack.pop()
        if isinstance(item, dict):
            if item.get("_truncated") or "_truncated_items" in item:
                return True
            stack.extend(item.values())
        elif isinstance(item, list):
            stack.extend(item)
        elif isinstance(item, str) and ("...[truncated " in item or item.endswith(" depth limit>")):
            return True
    return False


def _payload(session_id, request_id, pre, post, tools):
    request, response = pre.get("request"), post.get("response")
    if not isinstance(response, dict) or _truncated_hook(response):
        return None
    if not isinstance(response.get("assistant_message"), dict):
        return None
    if (not isinstance(request, dict) or _truncated_hook(request)
            or request.get("method") != "POST"
            or not isinstance(request.get("body"), dict) or not request["body"]):
        user_message = pre.get("user_message")
        if not isinstance(user_message, str) or not user_message:
            return None
        request = {"messages": [{"role": "user", "content": user_message}],
                   "capture_scope": "user_turn"}
    model = post.get("response_model") or post.get("model") or pre.get("model")
    if not isinstance(model, str) or not model:
        return None
    usage = post.get("usage") if isinstance(post.get("usage"), dict) else {}
    start, end = pre.get("started_at"), post.get("ended_at")
    try:
        ts = datetime.fromtimestamp(start, _UTC).isoformat().replace("+00:00", "Z")
    except (ValueError, OverflowError, OSError, TypeError):
        ts = _now()
    try:
        duration = max(0, round((end - start) * 1000))
    except (ValueError, TypeError, OverflowError):
        duration = 0
    provider = pre.get("provider") or post.get("provider")
    return {
        "exchange_id": _exchange_id(session_id, request_id),
        "ts": ts, "model": model[:256],
        "provider": provider[:256] if isinstance(provider, str) and provider else None,
        "request": request, "response": response,
        "tool_activity": tools[:1000],
        "usage": {key: _tokens(usage.get(key)) for key in
                  ("input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens")},
        "latency_ms": _tokens(duration),
        "request_kind": "primary",
    }


class _ExchangeQueue:
    """Disk-backed bounded queue; saved and skipped receipts have different meanings."""

    def __init__(self, connection, repo, directory):
        self.connection, self.repo, self.directory = connection, repo, directory
        self.lock = threading.Lock()
        self.active = False
        self.dirty = False
        try:
            os.makedirs(directory, mode=0o700, exist_ok=True)
            os.chmod(directory, 0o700)
        except OSError:
            self.directory = None

    def _path(self, session_id, exchange_id):
        digest = hashlib.sha256(f"{session_id}\0{exchange_id}".encode()).hexdigest()
        return os.path.join(self.directory, digest + ".json")

    def _files(self):
        try:
            return sorted((os.path.join(self.directory, name) for name in os.listdir(self.directory)
                           if re.fullmatch(r"[0-9a-f]{64}\.json", name)), key=os.path.getmtime)
        except OSError:
            return []

    def _replace(self, path, item):
        data = json.dumps(item, ensure_ascii=False, allow_nan=False).encode("utf-8")
        if len(data) > MAX_EXCHANGE_BYTES:
            return False
        fd, temporary = tempfile.mkstemp(dir=self.directory)
        try:
            with os.fdopen(fd, "wb") as handle:
                handle.write(data)
                handle.flush()
                os.fsync(handle.fileno())
            os.chmod(temporary, 0o600)
            os.replace(temporary, path)
            return True
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)

    def enqueue(self, session_id, payload, kind="exchanges"):
        if not self.directory:
            return False
        item = {"session_id": session_id, "kind": kind, "payload": payload, "repo": self.repo}
        try:
            path = self._path(session_id, payload["exchange_id"])
            with self.lock:
                if not os.path.exists(path):
                    if len(self._files()) >= MAX_PENDING_EXCHANGES or not self._replace(path, item):
                        return False
            self.replay()
            return True
        except (OSError, ValueError, TypeError, KeyError):
            return False

    def _post(self, item):
        request = urllib.request.Request(
            f"{self.connection['url']}/sessions/{urllib.parse.quote(item['session_id'], safe='')}/{item['kind']}",
            data=json.dumps(item["payload"], ensure_ascii=False).encode("utf-8"),
            headers={"authorization": f"Bearer {self.connection['token']}",
                     "content-type": "application/json", "user-agent": USER_AGENT,
                     "x-mimir-harness": "hermes",
                     **({"x-mimir-repo": item["repo"]} if item.get("repo") else {})},
            method="POST",
        )
        try:
            with urllib.request.urlopen(request, timeout=10) as response:
                result = json.load(response)
            if not isinstance(result, dict):
                return "retry"
            if (result.get("exchange_id") != item["payload"]["exchange_id"]
                    or result.get("session_id") != item["session_id"]):
                return "retry"
            status = result.get("capture_status")
            if item["kind"] == "exchange-failures":
                return "done" if status in ("failed", "saved") else "retry"
            return "done" if status in ("saved", "skipped") else "retry"
        except urllib.error.HTTPError as error:
            if error.code in (400, 413, 422):
                return "invalid"
            return "retry" if error.code == 429 or error.code >= 500 else "exhausted"
        except Exception:
            return "retry"

    def _drain(self):
        try:
            for path in self._files()[:MAX_PENDING_EXCHANGES]:
                try:
                    with open(path, encoding="utf-8") as handle:
                        item = json.load(handle)
                    if item.get("kind") not in ("exchanges", "exchange-failures"):
                        continue
                    for attempt in range(4):
                        result = self._post(item)
                        if result != "retry":
                            break
                        if attempt < 3:
                            threading.Event().wait(0.25 * (2 ** attempt))
                    if result == "done":
                        os.unlink(path)
                    elif result in ("invalid", "exhausted") and item["kind"] == "exchanges":
                        original = item["payload"]
                        item["kind"] = "exchange-failures"
                        item["payload"] = {
                            "exchange_id": original["exchange_id"], "model": original["model"],
                            "ts": original["ts"],
                            "failure_code": ("reported_payload_invalid" if result == "invalid"
                                             else "reported_delivery_exhausted"),
                        }
                        if self._replace(path, item):
                            for attempt in range(4):
                                if self._post(item) == "done":
                                    os.unlink(path)
                                    break
                                if attempt < 3:
                                    threading.Event().wait(0.25 * (2 ** attempt))
                except (OSError, ValueError, KeyError, TypeError):
                    continue
        finally:
            with self.lock:
                self.active = False
                dirty = self.dirty
                self.dirty = False
            if dirty:
                self.replay()

    def replay(self):
        if not self.directory:
            return
        with self.lock:
            if self.active:
                self.dirty = True
                return
            self.active = True
        threading.Thread(target=self._drain, daemon=True).start()


class _Reporter:
    def __init__(self, connection, repo):
        self._connection = connection
        self._repo = repo
        self._reported = []
        self._reported_set = set()
        self._last_session = None
        self._last_activity = 0.0
        self._requests = {}
        self._direct_sessions = set()
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
                "user-agent": USER_AGENT,
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

    def pre_request(self, session_id, turn_id, request_id, fields):
        if not session_id or not request_id:
            return
        self.flush(session_id, turn_id, except_id=request_id)
        proxied = turn_uses_proxy(fields.get("provider"), fields.get("base_url"),
                                  os.environ, self._connection["url"])
        key = (session_id, request_id)
        with self._lock:
            oldest = next(iter(self._requests), None) if len(self._requests) >= MAX_PENDING_REQUESTS else None
        if oldest and oldest != key:
            self.flush(oldest[0])
        evicted = None
        with self._lock:
            if key not in self._requests:
                if len(self._requests) >= MAX_PENDING_REQUESTS:
                    oldest_key = next(iter(self._requests))
                    evicted = (oldest_key, self._requests.pop(oldest_key))
                self._requests[key] = {"turn_id": turn_id, "pre": fields,
                                       "post": None, "tools": [], "tool_ids": set(),
                                       "proxied": proxied}
        if evicted and not evicted[1]["proxied"] and self._queue:
            (old_session, old_request), prior = evicted
            model = prior["pre"].get("model")
            self._queue.enqueue(old_session, {
                "exchange_id": _exchange_id(old_session, old_request),
                "model": model[:256] if isinstance(model, str) and model else "unknown",
                "ts": _now(), "failure_code": "reported_delivery_exhausted",
            }, kind="exchange-failures")
        if proxied:
            # Match the proxy's exact-ID heartbeat with no repo identity.
            self.post(build_simple_event("heartbeat", session_id, None))
        else:
            self.activate_direct(session_id)

    def post_request(self, session_id, request_id, fields):
        with self._lock:
            entry = self._requests.get((session_id, request_id))
            if entry and entry["post"] is None:
                entry["post"] = fields

    def tool_call(self, session_id, turn_id, request_id, fields):
        with self._lock:
            key = (session_id, request_id)
            entry = self._requests.get(key)
            if entry is None and not request_id:
                entries = [(k, v) for k, v in self._requests.items()
                           if k[0] == session_id and v["turn_id"] == turn_id and v["post"]]
                entry = entries[-1][1] if entries else None
            if not entry or entry["proxied"] or len(entry["tools"]) >= 1000:
                return
            tool_id = fields.get("tool_call_id")
            if tool_id and tool_id in entry["tool_ids"]:
                return
            name = fields.get("tool_name")
            if not isinstance(name, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._:-]{0,127}", name):
                return
            args = fields.get("args")
            if not isinstance(args, dict):
                return
            try:
                args_text = json.dumps(args, ensure_ascii=False, default=str)
                if len(args_text) > 50000:
                    return
                output = fields.get("result")
                rendered = (output if isinstance(output, str)
                            else json.dumps(output, ensure_ascii=False, default=str))
            except (ValueError, TypeError):
                return
            activity = {"name": name, "input": json.loads(args_text),
                        "status": "failed" if fields.get("status") not in (None, "ok", "succeeded", "success")
                        else "succeeded"}
            if output is not None:
                activity["output"] = rendered[:50000]
            entry["tools"].append(activity)
            if tool_id:
                entry["tool_ids"].add(tool_id)

    def flush(self, session_id, turn_id=None, except_id=None):
        with self._lock:
            ready = [(key, value) for key, value in self._requests.items()
                     if key[0] == session_id and key[1] != except_id
                     and (turn_id is None or value["turn_id"] == turn_id) and value["post"]]
            for key, _value in ready:
                del self._requests[key]
        for (sid, request_id), entry in ready:
            if entry["proxied"]:
                continue
            payload = _payload(sid, request_id, entry["pre"], entry["post"], entry["tools"])
            if payload and self._queue.enqueue(sid, payload):
                self.touch(sid)
                continue
            pre = entry["pre"]
            model = entry["post"].get("model") or pre.get("model")
            self._queue.enqueue(sid, {
                "exchange_id": _exchange_id(sid, request_id),
                "model": model[:256] if isinstance(model, str) and model else "unknown",
                "ts": payload["ts"] if payload else _now(),
                "failure_code": "reported_payload_invalid" if payload is None else "reported_delivery_exhausted",
            }, kind="exchange-failures")

    def activate_direct(self, session_id):
        with self._lock:
            if session_id in self._direct_sessions:
                return False
            self._direct_sessions.add(session_id)
            self._last_session = session_id
            self._last_activity = time.monotonic()
        self.deliver(build_simple_event("heartbeat", session_id, self._repo))
        return True

    def finish(self, session_id, reason):
        with self._lock:
            direct = session_id in self._direct_sessions
            self._direct_sessions.discard(session_id)
            stale_requests = [key for key in self._requests if key[0] == session_id]
            for key in stale_requests:
                del self._requests[key]
            if self._last_session == session_id:
                self._last_session = None
                self._last_activity = 0.0
        if direct:
            self.deliver(build_simple_event("end", session_id, self._repo, reason=reason), wait_on_exit=True)

    def heartbeat_if_active(self):
        session_id = self.active_session()
        if session_id:
            self.deliver(build_simple_event("heartbeat", session_id, self._repo))


def register(ctx):
    connection = load_connection()
    if not connection:
        return
    home = None
    try:
        home = os.path.expanduser("~")
    except Exception:
        pass
    load = load_harness_load(os.environ, _read_file, home)
    if load:
        threading.Thread(target=report_harness_load, args=(connection, load), daemon=True).start()
    try:
        repo = repo_name(os.getcwd())
    except Exception:
        repo = None
    reporter = _Reporter(connection, repo)
    directory = (os.environ.get("MIMIR_HOME") or
                 (os.path.join(home, ".mimir") if home else None))
    reporter._queue = _ExchangeQueue(
        connection, repo, os.path.join(directory, "hermes-exchanges")
        if directory else None,
    ) if directory else None
    if reporter._queue:
        reporter._queue.replay()
    def heartbeat_loop():
        while True:
            threading.Event().wait(HEARTBEAT_SECONDS)
            if reporter._queue:
                reporter._queue.replay()
            reporter.heartbeat_if_active()

    threading.Thread(target=heartbeat_loop, daemon=True).start()

    def on_transport(session_id=None, turn_id=None, api_request_id=None, provider=None,
                     base_url=None, **kwargs):
        reporter.pre_request(session_id, turn_id, api_request_id,
                             {"provider": provider, "base_url": base_url, **kwargs})

    def on_response(session_id=None, api_request_id=None, **kwargs):
        reporter.post_request(session_id, api_request_id, kwargs)

    def on_tool(session_id=None, turn_id=None, api_request_id=None, **kwargs):
        reporter.tool_call(session_id, turn_id, api_request_id, kwargs)

    def on_turn(session_id=None, turn_id=None, **_kwargs):
        if session_id:
            reporter.flush(session_id, turn_id)

    def on_start(**_kwargs):
        pass

    def on_finalize(session_id=None, reason=None, **_kwargs):
        if session_id:
            reporter.flush(session_id)
            reporter.finish(session_id, reason or "session finalized")

    ctx.register_hook("pre_api_request", on_transport)
    ctx.register_hook("post_api_request", on_response)
    ctx.register_hook("post_tool_call", on_tool)
    ctx.register_hook("post_llm_call", on_turn)
    ctx.register_hook("on_session_start", on_start)
    ctx.register_hook("on_session_finalize", on_finalize)


# Test surface.
__testing = {
    "parse_mimir_config": parse_mimir_config,
    "resolve_connection": resolve_connection,
    "repo_name": repo_name,
    "build_simple_event": build_simple_event,
    "uses_connection_url": _uses_connection_url,
    "liveness_only": liveness_only,
    "turn_uses_proxy": turn_uses_proxy,
    "build_harness_load": build_harness_load,
    "load_harness_load": load_harness_load,
    "post_harness_load": post_harness_load,
    "report_harness_load": report_harness_load,
}
