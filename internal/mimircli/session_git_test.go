package mimircli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

func gitRepairCheckout(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v: %s", err, data)
		}
		return strings.TrimSpace(string(data))
	}
	git("init", "-q")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "example.txt"), []byte("first\nsecond\napi_key=supersecretvalue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "exact older commit")
	sha := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "later.txt"), []byte("later\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "later unrelated commit")
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return sha
}

func canonicalGitDetail(id, sha, patch string) map[string]any {
	files, additions, deletions := 0, 0, 0
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files++
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			additions++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			deletions++
		}
	}
	return map[string]any{"session": map[string]any{"id": id}, "git_artifacts": []map[string]any{{
		"commit_sha": sha, "capture_status": "saved", "patch_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(patch))),
		"patch_bytes": len(patch), "patch_files": files, "patch_additions": additions, "patch_deletions": deletions,
	}}}
}

func TestSessionGitRepairContractAndIndependentVerification(t *testing.T) {
	for _, failure := range []string{"", "server-redaction", "conflict", "readback", "different-patch", "wrong-session", "missing-session", "missing-commit",
		"response-session", "response-sha", "response-status", "response-unsaved", "response-digest", "response-audit", "response-repaired-sha", "response-previous-digest", "response-repaired-digest", "response-artifacts"} {
		t.Run(failure, func(t *testing.T) {
			isolatedInstallation(t, false)
			sha := gitRepairCheckout(t)
			if failure == "missing-commit" {
				sha = strings.Repeat("0", 40)
			}
			id := "session /?#% exact"
			oldDigest := strings.Repeat("b", 64)
			var patch string
			var localDigest string
			var routes []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing bearer")
				}
				if r.URL.RawQuery != "" || !strings.Contains(r.URL.EscapedPath(), "session%20%2F%3F%23%25%20exact") {
					t.Errorf("ID not escaped: %s", r.URL)
				}
				route := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/sessions/"+id)
				routes = append(routes, route)
				switch route {
				case "GET /status":
					if failure == "missing-session" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					statusID := id
					if failure == "wrong-session" {
						statusID = "other"
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"session_id": statusID})
				case "POST /git-artifacts/" + sha + "/repair":
					var body struct {
						ExpectedDigest string                    `json:"expected_digest"`
						Artifact       sessionimport.GitArtifact `json:"artifact"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					patch = body.Artifact.Patch
					localDigest = fmt.Sprintf("%x", sha256.Sum256([]byte(patch)))
					if body.ExpectedDigest != oldDigest || body.Artifact.CommitSHA != sha || body.Artifact.Subject != "exact older commit" || strings.Contains(patch, "supersecretvalue") || !strings.Contains(patch, "[REDACTED]") || !strings.Contains(patch, "\n+second\n") {
						t.Errorf("invalid repair body: %+v", body)
					}
					if failure == "conflict" {
						w.WriteHeader(http.StatusConflict)
						_, _ = w.Write([]byte(`{"error":"digest conflict"}`))
						return
					}
					if failure == "server-redaction" {
						patch = strings.ReplaceAll(patch, "second", "[REDACTED]")
						if patch == body.Artifact.Patch {
							t.Error("server redaction did not change patch")
						}
					}
					artifact := canonicalGitDetail(id, sha, patch)["git_artifacts"].([]map[string]any)[0]
					repaired := map[string]any{"commit_sha": sha, "previous_digest": oldDigest,
						"patch_sha256": artifact["patch_sha256"], "audit_r2_key": "sessions/repair.patch.audit.json", "duplicate": false}
					response := map[string]any{"kind": "ok", "session_id": id, "artifacts": []map[string]any{artifact}, "duplicates": 0, "repaired": repaired}
					switch failure {
					case "response-session":
						response["session_id"] = "other"
					case "response-sha":
						artifact["commit_sha"] = strings.Repeat("c", 40)
					case "response-status":
						response["kind"] = "failed"
					case "response-unsaved":
						artifact["capture_status"] = "accepted"
					case "response-digest":
						artifact["patch_sha256"] = "invalid"
					case "response-audit":
						delete(repaired, "audit_r2_key")
					case "response-repaired-sha":
						repaired["commit_sha"] = strings.Repeat("c", 40)
					case "response-previous-digest":
						repaired["previous_digest"] = strings.Repeat("c", 64)
					case "response-repaired-digest":
						repaired["patch_sha256"] = strings.Repeat("c", 64)
					case "response-artifacts":
						response["artifacts"] = []any{}
					}
					_ = json.NewEncoder(w).Encode(response)
					if failure == "different-patch" {
						patch = "diff --git a/x b/x\nBinary files a/x and b/x differ\n"
					}
				case "GET ":
					_ = json.NewEncoder(w).Encode(canonicalGitDetail(id, sha, patch))
				case "GET /git-artifacts/" + sha + "/patch":
					if failure == "readback" {
						patch = strings.ReplaceAll(patch, "\n", `\n`)
					}
					_, _ = w.Write([]byte(patch))
				default:
					t.Errorf("unexpected side effect: %s", route)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err := ExecuteIO(context.Background(), []string{"session", "git", "repair", id, sha, "--expected-digest", oldDigest, "--json"}, IO{Out: &output})
			if failure != "" && failure != "server-redaction" {
				if err == nil || output.Len() != 0 {
					t.Fatalf("failed repair reported success: %v %s", err, &output)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					gitVerification
					Repair struct {
						Repaired struct {
							PatchSHA256 string `json:"patch_sha256"`
							AuditR2Key  string `json:"audit_r2_key"`
						} `json:"repaired"`
					} `json:"repair"`
				}
				if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.Verified || result.Repair.Repaired.AuditR2Key == "" || result.PatchSHA256 != result.Repair.Repaired.PatchSHA256 || result.SessionID != id || result.CommitSHA != sha || result.PatchLines < 2 {
					t.Fatalf("invalid result: %s (%v)", &output, err)
				}
				if failure == "server-redaction" && result.PatchSHA256 == localDigest {
					t.Fatal("server-redacted patch retained local digest")
				}
			}
			want := []string{"GET /status"}
			if failure != "wrong-session" && failure != "missing-session" && failure != "missing-commit" {
				want = append(want, "POST /git-artifacts/"+sha+"/repair")
			}
			if failure != "wrong-session" && failure != "missing-session" && failure != "missing-commit" && failure != "conflict" && !strings.HasPrefix(failure, "response-") {
				want = append(want, "GET ", "GET /git-artifacts/"+sha+"/patch")
			}
			if strings.Join(routes, ",") != strings.Join(want, ",") {
				t.Fatalf("routes=%v want=%v", routes, want)
			}
		})
	}
}

func TestSessionGitVerifyIntegrity(t *testing.T) {
	sha := strings.Repeat("a", 40)
	text := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n"
	binary := "diff --git a/x b/x\nBinary files a/x and b/x differ\n"
	for _, scenario := range []string{"text", "binary", "binary-payload", "digest", "stats", "collapsed", "format", "missing", "pending", "wrong-session", "patch-missing"} {
		t.Run(scenario, func(t *testing.T) {
			isolatedInstallation(t, false)
			patch := text
			if scenario == "binary" {
				patch = binary
			}
			if scenario == "binary-payload" {
				patch = "diff --git a/x b/x\nGIT binary patch\nliteral 3\nKcmZQzU|?Vb0000\n"
			}
			if scenario == "collapsed" {
				patch = strings.ReplaceAll(text, "\n", `\n`)
			}
			if scenario == "format" {
				patch = "not a Git patch\nsecond line\n"
			}
			detail := canonicalGitDetail("exact-id", sha, patch)
			artifact := detail["git_artifacts"].([]map[string]any)[0]
			switch scenario {
			case "digest":
				artifact["patch_sha256"] = strings.Repeat("0", 64)
			case "stats":
				artifact["patch_additions"] = 99
			case "pending":
				artifact["capture_status"] = "accepted"
			case "missing":
				artifact["commit_sha"] = strings.Repeat("b", 40)
			case "wrong-session":
				detail["session"] = map[string]string{"id": "other"}
			}
			var routes []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing bearer")
				}
				routes = append(routes, r.Method+" "+r.URL.Path)
				switch r.Method + " " + r.URL.Path {
				case "GET /sessions/exact-id":
					_ = json.NewEncoder(w).Encode(detail)
				case "GET /sessions/exact-id/git-artifacts/" + sha + "/patch":
					if scenario == "patch-missing" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = w.Write([]byte(patch))
				default:
					t.Errorf("unexpected route: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err := ExecuteIO(context.Background(), []string{"session", "git", "verify", "exact-id", sha, "--json"}, IO{Out: &output})
			valid := scenario == "text" || scenario == "binary" || scenario == "binary-payload"
			if valid {
				var result gitVerification
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.Verified || result.PatchBytes != len(patch) {
					t.Fatalf("result %s (%v)", &output, err)
				}
			} else if err == nil || output.Len() != 0 {
				t.Fatalf("accepted invalid patch: %v %s", err, &output)
			}
			want := 2
			if scenario == "pending" || scenario == "missing" || scenario == "wrong-session" {
				want = 1
			}
			if len(routes) != want {
				t.Fatalf("routes: %v", routes)
			}
		})
	}
}

func TestSessionGitRepairVerifyRejectInvalidArguments(t *testing.T) {
	isolatedInstallation(t, false)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	sha, digest := strings.Repeat("a", 40), strings.Repeat("b", 64)
	for _, args := range [][]string{
		{"repair", "id", sha},
		{"repair", "id", sha, "--expected-digest", ""},
		{"repair", "id", sha, "--expected-digest", strings.Repeat("B", 64)},
		{"repair", "id", sha, "--expected-digest", strings.Repeat("b", 63)},
		{"repair", "id", "HEAD", "--expected-digest", digest},
		{"repair", "id", strings.Repeat("A", 40), "--expected-digest", digest},
		{"repair", "", sha, "--expected-digest", digest},
		{"repair", "--recent", sha, "--expected-digest", digest},
		{"repair", "id", sha, "--expected-digest", digest, "--yes"},
		{"verify", "", sha}, {"verify", "id", "HEAD"}, {"verify", "id", sha, "--yes"},
		{"verify", "id\nother", sha},
		{"verify", "id", sha, "--json", "--json"},
	} {
		var output bytes.Buffer
		if err := ExecuteIO(context.Background(), append([]string{"session", "git"}, args...), IO{Out: &output}); err == nil || output.Len() != 0 {
			t.Fatalf("accepted args %v: %v", args, err)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid arguments made %d requests", requests)
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
