package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

func TestSessionGitCaptureExactCommit(t *testing.T) {
	isolatedInstallation(t, false)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git command failed: %v: %s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-q")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "example.txt"), []byte("api_key=supersecretvalue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "example.txt")
	git("commit", "-qm", "capture work")
	sha := git("rev-parse", "HEAD")
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing machine authentication")
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /sessions/session-1/status":
			_, _ = w.Write([]byte(`{"session_id":"session-1"}`))
		case "POST /sessions/session-1/git-artifacts":
			var body struct {
				Version int `json:"version"`
				Commits []struct {
					CommitSHA string `json:"commit_sha"`
					Patch     string `json:"patch"`
				} `json:"commits"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Version != 1 || len(body.Commits) != 1 || body.Commits[0].CommitSHA != sha || strings.Contains(body.Commits[0].Patch, "supersecretvalue") || !strings.Contains(body.Commits[0].Patch, "[REDACTED]") {
				t.Error("invalid or unredacted commit upload")
			}
			_, _ = w.Write([]byte(`{"artifacts":[{}],"duplicates":0}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"session", "git", "capture", "session-1", sha, "--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "GET /sessions/session-1/status,POST /sessions/session-1/git-artifacts" {
		t.Fatalf("requests: %v", methods)
	}
	var result struct {
		SessionID string `json:"session_id"`
		CommitSHA string `json:"commit_sha"`
		Saved     int    `json:"artifacts_saved"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.SessionID != "session-1" || result.CommitSHA != sha || result.Saved != 1 {
		t.Fatalf("result: %q (%v)", output.String(), err)
	}
}

func TestSessionGitCaptureRejectsInvalidSHAAndMissingSession(t *testing.T) {
	isolatedInstallation(t, false)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"session not found"}`))
	}))
	defer server.Close()
	if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"session", "git", "capture", "session-1", "HEAD", "--json"},
		{"session", "git", "capture", "session-1", strings.Repeat("A", 40)},
		{"session", "git", "capture", "session-1", strings.Repeat("a", 40), "--yes"},
	} {
		if err := ExecuteIO(context.Background(), args, IO{Out: &bytes.Buffer{}}); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid args made %d network requests", requests)
	}
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"session", "git", "capture", "session-1", strings.Repeat("a", 40), "--json"}, IO{Out: &output}); err == nil {
		t.Fatal("missing session accepted")
	}
	if requests != 1 || output.Len() != 0 {
		t.Fatalf("requests=%d output=%q", requests, output.String())
	}
}
