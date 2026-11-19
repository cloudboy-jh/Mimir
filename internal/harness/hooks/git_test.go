package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	"github.com/cloudboy-jh/mimir/internal/sessionimport"
)

func TestDecodeGitInput(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "repo")
	valid := `{"session_id":"pi-123","cwd":` + toJSON(cwd) + `,"harness":"pi","shas":["ABCDEF1"],"tool_output":"[main abcdef1] x","refresh":true}`
	input, err := DecodeGitInput(strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	if input.SessionID != "pi-123" || input.CWD != cwd || input.Harness != "pi" || !input.Refresh || len(input.SHAs) != 1 {
		t.Fatalf("input = %#v", input)
	}
	for name, body := range map[string]string{
		"missing session": `{"cwd":` + toJSON(cwd) + `,"harness":"pi"}`,
		"unsafe session":  `{"session_id":"../x","cwd":` + toJSON(cwd) + `,"harness":"pi"}`,
		"relative cwd":    `{"session_id":"s","cwd":"repo","harness":"pi"}`,
		"missing harness": `{"session_id":"s","cwd":` + toJSON(cwd) + `}`,
		"bad sha":         `{"session_id":"s","cwd":` + toJSON(cwd) + `,"harness":"pi","shas":["HEAD"]}`,
		"unknown field":   `{"session_id":"s","cwd":` + toJSON(cwd) + `,"harness":"pi","outcome":"landed"}`,
		"two objects":     `{"session_id":"s","cwd":` + toJSON(cwd) + `,"harness":"pi"}{}`,
	} {
		if _, err := DecodeGitInput(strings.NewReader(body)); err == nil {
			t.Errorf("%s: accepted %s", name, body)
		}
	}
}

func TestCommitHintsFromSHAsAndCommitConfirmations(t *testing.T) {
	output := "[main 1a2b3c4] Fix receipt\n 1 file changed\n[feature (root-commit) 9f8e7d6] Init\nnot [a commit] line\n[main 1A2B3C4] duplicate"
	got := commitHints([]string{"ABCDEF0", "1a2b3c4"}, output)
	want := []string{"abcdef0", "1a2b3c4", "9f8e7d6"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("hints = %v, want %v", got, want)
	}
}

type fakeCollector map[string]bool

func (f fakeCollector) CollectCommit(_ context.Context, _ string, sha string) (sessionimport.GitArtifact, error) {
	if !f[sha] {
		return sessionimport.GitArtifact{}, errors.New("exact commit not found in local checkout")
	}
	return sessionimport.GitArtifact{CommitSHA: sha, CommittedAt: "2026-11-18T10:00:00.000Z", Subject: "s", Patch: "diff --git a/x b/x\n"}, nil
}

func TestCaptureGitSetsPushedAndRefreshesUnpushedCommits(t *testing.T) {
	committed := strings.Repeat("a", 40)
	pushedNow := strings.Repeat("b", 40)
	stillLocal := strings.Repeat("c", 40)
	var posts []map[string]any
	var postPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions/s-1":
			_, _ = w.Write([]byte(`{"commits":[
				{"session_id":"s-1","commit_sha":"` + committed + `","capture_status":"saved","pushed":false},
				{"session_id":"child-1","commit_sha":"` + pushedNow + `","capture_status":"saved","pushed":false},
				{"session_id":"s-1","commit_sha":"` + stillLocal + `","capture_status":"saved","pushed":false}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-artifacts"):
			data, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(data, &body)
			posts = append(posts, body)
			postPaths = append(postPaths, r.URL.Path)
			commits := body["commits"].([]any)
			artifacts := make([]map[string]any, len(commits))
			for i := range commits {
				artifacts[i] = map[string]any{"commit_sha": commits[i].(map[string]any)["commit_sha"]}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts, "duplicates": 0})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := mimirapi.Client{HTTPClient: server.Client(), Pointer: mimirapi.Pointer{URL: server.URL, Token: "t"}}
	cwd := t.TempDir()
	service := Service{
		API:       &client,
		Collector: fakeCollector{committed: true, pushedNow: true, stillLocal: true},
		Git: func(_ context.Context, dir string, args ...string) (string, error) {
			if dir != cwd {
				t.Errorf("git dir = %q", dir)
			}
			switch strings.Join(args[:2], " ") {
			case "rev-parse --verify":
				if args[2] == "aaaaaaa^{commit}" {
					return committed, nil
				}
				return "", errors.New("unknown revision")
			case "branch -r":
				if args[3] == pushedNow {
					return "  origin/main", nil
				}
				return "", nil
			}
			t.Errorf("git %v", args)
			return "", nil
		},
	}
	result := service.CaptureGit(context.Background(), GitInput{
		SessionID: "s-1", CWD: cwd, Harness: "pi",
		ToolOutput: "[main aaaaaaa] Fix\n[main 1234567] missing", Refresh: true,
	})
	if result.Captured != 2 || result.Pushed != 1 || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "1234567") {
		t.Fatalf("result = %#v", result)
	}
	if len(posts) != 2 || postPaths[0] != "/sessions/s-1/git-artifacts" || postPaths[1] != "/sessions/child-1/git-artifacts" {
		t.Fatalf("posts %v %v", postPaths, posts)
	}
	first := posts[0]["commits"].([]any)[0].(map[string]any)
	if first["commit_sha"] != committed || first["pushed"] != false {
		t.Fatalf("captured commit %#v", first)
	}
	refreshed := posts[1]["commits"].([]any)
	if len(refreshed) != 1 || refreshed[0].(map[string]any)["commit_sha"] != pushedNow || refreshed[0].(map[string]any)["pushed"] != true {
		t.Fatalf("refreshed commits %#v", refreshed)
	}
}

func TestCaptureGitWithoutConnectionReportsError(t *testing.T) {
	result := Service{}.CaptureGit(context.Background(), GitInput{SessionID: "s", CWD: t.TempDir(), Harness: "pi", SHAs: []string{"abcdef0"}})
	if result.Captured != 0 || len(result.Errors) != 1 {
		t.Fatalf("result = %#v", result)
	}
}
