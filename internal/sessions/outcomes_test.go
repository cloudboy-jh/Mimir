package sessions

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fakeGitEvidence struct {
	commits    []string
	changed    map[string][]string
	branches   map[string][]string
	patches    map[string]string
	repository string
	ref        string
}

func (g fakeGitEvidence) CommitsSince(context.Context, time.Time) ([]string, error) {
	return g.commits, nil
}

func (g fakeGitEvidence) FilesChanged(_ context.Context, commit string) ([]string, error) {
	return g.changed[commit], nil
}

func (g fakeGitEvidence) RemoteBranchesContaining(_ context.Context, commit string) ([]string, error) {
	return g.branches[commit], nil
}

func (g fakeGitEvidence) Patch(_ context.Context, commit string) (string, error) {
	return g.patches[commit], nil
}
func (g fakeGitEvidence) RepositoryURL(context.Context) (string, error) { return g.repository, nil }
func (g fakeGitEvidence) Ref(context.Context) (string, error)           { return g.ref, nil }

func TestSetGitOutcomeRecordsLandedEvidence(t *testing.T) {
	var outcomePath string
	var outcomeBody map[string]any
	service := New(requestFunc(func(_ context.Context, method, path string, body any) ([]byte, error) {
		if method == "GET" {
			if path != "/sessions/owner%2Fsession" {
				t.Fatalf("get path = %q", path)
			}
			return []byte(`{"session":{"id":"owner/session","started_at":"2026-07-12T18:06:00Z"},"files":["src/auth/login.go"]}`), nil
		}
		outcomePath = path
		outcomeBody = body.(map[string]any)
		return []byte(`{"ok":true}`), nil
	}))
	data, err := service.SetGitOutcome(context.Background(), "owner/session", fakeGitEvidence{
		commits:    []string{"abc123"},
		changed:    map[string][]string{"abc123": {"src/auth/login.go"}},
		branches:   map[string][]string{"abc123": {"origin/main"}},
		patches:    map[string]string{"abc123": "diff --git a/src/auth/login.go b/src/auth/login.go\n+fixed\n"},
		repository: "https://github.com/owner/repo",
		ref:        "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ok":true}` || outcomePath != "/sessions/owner%2Fsession/outcome" {
		t.Fatalf("data=%s path=%q", data, outcomePath)
	}
	evidence, ok := outcomeBody["evidence"].(map[string]any)
	if outcomeBody["outcome"] != "landed" || outcomeBody["source"] != "agent" || !ok || evidence["commit"] != "abc123" || evidence["repository_url"] != "https://github.com/owner/repo" || evidence["ref"] != "main" || evidence["patch"] == "" {
		t.Fatalf("body = %#v", outcomeBody)
	}
}

func TestSetGitOutcomeRecordsUnresolvedWithoutDurableCommit(t *testing.T) {
	var body map[string]any
	service := New(requestFunc(func(_ context.Context, method, _ string, value any) ([]byte, error) {
		if method == "GET" {
			return []byte(`{"session":{"id":"session-1","started_at":"2026-07-12T18:06:00Z"},"files":["src/auth/login.go"]}`), nil
		}
		body = value.(map[string]any)
		return []byte(`{}`), nil
	}))
	_, err := service.SetGitOutcome(context.Background(), "session-1", fakeGitEvidence{
		commits:  []string{"abc123"},
		changed:  map[string][]string{"abc123": {"src/store.go"}},
		branches: map[string][]string{"abc123": {"origin/main"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["outcome"] != "unresolved" || body["evidence"] != "session started at 2026-07-12T18:06:00Z" {
		t.Fatalf("body = %#v", body)
	}
}

func TestSetOutcomePreservesExplicitNullEvidence(t *testing.T) {
	var body map[string]any
	service := New(requestFunc(func(_ context.Context, _, path string, value any) ([]byte, error) {
		if path != "/sessions/session%2F1/outcome" {
			t.Fatalf("path = %q", path)
		}
		body = value.(map[string]any)
		return []byte(`{"future":"kept"}`), nil
	}))
	data, err := service.SetOutcome(context.Background(), "session/1", SetOutcomeOptions{Outcome: "landed", EvidenceSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := body["evidence"]; !exists || body["evidence"] != nil || !json.Valid(data) {
		t.Fatalf("body=%#v data=%s", body, data)
	}
}

func TestEndBuildsEscapedRequestAndReturnsStatus(t *testing.T) {
	var body map[string]any
	service := New(requestFunc(func(_ context.Context, method, path string, value any) ([]byte, error) {
		switch method {
		case "POST":
			if path != "/sessions/session%2F1/end" {
				t.Fatalf("end path = %q", path)
			}
			body = value.(map[string]any)
			return []byte(`{"session":{"id":"session/1"}}`), nil
		case "GET":
			if path != "/sessions/session%2F1/status" {
				t.Fatalf("status path = %q", path)
			}
			return []byte(`{"session_id":"session/1","capture":{"status":"saved","saved_exchanges":1},"receipt":{"label":"Saved to Mimir","detail":"1 exchange in this session"},"outcome":"landed"}`), nil
		default:
			t.Fatalf("method = %q", method)
			return nil, nil
		}
	}))
	service.PollSchedule = nil
	status, err := service.End(context.Background(), "session/1", EndOptions{Outcome: "landed", Reason: "merged", Evidence: nil, EvidenceSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.SessionID != "session/1" || body["outcome"] != "landed" || body["reason"] != "merged" {
		t.Fatalf("status=%#v body=%#v", status, body)
	}
	if evidence, exists := body["evidence"]; !exists || evidence != nil {
		t.Fatalf("body=%#v", body)
	}
}

func TestOverlapsSessionFiles(t *testing.T) {
	if !overlaps([]string{"src/auth/login.go"}, []string{"src/auth/login.go"}) {
		t.Fatal("exact file did not overlap")
	}
	if overlaps([]string{"src/auth/login.go"}, []string{"src/store.go"}) {
		t.Fatal("unrelated files overlapped")
	}
}

func TestDurableBranch(t *testing.T) {
	if !durableBranch([]string{"origin/main"}) {
		t.Fatal("main should be durable")
	}
	if durableBranch([]string{"origin/feature"}) {
		t.Fatal("feature should not be durable")
	}
}
