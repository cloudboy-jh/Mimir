import io
import os
import json
import sys
import tempfile
import unittest
import urllib.error
from unittest.mock import patch

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from __init__ import __testing  # noqa: E402
import __init__ as mimir_plugin  # noqa: E402

parse_mimir_config = __testing["parse_mimir_config"]
resolve_connection = __testing["resolve_connection"]
repo_name = __testing["repo_name"]
build_simple_event = __testing["build_simple_event"]
liveness_only = __testing["liveness_only"]
turn_uses_proxy = __testing["turn_uses_proxy"]
uses_connection_url = __testing["uses_connection_url"]
build_harness_load = __testing["build_harness_load"]
load_harness_load = __testing["load_harness_load"]
post_harness_load = __testing["post_harness_load"]
report_harness_load = __testing["report_harness_load"]
mimir_executable = __testing["mimir_executable"]
commit_confirmations = __testing["commit_confirmations"]
tool_schema = __testing["tool_schema"]
call_mimir_tool = __testing["call_mimir_tool"]


class FakeProcess:
    """Popen stand-in that records the argv and the JSON written to stdin."""

    def __init__(self, argv, spawned):
        sink = spawned

        class Stdin(io.BytesIO):
            def close(self):
                sink.append((argv, json.loads(self.getvalue())))
                super().close()

        self.stdin = Stdin()

    def wait(self, timeout=None):
        return 0


class ParseMimirConfigTest(unittest.TestCase):
    def test_extracts_url_and_executable(self):
        self.assertEqual(parse_mimir_config('url = "https://mimir.example/"\n'), {"url": "https://mimir.example"})
        self.assertEqual(parse_mimir_config("url = https://mimir.example\r\nexecutable=C:\\Tools\\mimir.exe\r\n"),
                         {"url": "https://mimir.example", "executable": "C:\\Tools\\mimir.exe"})
        self.assertEqual(parse_mimir_config("other = 1\n"), {})

    def test_mimir_binary_resolution_order(self):
        files = {os.path.join("/home/u", ".mimir", "config"): 'url = "https://m"\nexecutable=/usr/local/bin/mimir\n'}
        self.assertEqual(mimir_executable({"MIMIR_BIN": " /opt/mimir "}, files.get, "/home/u"), "/opt/mimir")
        self.assertEqual(mimir_executable({}, files.get, "/home/u"), "/usr/local/bin/mimir")
        self.assertEqual(mimir_executable({}, lambda _path: None, "/home/u"), "mimir")

    def test_commit_confirmations_keep_only_git_summary_lines(self):
        self.assertEqual(commit_confirmations({"output": "lint ok\n[main 1a2b3c4] fix\n 1 file changed"}), "[main 1a2b3c4] fix")
        self.assertEqual(commit_confirmations("[main (root-commit) 1a2b3c4] first"), "[main (root-commit) 1a2b3c4] first")
        self.assertIsNone(commit_confirmations("nothing to commit"))
        self.assertIsNone(commit_confirmations(None))


class ResolveConnectionTest(unittest.TestCase):
    FILES = {
        os.path.join("/home/u", ".mimir", "config"): 'url = "https://mimir.example"\n',
        os.path.join("/home/u", ".mimir", "token"): "tok-123\n",
    }

    def read_file(self, path):
        return self.FILES.get(path.replace("\\", "/")) or self.FILES.get(path)

    def test_prefers_environment_overrides(self):
        conn = resolve_connection({"MIMIR_URL": "https://env.example/", "MIMIR_TOKEN": "env-tok"}, self.read_file, "/home/u")
        self.assertEqual(conn, {"url": "https://env.example", "token": "env-tok"})

    def test_reads_mimir_home(self):
        files = {path.replace("\\", "/"): text for path, text in self.FILES.items()}
        conn = resolve_connection({}, lambda path: files.get(path.replace("\\", "/")), "/home/u")
        self.assertEqual(conn, {"url": "https://mimir.example", "token": "tok-123"})

    def test_inert_without_complete_connection(self):
        self.assertIsNone(resolve_connection({}, lambda _path: None, "/home/u"))
        self.assertIsNone(resolve_connection({"MIMIR_URL": "https://env.example"}, lambda _path: None, None))


class BuildEventsTest(unittest.TestCase):
    def test_simple_event(self):
        event = build_simple_event("end", "ses-1", "mimir", reason="harness exit")
        self.assertEqual(event["kind"], "end")
        self.assertEqual(event["reason"], "harness exit")
        heartbeat = build_simple_event("heartbeat", "ses-1", "mimir")
        self.assertNotIn("reason", heartbeat)


class StartupBuildIdentityTest(unittest.TestCase):
    def test_payload_auth_and_path_allowlist_safe_provenance(self):
        load = build_harness_load(b"loaded plugin source", json.dumps({
            "bundle_version": "v2.3.4",
            "installation_id": "install-1",
            "cli": {"version": "2.3.4", "commit": "abc123", "path": "/secret/mimir", "sha256": "cli-hash"},
            "source": "/private/checkout",
            "token": "do-not-send",
        }))

        class Response:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def getcode(self):
                return 204

        captured = []
        with patch("urllib.request.urlopen", side_effect=lambda request, timeout: captured.append((request, timeout)) or Response()):
            self.assertTrue(post_harness_load({"url": "https://mimir.example", "token": "tok-secret"}, load))
        request, timeout = captured[0]
        self.assertEqual(request.full_url, "https://mimir.example/integrations/harness-loads")
        self.assertEqual(request.method, "POST")
        self.assertEqual(request.get_header("Authorization"), "Bearer tok-secret")
        self.assertEqual(request.get_header("User-agent"), "mimir-hermes/1.0")
        self.assertEqual(timeout, 10)
        self.assertEqual(json.loads(request.data), {
            "version": 1,
            "harness": "hermes",
            "source_sha256": "1f276ede474cf6948a22d1f3dc41be29d345f91672da261558f922e1826aed59",
            "bundle_version": "v2.3.4",
            "cli_version": "2.3.4",
            "cli_commit": "abc123",
            "installation_id": "install-1",
        })

    def test_missing_manifest_reports_loaded_source_hash(self):
        # NamedTemporaryFile keeps an exclusive handle on Windows, so write
        # and close the source before the plugin reopens it by path.
        with tempfile.TemporaryDirectory() as directory:
            source_path = os.path.join(directory, "mimir_plugin.py")
            with open(source_path, "wb") as source:
                source.write(b"source")
            load = load_harness_load({"MIMIR_HOME": "/missing"}, lambda _path: None, None, source_path)
        self.assertEqual(load, {
            "version": 1,
            "harness": "hermes",
            "source_sha256": "41cf6794ba4200b839c53531555f0f3998df4cbb01a4d5cb0b94e3ca5e23947d",
        })

    def test_network_and_non_2xx_failures_are_contained_and_retried(self):
        connection = {"url": "https://mimir.example", "token": "tok"}
        load = build_harness_load(b"source")

        class Response:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def getcode(self):
                return 503

        with patch("urllib.request.urlopen", return_value=Response()):
            self.assertFalse(post_harness_load(connection, load))
        with patch("urllib.request.urlopen", side_effect=OSError("offline")):
            self.assertFalse(post_harness_load(connection, load))
        with patch.object(mimir_plugin, "post_harness_load", side_effect=[False, True]) as post, \
             patch("threading.Event.wait", return_value=True):
            report_harness_load(connection, load)
        self.assertEqual(post.call_count, 2)

    def test_session_event_uses_explicit_user_agent(self):
        class Response:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

        captured = []
        reporter = mimir_plugin._Reporter(
            {"url": "https://mimir.example", "token": "tok-secret"},
            "repo",
        )
        with patch(
            "urllib.request.urlopen",
            side_effect=lambda request, timeout: captured.append((request, timeout)) or Response(),
        ):
            self.assertTrue(reporter.post({"session_id": "session-1", "kind": "heartbeat"}))
        request, timeout = captured[0]
        self.assertEqual(request.get_header("User-agent"), "mimir-hermes/1.0")
        self.assertEqual(timeout, 10)


class RepoNameTest(unittest.TestCase):
    def test_git_work_tree_top_level_or_none(self):
        with tempfile.TemporaryDirectory() as root:
            checkout = os.path.join(root, "mimir")
            nested = os.path.join(checkout, "worker", "src")
            worktree = os.path.join(root, "mimir-feature")
            outside = os.path.join(root, "johns", "Temp")
            os.makedirs(os.path.join(checkout, ".git"))
            os.makedirs(nested)
            os.makedirs(worktree)
            os.makedirs(outside)
            with open(os.path.join(worktree, ".git"), "w", encoding="utf-8") as handle:
                handle.write("gitdir: ../mimir/.git/worktrees/feature\n")
            self.assertEqual(repo_name(checkout), "mimir")
            self.assertEqual(repo_name(nested + os.sep), "mimir")
            self.assertEqual(repo_name(worktree), "mimir-feature")
            self.assertIsNone(repo_name(outside))
        self.assertIsNone(repo_name(None))


class LivenessOnlyTest(unittest.TestCase):
    def test_detects_managed_redirect(self):
        env = {"OPENROUTER_BASE_URL": "https://mimir.example.workers.dev/v1/hermes"}
        self.assertTrue(liveness_only(env, "https://mimir.example.workers.dev"))
        self.assertFalse(liveness_only({}, "https://mimir.example.workers.dev"))
        self.assertFalse(liveness_only({"OPENROUTER_BASE_URL": "https://openrouter.ai/api/v1"}, "https://mimir.example.workers.dev"))

    def test_classifies_each_turn_instead_of_disabling_all_providers(self):
        env = {"OPENROUTER_BASE_URL": "https://mimir.example.workers.dev/v1/hermes"}
        worker = "https://mimir.example.workers.dev"
        self.assertTrue(turn_uses_proxy("openrouter", "https://mimir.example.workers.dev/v1/hermes", env, worker))
        self.assertFalse(turn_uses_proxy("anthropic", "https://api.anthropic.com", env, worker))
        self.assertFalse(turn_uses_proxy("nous", None, env, worker))

    def test_requires_the_same_origin_and_path_boundary(self):
        worker = "https://mimir.example.workers.dev"
        self.assertTrue(uses_connection_url(worker + "/v1/hermes", worker))
        self.assertFalse(uses_connection_url("https://mimir.example.workers.dev.evil/v1/hermes", worker))
        self.assertFalse(uses_connection_url("https://other.example/mimir.example.workers.dev", worker))


class ReporterLifecycleTest(unittest.TestCase):
    def test_heartbeat_does_not_keep_session_alive_forever_and_finish_clears_it(self):
        reporter_type = __import__("__init__")._Reporter
        reporter = reporter_type({"url": "https://mimir.example", "token": "tok"}, "repo")

        with patch.object(reporter, "deliver"):
            reporter.activate_direct("ses-1")
            reporter.heartbeat_if_active()
            reporter.finish("ses-1", "finalized")
        self.assertIsNone(reporter.active_session())

    def test_failed_delivery_retries(self):
        reporter = mimir_plugin._Reporter({"url": "https://mimir.example", "token": "tok"}, "repo")

        class ImmediateTimer:
            def __init__(self, _delay, callback, args=()):
                self.callback, self.args, self.daemon = callback, args, False

            def start(self):
                self.callback(*self.args)

        class ImmediateThread:
            def __init__(self, target, args=(), daemon=False):
                self.target, self.args, self.daemon = target, args, daemon

            def start(self):
                self.target(*self.args)

        with patch.object(reporter, "post", side_effect=[False, True]) as post, \
             patch("threading.Timer", ImmediateTimer), patch("threading.Thread", ImmediateThread):
            reporter.deliver(build_simple_event("heartbeat", "ses-1", "repo"), key="heartbeat:ses-1")
        self.assertEqual(post.call_count, 2)


class HookContractTest(unittest.TestCase):
    class Context:
        def __init__(self):
            self.hooks = {}
            self.tools = {}

        def register_hook(self, name, callback):
            self.hooks[name] = callback

        def register_tool(self, name, toolset, schema, handler):
            self.tools[name] = (toolset, schema, handler)

    def setUp(self):
        import tempfile
        self.home = tempfile.TemporaryDirectory()
        self.addCleanup(self.home.cleanup)
        self.ctx = self.Context()
        self.events = []
        self.spawned = []
        self.patches = [
            patch.object(mimir_plugin, "load_connection", return_value={"url": "https://mimir.example", "token": "tok"}),
            patch.object(mimir_plugin, "load_harness_load", return_value=None),
            patch.object(mimir_plugin._Reporter, "deliver",
                         lambda _reporter, event, **kw: self.events.append(event)),
            patch.object(mimir_plugin._Reporter, "post",
                         lambda _reporter, event: self.events.append(event) or True),
            patch.object(mimir_plugin._ExchangeQueue, "replay", lambda _queue: None),
            patch("threading.Thread.start", return_value=None),
            patch.object(mimir_plugin.subprocess, "Popen", lambda argv, **_kw: FakeProcess(argv, self.spawned)),
            patch.dict(os.environ, {"MIMIR_HOME": self.home.name, "MIMIR_BIN": "/opt/mimir/bin/mimir",
                                    "PATH": os.environ.get("PATH", "")}, clear=True),
        ]
        for active_patch in self.patches:
            active_patch.start()
            self.addCleanup(active_patch.stop)
        mimir_plugin.register(self.ctx)

    def pre(self, session="s", request_id="r", provider="codex", base_url="https://api.openai.com"):
        self.ctx.hooks["pre_api_request"](
            session_id=session, turn_id="turn", api_request_id=request_id,
            provider=provider, base_url=base_url, model="gpt-5-codex",
            started_at=1760000000.0,
            request={"method": "POST", "body": {"messages": [{"role": "user", "content": "fix bug"}]}},
        )

    def response(self, session="s", request_id="r"):
        self.ctx.hooks["post_api_request"](
            session_id=session, turn_id="turn", api_request_id=request_id,
            model="gpt-5-codex", response_model="gpt-5-codex",
            response={"assistant_message": {"role": "assistant", "content": "done"}},
            usage={"input_tokens": 10, "output_tokens": 3, "cache_read_tokens": 2},
            ended_at=1760000000.125,
        )

    def finish(self, session="s"):
        self.ctx.hooks["post_llm_call"](session_id=session, turn_id="turn")

    def queued(self):
        directory = os.path.join(self.home.name, "hermes-exchanges")
        result = []
        for name in os.listdir(directory):
            if name.endswith(".json"):
                with open(os.path.join(directory, name), encoding="utf-8") as handle:
                    result.append(json.load(handle))
        return result

    def test_real_codex_exchange_includes_tool_activity_and_stable_identity(self):
        self.assertEqual(set(self.ctx.hooks), {"pre_api_request", "post_api_request",
                                                "post_tool_call", "post_llm_call",
                                                "on_session_start", "on_session_finalize"})
        self.pre()
        self.response()
        self.ctx.hooks["post_tool_call"](
            session_id="s", turn_id="turn", api_request_id="r",
            tool_call_id="tool-1", tool_name="bash", args={"command": "pwd"},
            status="ok", result="C:/repo",
        )
        self.finish()
        self.assertEqual([e["kind"] for e in self.events], ["heartbeat"])
        item, = self.queued()
        payload = item["payload"]
        self.assertEqual(item["kind"], "exchanges")
        self.assertEqual(payload["exchange_id"], mimir_plugin._exchange_id("s", "r"))
        self.assertEqual(payload["request"]["body"]["messages"][0]["content"], "fix bug")
        self.assertEqual(payload["response"]["assistant_message"]["content"], "done")
        self.assertEqual(payload["usage"]["cache_read_tokens"], 2)
        self.assertEqual(payload["latency_ms"], 125)
        self.assertEqual(payload["tool_activity"], [
            {"name": "bash", "input": {"command": "pwd"}, "status": "succeeded", "output": "C:/repo"}
        ])
        if os.name == "posix":
            self.assertEqual(os.stat(os.path.join(self.home.name, "hermes-exchanges")).st_mode & 0o777, 0o700)
        self.ctx.hooks["on_session_finalize"](session_id="s", reason="finalized")
        self.assertEqual([e["kind"] for e in self.events], ["heartbeat", "end"])

    def test_proxy_and_direct_in_same_turn_only_queue_direct(self):
        self.pre(request_id="proxy", provider="openrouter",
                 base_url="https://mimir.example/v1/hermes")
        self.response(request_id="proxy")
        self.pre(request_id="r")
        self.response()
        self.finish()
        self.assertEqual(len(self.queued()), 1)
        self.assertEqual([e["repo"] for e in self.events], [None, mimir_plugin.repo_name(os.getcwd())])

    def test_repeated_hook_and_retry_remain_idempotent(self):
        self.pre()
        self.response()
        self.response()
        self.finish()
        self.pre()
        self.response()
        self.finish()
        self.assertEqual(len(self.queued()), 1)

    def test_no_request_response_does_not_fabricate_exchange(self):
        self.pre()
        self.finish()
        self.ctx.hooks["on_session_finalize"](session_id="s")
        self.assertEqual(self.queued(), [])
        self.assertEqual([e["kind"] for e in self.events], ["heartbeat", "end"])

    def test_oversized_provider_request_retains_actual_user_and_assistant(self):
        self.ctx.hooks["pre_api_request"](
            session_id="s", turn_id="turn", api_request_id="large",
            provider="openai-codex", base_url="https://api.openai.com",
            model="gpt-5-codex", started_at=1760000000.0,
            user_message="the actual user question",
            request={"_truncated": True, "preview": "partial provider context"},
        )
        self.response(request_id="large")
        self.finish()
        item, = self.queued()
        self.assertEqual(item["payload"]["request"], {
            "messages": [{"role": "user", "content": "the actual user question"}],
            "capture_scope": "user_turn",
        })
        self.assertEqual(item["payload"]["response"]["assistant_message"]["content"], "done")

    def test_truncated_hook_reports_payload_invalid_not_empty_echo(self):
        self.pre()
        self.ctx.hooks["post_api_request"](
            session_id="s", api_request_id="r", model="gpt-5-codex",
            response={"_truncated": True, "preview": "partial"},
        )
        self.finish()
        item, = self.queued()
        self.assertEqual(item["kind"], "exchange-failures")
        self.assertEqual(item["payload"]["failure_code"], "reported_payload_invalid")

    def hook_input(self, **fields):
        repo = mimir_plugin.repo_name(os.getcwd())
        return {"session_id": "exact", "harness": "hermes", **({"repo": repo} if repo else {}), **fields}

    def test_shell_commit_output_is_handed_to_mimir_hook_git(self):
        after = self.ctx.hooks["post_tool_call"]
        summary = "[main 1a2b3c4] done\n 1 file changed"
        after(session_id="exact", tool_call_id="1", tool_name="terminal",
              args={"command": "git commit -m done", "workdir": self.home.name}, status="ok", result={"output": summary})
        after(session_id="exact", tool_call_id="2", tool_name="terminal", args={}, status="failed", result=summary)
        after(session_id="exact", tool_call_id="3", tool_name="read_file", args={}, status="ok", result=summary)
        after(session_id=None, tool_call_id="4", tool_name="terminal", args={}, status="ok", result=summary)
        after(session_id="exact", tool_call_id="5", tool_name="terminal", args={}, status="ok", result="nothing to commit")
        self.assertEqual(self.spawned, [(["/opt/mimir/bin/mimir", "_hook", "git"],
                                         self.hook_input(cwd=self.home.name, tool_output="[main 1a2b3c4] done"))])
        self.assertEqual(self.queued(), [])

    def test_session_finalize_refreshes_pushed_state(self):
        self.ctx.hooks["on_session_finalize"](session_id="exact", reason="done")
        self.assertEqual(self.spawned, [(["/opt/mimir/bin/mimir", "_hook", "git"],
                                         self.hook_input(cwd=os.getcwd(), refresh=True))])

    def test_missing_mimir_binary_never_interrupts_hermes(self):
        with patch.object(mimir_plugin.subprocess, "Popen", side_effect=FileNotFoundError("mimir")):
            self.ctx.hooks["post_tool_call"](session_id="exact", tool_call_id="1", tool_name="terminal",
                                             args={}, status="ok", result="[main 1a2b3c4] done")
            self.ctx.hooks["on_session_finalize"](session_id="exact", reason="done")
        self.assertEqual(self.spawned, [])

    def test_registers_forwarding_mcp_tools(self):
        self.assertEqual(set(self.ctx.tools), {
            "mimir_search", "mimir_sessions", "mimir_session", "mimir_turns", "mimir_stats",
            "mimir_day", "mimir_devices", "mimir_commits", "mimir_commit_patch"})
        toolset, schema, handler = self.ctx.tools["mimir_session"]
        self.assertEqual(toolset, "mimir")
        self.assertEqual(schema["name"], "mimir_session")
        response = io.BytesIO(json.dumps({"jsonrpc": "2.0", "id": 1, "result": {
            "content": [{"type": "text", "text": '{"session": {"id": "s"}}'}]}}).encode())
        with patch("urllib.request.urlopen", return_value=response) as urlopen:
            self.assertEqual(handler({"id": "s"}, task_id="t"), '{"session": {"id": "s"}}')
        self.assertEqual(json.loads(urlopen.call_args.args[0].data)["params"], {"name": "session", "arguments": {"id": "s"}})


class McpToolTest(unittest.TestCase):
    CONNECTION = {"url": "https://mimir.example", "token": "tok"}

    @staticmethod
    def reply(message):
        return io.BytesIO(json.dumps(message).encode())

    def test_forwards_tools_call_with_machine_token(self):
        result = {"sessions": [], "next_cursor": None}
        with patch("urllib.request.urlopen", return_value=self.reply({"jsonrpc": "2.0", "id": 1, "result": {
                "content": [{"type": "text", "text": json.dumps(result)}], "structuredContent": result}})) as urlopen:
            text = call_mimir_tool(self.CONNECTION, "sessions", {"repo": "mimir", "limit": 5, "cursor": None})
        self.assertEqual(json.loads(text), result)
        request = urlopen.call_args.args[0]
        self.assertEqual(request.full_url, "https://mimir.example/mcp")
        self.assertEqual(request.method, "POST")
        self.assertEqual(request.get_header("Authorization"), "Bearer tok")
        self.assertEqual(json.loads(request.data), {"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                                                    "params": {"name": "sessions", "arguments": {"repo": "mimir", "limit": 5}}})

    def test_failures_return_json_errors_instead_of_raising(self):
        with patch("urllib.request.urlopen", return_value=self.reply({"jsonrpc": "2.0", "id": 1, "result": {
                "content": [{"type": "text", "text": "query is required"}], "isError": True}})):
            self.assertEqual(json.loads(call_mimir_tool(self.CONNECTION, "search", {})),
                             {"error": "Mimir search failed: query is required"})
        with patch("urllib.request.urlopen", return_value=self.reply({"jsonrpc": "2.0", "id": 1,
                                                                      "error": {"code": -32601, "message": "Method not found"}})):
            self.assertEqual(json.loads(call_mimir_tool(self.CONNECTION, "devices", {})),
                             {"error": "Mimir devices failed: Method not found"})
        unauthorized = urllib.error.HTTPError("https://mimir.example/mcp", 401, "Unauthorized", {}, io.BytesIO(b"unauthorized"))
        with patch("urllib.request.urlopen", side_effect=unauthorized):
            self.assertEqual(json.loads(call_mimir_tool(self.CONNECTION, "stats", {"group_by": "repo"})),
                             {"error": "Mimir stats failed (401): unauthorized"})
        with patch("urllib.request.urlopen", side_effect=urllib.error.URLError("offline")):
            self.assertIn("Mimir day failed", json.loads(call_mimir_tool(self.CONNECTION, "day", {}))["error"])

    def test_schemas_match_the_mcp_contract(self):
        expected = {
            "search": (["query", "repo", "from", "to", "limit"], ["query"]),
            "sessions": (["q", "repo", "harness", "device", "state", "git_status", "from", "to", "limit", "cursor"], []),
            "session": (["id"], ["id"]),
            "turns": (["id", "cursor", "limit", "order"], ["id"]),
            "stats": (["group_by", "from", "to", "tz"], ["group_by"]),
            "day": (["date", "tz"], []),
            "devices": ([], []),
            "commits": (["repo", "ref", "q", "session", "from", "to", "limit", "cursor"], []),
            "commit_patch": (["session_id", "sha", "max_bytes"], ["session_id", "sha"]),
        }
        for name, (properties, required) in expected.items():
            parameters = tool_schema(name)["parameters"]
            self.assertEqual(list(parameters["properties"]), properties, name)
            self.assertEqual(parameters["required"], required, name)
            self.assertTrue(tool_schema(name)["description"].endswith("."), name)


class ExchangeDeliveryTest(unittest.TestCase):
    def setUp(self):
        import tempfile
        self.home = tempfile.TemporaryDirectory()
        self.addCleanup(self.home.cleanup)
        self.queue = mimir_plugin._ExchangeQueue(
            {"url": "https://mimir.example", "token": "tok"}, "repo", self.home.name,
        )
        self.payload = {
            "exchange_id": "hermes-123", "model": "gpt-5-codex",
            "ts": "2026-01-01T00:00:00Z", "request": {"body": {"input": "hello"}},
            "response": {"assistant_message": {"content": "world"}},
            "tool_activity": [], "usage": {"input_tokens": 1, "output_tokens": 1},
            "latency_ms": 5, "request_kind": "primary",
        }

    def receipt(self, status):
        import io
        return io.BytesIO(json.dumps({
            "exchange_id": "hermes-123", "session_id": "s",
            "capture_status": status, "duplicate": False,
        }).encode())

    def test_saved_receipt_and_headers_remove_durable_payload(self):
        with patch.object(self.queue, "replay"), patch("urllib.request.urlopen", return_value=self.receipt("saved")) as send:
            self.assertTrue(self.queue.enqueue("s", self.payload))
            self.queue._drain()
        self.assertEqual(self.queue._files(), [])
        request = send.call_args.args[0]
        self.assertTrue(request.full_url.endswith("/sessions/s/exchanges"))
        self.assertEqual(request.get_header("X-mimir-harness"), "hermes")
        self.assertEqual(request.get_header("X-mimir-repo"), "repo")

    def test_transport_failure_stays_durable_until_next_registration(self):
        import urllib.error
        with patch.object(self.queue, "replay"), \
             patch("urllib.request.urlopen", side_effect=urllib.error.URLError("offline")), \
             patch("threading.Event.wait", return_value=None):
            self.assertTrue(self.queue.enqueue("s", self.payload))
            self.queue._drain()
        self.assertEqual(len(self.queue._files()), 1)
        resumed = mimir_plugin._ExchangeQueue(self.queue.connection, "repo", self.home.name)
        with patch("urllib.request.urlopen", return_value=self.receipt("saved")):
            resumed._drain()
        self.assertEqual(resumed._files(), [])

    def test_skipped_policy_is_not_failure_or_retry(self):
        with patch.object(self.queue, "replay"), \
             patch("urllib.request.urlopen", return_value=self.receipt("skipped")) as send:
            self.queue.enqueue("s", self.payload)
            self.queue._drain()
        self.assertEqual(send.call_count, 1)
        self.assertEqual(self.queue._files(), [])

    def test_invalid_exchange_becomes_durable_failure_report(self):
        import urllib.error
        with patch.object(self.queue, "replay"), \
             patch("urllib.request.urlopen", side_effect=urllib.error.HTTPError(
                 "https://mimir.example", 400, "invalid", {}, None,
             )):
            self.queue.enqueue("s", self.payload)
            self.queue._drain()
        path, = self.queue._files()
        with open(path, encoding="utf-8") as handle:
            failure = json.load(handle)
        self.assertEqual(failure["kind"], "exchange-failures")
        self.assertEqual(failure["payload"]["failure_code"], "reported_payload_invalid")
        with patch("urllib.request.urlopen", return_value=self.receipt("failed")):
            self.queue._drain()
        self.assertEqual(self.queue._files(), [])

    def test_failure_notification_requires_durable_receipt(self):
        failure = {"exchange_id": "hermes-123", "model": "gpt-5-codex",
                   "ts": "2026-01-01T00:00:00Z",
                   "failure_code": "reported_payload_invalid"}
        with patch.object(self.queue, "replay"):
            self.queue.enqueue("s", failure, kind="exchange-failures")
        with patch("urllib.request.urlopen", return_value=self.receipt("accepted")), \
             patch("threading.Event.wait", return_value=None):
            self.queue._drain()
        self.assertEqual(len(self.queue._files()), 1)
        with patch("urllib.request.urlopen", return_value=self.receipt("saved")):
            self.queue._drain()
        self.assertEqual(self.queue._files(), [])


if __name__ == "__main__":
    unittest.main()
