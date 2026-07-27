import os
import json
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from __init__ import __testing  # noqa: E402
import __init__ as mimir_plugin  # noqa: E402

parse_mimir_config = __testing["parse_mimir_config"]
resolve_connection = __testing["resolve_connection"]
repo_name = __testing["repo_name"]
build_turn_event = __testing["build_turn_event"]
build_simple_event = __testing["build_simple_event"]
liveness_only = __testing["liveness_only"]
turn_uses_proxy = __testing["turn_uses_proxy"]
uses_connection_url = __testing["uses_connection_url"]
build_harness_load = __testing["build_harness_load"]
load_harness_load = __testing["load_harness_load"]
post_harness_load = __testing["post_harness_load"]
report_harness_load = __testing["report_harness_load"]


class ParseMimirConfigTest(unittest.TestCase):
    def test_extracts_and_normalizes_url(self):
        self.assertEqual(parse_mimir_config('url = "https://mimir.example/"\n'), {"url": "https://mimir.example"})
        self.assertEqual(parse_mimir_config("url = https://mimir.example\n"), {"url": "https://mimir.example"})
        self.assertEqual(parse_mimir_config("other = 1\n"), {})


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
    def test_turn_event(self):
        event = build_turn_event("ses-1", "turn-1", "openai/gpt-5", "fix the bug", "mimir")
        self.assertEqual(event["version"], 1)
        self.assertEqual(event["kind"], "turn")
        self.assertEqual(event["session_id"], "ses-1")
        self.assertEqual(event["harness"], "hermes")
        self.assertEqual(event["repo"], "mimir")
        self.assertEqual(event["turn"]["exchange_id"], "turn-1")
        self.assertEqual(event["turn"]["model"], "openai/gpt-5")
        self.assertEqual(event["turn"]["request_kind"], "primary")
        self.assertEqual(event["turn"]["excerpt"], "fix the bug")

    def test_turn_event_caps_and_drops_fields(self):
        event = build_turn_event("ses-1", None, None, "x" * 900, None)
        self.assertIsNone(event["turn"]["model"])
        self.assertIsNone(event["repo"])
        self.assertEqual(len(event["turn"]["excerpt"]), 500)

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

    def test_missing_receipt_reports_loaded_source_hash(self):
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
    def test_posix_and_windows_paths(self):
        self.assertEqual(repo_name("/home/u/projects/mimir"), "mimir")
        self.assertEqual(repo_name("C:\\Users\\u\\projects\\mimir\\"), "mimir")
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
    def test_heartbeat_does_not_keep_session_alive_forever_and_end_clears_it(self):
        reporter_type = __import__("__init__")._Reporter
        reporter = reporter_type({"url": "https://mimir.example", "token": "tok"}, "repo")
        reporter.touch("ses-1")

        class ImmediateThread:
            def __init__(self, target, args=(), daemon=False):
                self.target, self.args, self.daemon = target, args, daemon

            def start(self):
                self.target(*self.args)

        with patch("urllib.request.urlopen"), patch("threading.Thread", ImmediateThread):
            reporter.heartbeat_if_active()
            reporter.end("ses-1", "finalized")
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
            reporter.deliver(build_turn_event("ses-1", "1", "model", "hi", "repo"), key="turn:1")
        self.assertEqual(post.call_count, 2)


class HookContractTest(unittest.TestCase):
    def test_uses_pre_api_transport_metadata_and_only_finalizes_old_session(self):
        class Context:
            def __init__(self):
                self.hooks = {}

            def register_hook(self, name, callback):
                self.hooks[name] = callback

        ctx = Context()
        delivered = []
        with patch.object(mimir_plugin, "load_connection", return_value={"url": "https://mimir.example", "token": "tok"}), \
             patch.object(mimir_plugin._Reporter, "deliver", lambda _self, event, **_kwargs: delivered.append(event)), \
             patch("threading.Thread.start", return_value=None):
            mimir_plugin.register(ctx)
            ctx.hooks["pre_api_request"](session_id="ses-1", turn_id="turn-1", provider="anthropic", base_url="https://api.anthropic.com")
            ctx.hooks["post_llm_call"](session_id="ses-1", turn_id="turn-1", model="claude", user_message="hello")
            ctx.hooks["pre_api_request"](session_id="ses-1", turn_id="turn-2", provider="openrouter", base_url="https://mimir.example/v1/hermes")
            ctx.hooks["post_llm_call"](session_id="ses-1", turn_id="turn-2", model="openrouter/model", user_message="hello")
        self.assertEqual([event["turn"]["exchange_id"] for event in delivered if event["kind"] == "turn"], ["turn-1"])
        self.assertIn("on_session_finalize", ctx.hooks)
        self.assertNotIn("on_session_reset", ctx.hooks)


if __name__ == "__main__":
    unittest.main()
