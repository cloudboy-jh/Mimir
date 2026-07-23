import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from __init__ import __testing  # noqa: E402

parse_mimir_config = __testing["parse_mimir_config"]
resolve_connection = __testing["resolve_connection"]
repo_name = __testing["repo_name"]
build_turn_event = __testing["build_turn_event"]
build_simple_event = __testing["build_simple_event"]
liveness_only = __testing["liveness_only"]


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
        event = build_turn_event("ses-1", "openai/gpt-5", "fix the bug", "mimir")
        self.assertEqual(event["version"], 1)
        self.assertEqual(event["kind"], "turn")
        self.assertEqual(event["session_id"], "ses-1")
        self.assertEqual(event["harness"], "hermes")
        self.assertEqual(event["repo"], "mimir")
        self.assertEqual(event["turn"]["model"], "openai/gpt-5")
        self.assertEqual(event["turn"]["request_kind"], "primary")
        self.assertEqual(event["turn"]["excerpt"], "fix the bug")

    def test_turn_event_caps_and_drops_fields(self):
        event = build_turn_event("ses-1", None, "x" * 900, None)
        self.assertIsNone(event["turn"]["model"])
        self.assertIsNone(event["repo"])
        self.assertEqual(len(event["turn"]["excerpt"]), 500)

    def test_simple_event(self):
        event = build_simple_event("end", "ses-1", "mimir", reason="harness exit")
        self.assertEqual(event["kind"], "end")
        self.assertEqual(event["reason"], "harness exit")
        heartbeat = build_simple_event("heartbeat", "ses-1", "mimir")
        self.assertNotIn("reason", heartbeat)


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


if __name__ == "__main__":
    unittest.main()
