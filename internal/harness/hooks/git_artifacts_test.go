package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	"github.com/cloudboy-jh/mimir/internal/sessionimport"
)

func TestLiveCommitCaptureByHarness(t *testing.T) {
	checkout := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = checkout
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, data)
		}
		return string(data)
	}
	git("init")
	if err := os.WriteFile(filepath.Join(checkout, "note.txt"), []byte("test commit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "note.txt")
	output := git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Capture artifact")
	sha := strings.TrimSpace(git("rev-parse", "HEAD"))
	for _, harness := range []string{"claude-code", "codex", "cursor"} {
		t.Run(harness, func(t *testing.T) {
			var sent []Delivery
			service := Service{Home: t.TempDir(), key: storageKey("local-key"), Deliver: func(_ context.Context, d Delivery) error { sent = append(sent, d); return nil }}
			payload := commitPayload(harness, checkout, output)
			feed := func(input map[string]any) {
				t.Helper()
				data, _ := json.Marshal(input)
				if err := service.Ingest(context.Background(), harness, bytes.NewReader(data)); err != nil {
					t.Fatal(err)
				}
			}
			feed(payload)
			feed(payload) // A repeated callback must address the same commit and session.
			if len(sent) != 2 || sent[0].Kind != "git-artifacts" || sent[0].SessionID != "live-session" || sent[0].Repo != filepath.Base(checkout) {
				t.Fatalf("deliveries: %#v", sent)
			}
			var commits []sessionimport.GitArtifact
			encoded, _ := json.Marshal(sent[0].Body["commits"])
			if err := json.Unmarshal(encoded, &commits); err != nil {
				t.Fatal(err)
			}
			if len(commits) != 1 || commits[0].CommitSHA != sha || !strings.Contains(commits[0].Patch, "test commit") {
				t.Fatalf("commits: %#v", commits)
			}
			if sent[0].SessionID != sent[1].SessionID || sent[0].Repo != sent[1].Repo {
				t.Fatalf("retry changed artifact target: %#v", sent)
			}
			second, _ := json.Marshal(sent[1].Body["commits"])
			if !bytes.Equal(encoded, second) {
				t.Fatalf("retry changed artifact: %s != %s", encoded, second)
			}
			for _, mutate := range []func(map[string]any){
				func(m map[string]any) { setCommitOutput(harness, m, "fatal: commit failed") },
				func(m map[string]any) { setCommitOutput(harness, m, strings.Replace(output, sha[:7], "deadbee", 1)) },
				func(m map[string]any) { setCommitCommand(harness, m, "git status") },
			} {
				negative := commitPayload(harness, checkout, output)
				mutate(negative)
				feed(negative)
			}
			if len(sent) != 2 {
				t.Fatalf("false positive deliveries: %#v", sent)
			}
			if harness == "codex" || harness == "claude-code" || harness == "cursor" {
				failure := commitPayload(harness, checkout, output)
				if harness != "cursor" {
					failure["tool_response"] = map[string]any{"exit_code": 1, "stdout": output}
				} else {
					failure["exit_code"] = 1
				}
				feed(failure)
				if len(sent) != 2 {
					t.Fatal("nonzero exit captured")
				}
				if harness != "cursor" {
					failure["tool_response"] = "Exit code: 1\nOutput:\n" + output
					feed(failure)
					if len(sent) != 2 {
						t.Fatal("textual nonzero exit captured")
					}
				}
			}
		})
	}
}

func TestRootCommitCaptureByHarness(t *testing.T) {
	checkout := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = checkout
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, data)
		}
		return string(data)
	}
	git("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(checkout, "first.txt"), []byte("root content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "first.txt")
	output := git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Root commit")
	sha := strings.TrimSpace(git("rev-parse", "HEAD"))
	if !strings.Contains(output, "(root-commit) "+sha[:7]) {
		t.Fatalf("expected Git root-commit output: %q", output)
	}
	if err := os.WriteFile(filepath.Join(checkout, "second.txt"), []byte("later content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "second.txt")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Later commit")
	for _, harness := range []string{"claude-code", "codex", "cursor"} {
		t.Run(harness, func(t *testing.T) {
			var sent []Delivery
			service := Service{Home: t.TempDir(), key: storageKey("key"), Deliver: func(_ context.Context, d Delivery) error { sent = append(sent, d); return nil }}
			payload := commitPayload(harness, checkout, output)
			data, _ := json.Marshal(payload)
			if err := service.Ingest(context.Background(), harness, bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
			if len(sent) != 1 || sent[0].Kind != "git-artifacts" || sent[0].SessionID != "live-session" {
				t.Fatalf("root commit not captured: %#v", sent)
			}
			encoded, _ := json.Marshal(sent[0].Body["commits"])
			var commits []sessionimport.GitArtifact
			if err := json.Unmarshal(encoded, &commits); err != nil || len(commits) != 1 || commits[0].CommitSHA != sha || !strings.Contains(commits[0].Patch, "root content") {
				t.Fatalf("wrong root commit: %v %#v", err, commits)
			}
			for _, bad := range []string{
				strings.Replace(output, sha[:7], "deadbee", 1),
				"[main (root-commit) deadbee] Root commit\n",
			} {
				setCommitOutput(harness, payload, bad)
				data, _ = json.Marshal(payload)
				if err := service.Ingest(context.Background(), harness, bytes.NewReader(data)); err != nil || len(sent) != 1 {
					t.Fatalf("captured non-emitted SHA: %v %#v", err, sent)
				}
			}
		})
	}
}

func TestCursorCommitRejectsFailedStructuredOutput(t *testing.T) {
	checkout := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = checkout
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, data)
		}
		return string(data)
	}
	git("init")
	if err := os.WriteFile(filepath.Join(checkout, "file.txt"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "file.txt")
	output := git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Root")
	var sent []Delivery
	service := Service{Home: t.TempDir(), key: storageKey("key"), Deliver: func(_ context.Context, d Delivery) error { sent = append(sent, d); return nil }}
	for _, response := range []any{
		map[string]any{"exitCode": 1, "stdout": output},
		"Exit code: 1\nOutput:\n" + output,
	} {
		payload := commitPayload("cursor", checkout, "")
		payload["output"] = response
		data, _ := json.Marshal(payload)
		if err := service.Ingest(context.Background(), "cursor", bytes.NewReader(data)); err != nil || len(sent) != 0 {
			t.Fatalf("captured failed Cursor shell result: %v %#v", err, sent)
		}
	}
}

func TestCommitOptionsResolveOnlyReportedCommit(t *testing.T) {
	checkout := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = checkout
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, data)
		}
		return string(data)
	}
	git("init")
	if err := os.WriteFile(filepath.Join(checkout, "file.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "file.txt")
	output := git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "first")
	sha := strings.TrimSpace(git("rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(checkout, "file.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "file.txt")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "second")
	for _, harness := range []string{"claude-code", "codex", "cursor"} {
		t.Run(harness, func(t *testing.T) {
			var sent []Delivery
			service := Service{Home: t.TempDir(), key: storageKey("key"), Deliver: func(_ context.Context, d Delivery) error { sent = append(sent, d); return nil }}
			payload := commitPayload(harness, t.TempDir(), output)
			setCommitCommand(harness, payload, "git -C \""+checkout+"\" -c user.name=Test commit -m first")
			data, _ := json.Marshal(payload)
			if err := service.Ingest(context.Background(), harness, bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
			if len(sent) != 1 || sent[0].Repo != filepath.Base(checkout) {
				t.Fatalf("missing optioned commit: %#v", sent)
			}
			encoded, _ := json.Marshal(sent[0].Body["commits"])
			var commits []sessionimport.GitArtifact
			if err := json.Unmarshal(encoded, &commits); err != nil || len(commits) != 1 || commits[0].CommitSHA != sha {
				t.Fatalf("captured HEAD instead of output SHA: %v %#v", err, commits)
			}
			payload = commitPayload(harness, t.TempDir(), output)
			setCommitCommand(harness, payload, "cd \""+checkout+"\" && git commit -m first")
			data, _ = json.Marshal(payload)
			if err := service.Ingest(context.Background(), harness, bytes.NewReader(data)); err != nil || len(sent) != 2 {
				t.Fatalf("explicit cd commit: %v %#v", err, sent)
			}
			payload = commitPayload(harness, checkout, output)
			setCommitCommand(harness, payload, "cd unknown && cd \""+checkout+"\" && git commit -m first")
			data, _ = json.Marshal(payload)
			if err := service.Ingest(context.Background(), harness, bytes.NewReader(data)); err != nil || len(sent) != 2 {
				t.Fatalf("ambiguous cd captured: %v %#v", err, sent)
			}
			payload = commitPayload(harness, checkout, output)
			setCommitCommand(harness, payload, "git -c user.name=Test commit -m first")
			if harness == "codex" {
				payload["tool_response"] = map[string]any{"exit_code": 2, "stdout": output}
			} else if harness == "cursor" {
				payload["exit_code"] = 2
			} else {
				payload["tool_response"] = map[string]any{"exitCode": 2, "stdout": output}
			}
			data, _ = json.Marshal(payload)
			if err := service.Ingest(context.Background(), harness, bytes.NewReader(data)); err != nil || len(sent) != 2 {
				t.Fatalf("captured failed commit: %v %#v", err, sent)
			}
		})
	}
}

func TestCursorCommitUsesActualWorkingDirectory(t *testing.T) {
	checkout := t.TempDir()
	other := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = checkout
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, data)
		}
		return string(data)
	}
	git("init")
	if err := os.WriteFile(filepath.Join(checkout, "file.txt"), []byte("change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "file.txt")
	output := git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Capture artifact")
	var sent []Delivery
	service := Service{Home: t.TempDir(), key: storageKey("local-key"), Deliver: func(_ context.Context, d Delivery) error { sent = append(sent, d); return nil }}
	feed := func(payload map[string]any) {
		t.Helper()
		data, _ := json.Marshal(payload)
		if err := service.Ingest(context.Background(), "cursor", bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	payload := commitPayload("cursor", checkout, output)
	payload["workspace_roots"] = []any{other, checkout}
	feed(payload)
	if len(sent) != 0 {
		t.Fatal("attributed commit to an ambiguous workspace root")
	}
	payload["tool_input"] = map[string]any{"working_directory": checkout}
	feed(payload)
	if len(sent) != 1 || sent[0].Repo != filepath.Base(checkout) {
		t.Fatalf("wrong checkout attribution: %#v", sent)
	}
}

func commitPayload(harness, cwd, output string) map[string]any {
	m := map[string]any{"hook_event_name": "PostToolUse", "session_id": "live-session", "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": "git commit -m 'Capture artifact'"}, "tool_response": map[string]any{"stdout": output}}
	if harness == "codex" {
		m["tool_response"] = map[string]any{"exit_code": 0, "stdout": output}
	}
	if harness == "cursor" {
		m = map[string]any{"hook_event_name": "afterShellExecution", "conversation_id": "live-session", "workspace_roots": []any{cwd}, "command": "git commit -m 'Capture artifact'", "output": output}
	}
	return m
}

func setCommitOutput(harness string, m map[string]any, output string) {
	if harness == "cursor" {
		m["output"] = output
	} else {
		m["tool_response"].(map[string]any)["stdout"] = output
	}
}

func setCommitCommand(harness string, m map[string]any, command string) {
	if harness == "cursor" {
		m["command"] = command
	} else {
		m["tool_input"].(map[string]any)["command"] = command
	}
}

func TestGitArtifactOutboxEncryptsAndRetries(t *testing.T) {
	home := t.TempDir()
	artifact := sessionimport.GitArtifact{CommitSHA: strings.Repeat("a", 40), Patch: "private patch"}
	service := Service{Home: home, key: storageKey("key"), Deliver: func(context.Context, Delivery) error { return errors.New("offline") }}
	if err := service.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	d := Delivery{Kind: "git-artifacts", Harness: "codex", SessionID: "s1", Body: map[string]any{"version": 1, "commits": []sessionimport.GitArtifact{artifact}}}
	if err := service.queue(d); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(home, "hook-outbox"))
	data, err := os.ReadFile(filepath.Join(home, "hook-outbox", entries[0].Name()))
	if err != nil || bytes.Contains(data, []byte("private patch")) {
		t.Fatalf("unencrypted outbox: %v", err)
	}
	if err := service.Flush(context.Background()); err == nil {
		t.Fatal("expected offline delivery failure")
	}
	service.Deliver = func(_ context.Context, got Delivery) error {
		if got.Kind != d.Kind || got.SessionID != d.SessionID {
			t.Fatalf("replay: %#v", got)
		}
		return nil
	}
	if err := service.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, _ = os.ReadDir(filepath.Join(home, "hook-outbox"))
	if len(entries) != 0 {
		t.Fatalf("remaining: %d", len(entries))
	}
}

func TestGitArtifactDeliveryUsesAuthenticatedUpload(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/sessions/live/git-artifacts" || r.Header.Get("Authorization") != "Bearer machine-token" || r.Header.Get("X-Mimir-Harness") != "codex" || r.Header.Get("X-Mimir-Repo") != "repo" {
			t.Errorf("request: %s %v", r.URL.Path, r.Header)
		}
		var body struct {
			Version int                         `json:"version"`
			Commits []sessionimport.GitArtifact `json:"commits"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Version != 1 || len(body.Commits) != 1 {
			t.Errorf("body: %#v, %v", body, err)
		}
		_, _ = w.Write([]byte(`{"artifacts":[{}],"duplicates":0}`))
	}))
	defer server.Close()
	client := mimirapi.New(mimirapi.Pointer{URL: server.URL, Token: "machine-token"})
	artifact := sessionimport.GitArtifact{CommitSHA: strings.Repeat("a", 40), Patch: "patch"}
	delivery := Delivery{Kind: "git-artifacts", Harness: "codex", SessionID: "live", Repo: "repo", Body: map[string]any{"commits": []sessionimport.GitArtifact{artifact}}}
	if err := hookDeliver(client)(context.Background(), delivery); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("upload was not called")
	}
}

func TestManifestsRegisterPostToolEvents(t *testing.T) {
	for path, event := range map[string]string{
		"../../../plugins/claude-code/hooks/hooks.json": "PostToolUse",
		"../../../plugins/codex/hooks/hooks.json":       "PostToolUse",
		"../../../plugins/cursor/hooks.json":            "afterShellExecution",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Hooks map[string]json.RawMessage `json:"hooks"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(manifest.Hooks[event], []byte("mimir _hook")) {
			t.Fatalf("%s missing %s handler", path, event)
		}
	}
}
