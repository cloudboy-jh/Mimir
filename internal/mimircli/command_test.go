package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestExecuteVersion(t *testing.T) {
	isolatedInstallation(t, false)
	oldVersion, oldCommit, oldDate := version, commit, date
	t.Cleanup(func() { SetBuildInfo(oldVersion, oldCommit, oldDate) })
	SetBuildInfo("1.2.3", "abc123", "2026-07-15")

	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"version"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "1.2.3 (abc123)\n"; got != want {
		t.Fatalf("version output %q, want %q", got, want)
	}
}

func TestExecuteBinaryVersionIgnoresMalformedReceipt(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if err := os.MkdirAll(filepath.Dir(paths.Receipt), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Receipt, []byte("{malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldVersion, oldCommit, oldDate := version, commit, date
	t.Cleanup(func() { SetBuildInfo(oldVersion, oldCommit, oldDate) })
	SetBuildInfo("4.5.6", "release", "2026-07-23")
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"--version"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "4.5.6 (release)\n" {
		t.Fatalf("--version output = %q", got)
	}
	if err := ExecuteIO(context.Background(), []string{"version"}, IO{Out: &bytes.Buffer{}}); err == nil {
		t.Fatal("version unexpectedly ignored malformed install receipt")
	}
}

func TestExecuteVersionJSONIncludesInstallState(t *testing.T) {
	isolatedInstallation(t, false)
	if _, err := syncManagedArtifacts(true, "install"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"version", "--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var report versionReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.BundleVersion == "" || report.ReceiptPath == "" || report.ArtifactCounts[artifactCurrent] == 0 {
		t.Fatalf("version report %#v", report)
	}
}

func TestExecuteInstallJSONEnrollsArtifacts(t *testing.T) {
	paths := isolatedInstallation(t, false)
	binDir := t.TempDir()
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"install", "--bin-dir", binDir, "--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var report installReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Artifacts.Operation != "install" || len(report.Artifacts.Artifacts) == 0 {
		t.Fatalf("install report %#v", report)
	}
	name := "mimir"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if report.Binary.Path != filepath.Join(binDir, name) || report.Binary.Hash == "" {
		t.Fatalf("binary report %#v", report.Binary)
	}
	if _, err := os.Stat(paths.Receipt); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteUninstallJSONReportsBinaryAndArtifacts(t *testing.T) {
	isolatedInstallation(t, false)
	binary := filepath.Join(t.TempDir(), "mimir")
	binaryData := []byte("managed binary")
	if err := os.WriteFile(binary, binaryData, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := syncInstallArtifacts(installReceiptUpdate{
		Source: "go-run", Method: "bootstrap-copy",
		CLI: installReceiptCLI{Path: binary, Hash: hashBytes(binaryData)},
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"uninstall", "--keep-binary", "--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var report uninstallReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Operation != "uninstall" || report.Binary.Status != "kept" || len(report.Artifacts) == 0 {
		t.Fatalf("uninstall report %#v", report)
	}
	for _, artifact := range report.Artifacts {
		if artifact.Status != artifactRemoved {
			t.Fatalf("artifact status = %s", artifact.Status)
		}
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("--keep-binary removed binary: %v", err)
	}
}

func TestResolveInstallDirPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MIMIR_INSTALL_DIR", filepath.Join(home, "mimir-bin"))
	t.Setenv("GOBIN", filepath.Join(home, "go-bin"))
	t.Setenv("GOPATH", filepath.Join(home, "go-path")+string(os.PathListSeparator)+filepath.Join(home, "other"))
	if got, _ := resolveInstallDir(""); got != filepath.Join(home, "mimir-bin") {
		t.Fatalf("MIMIR_INSTALL_DIR target = %q", got)
	}
	t.Setenv("MIMIR_INSTALL_DIR", "")
	if got, _ := resolveInstallDir(""); got != filepath.Join(home, "go-bin") {
		t.Fatalf("GOBIN target = %q", got)
	}
	t.Setenv("GOBIN", "")
	if got, _ := resolveInstallDir(""); got != filepath.Join(home, "go-path", "bin") {
		t.Fatalf("GOPATH target = %q", got)
	}
	t.Setenv("GOPATH", "")
	if got, _ := resolveInstallDir(""); got != filepath.Join(home, "go", "bin") {
		t.Fatalf("home target = %q", got)
	}
}

func TestInstallExecutableCopyFreshAndReplacement(t *testing.T) {
	target := filepath.Join(t.TempDir(), "new", "mimir")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	if err := installExecutableCopy(target, []byte("first"), ""); err != nil {
		t.Fatal(err)
	}
	if err := installExecutableCopy(target, []byte("second"), hashBytes([]byte("first"))); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "second" {
		t.Fatalf("installed binary = %q, %v", data, err)
	}
}

func TestBootstrapCurrentExecutableDoesNotRewriteTarget(t *testing.T) {
	paths := isolatedInstallation(t, false)
	dir := t.TempDir()
	name := "mimir"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(dir, name)
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(target, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	oldExecutablePath := executablePath
	executablePath = func() (string, error) { return target, nil }
	t.Cleanup(func() { executablePath = oldExecutablePath })
	receipt := newInstallReceipt()
	receipt.Source = "release"
	receipt.Method = "bootstrap-copy"
	receipt.CLI = installReceiptCLI{Path: target, Hash: hashBytes([]byte("binary"))}
	if err := writeJSONAtomic(paths.Receipt, receipt); err != nil {
		t.Fatal(err)
	}
	report, err := bootstrapCurrentExecutable(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "current" || report.Source != "release" || report.Method != "bootstrap-copy" || !info.ModTime().Equal(stamp) {
		t.Fatalf("report=%#v modtime=%s", report, info.ModTime())
	}
}

func TestBootstrapCurrentExecutableRejectsUnownedDifferentTarget(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	name := "mimir"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	source := filepath.Join(sourceDir, name)
	target := filepath.Join(targetDir, name)
	if err := os.WriteFile(source, []byte("new binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("someone else's binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExecutablePath := executablePath
	executablePath = func() (string, error) { return source, nil }
	t.Cleanup(func() { executablePath = oldExecutablePath })
	if _, err := bootstrapCurrentExecutable(targetDir); err == nil || !strings.Contains(err.Error(), "unowned executable") {
		t.Fatalf("error = %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "someone else's binary" {
		t.Fatalf("unowned target changed to %q", got)
	}
}

func TestInstallExecutableCopyRejectsChangedOwnedTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "mimir")
	if err := os.WriteFile(target, []byte("changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installExecutableCopy(target, []byte("new"), hashBytes([]byte("receipt bytes"))); err == nil || !strings.Contains(err.Error(), "no longer matches") {
		t.Fatalf("error = %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "changed" {
		t.Fatalf("target changed to %q", got)
	}
}

func TestPostUpdateCommandUsesJSONProtocol(t *testing.T) {
	isolatedInstallation(t, false)
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"_post-update"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var report lifecycleIntegrationReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Artifacts.Operation != "update" || !strings.Contains(report.Integrations.OpenCode.Detail, "without rewriting") {
		t.Fatalf("post-update report %#v", report)
	}
}

func TestResolveBuildInfoFromGoInstallMetadata(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef1234567890"},
			{Key: "vcs.time", Value: "2026-07-15T12:00:00Z"},
		},
	}

	gotVersion, gotCommit, gotDate := resolveBuildInfo("0.0.0-dev", "unknown", "unknown", info)
	if gotVersion != "1.2.3" || gotCommit != "abcdef123456" || gotDate != "2026-07-15T12:00:00Z" {
		t.Fatalf("build info = %q, %q, %q", gotVersion, gotCommit, gotDate)
	}
}

func TestResolveBuildInfoKeepsLinkerValues(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "metadata-commit"},
			{Key: "vcs.time", Value: "metadata-date"},
		},
	}

	gotVersion, gotCommit, gotDate := resolveBuildInfo("1.2.3", "release-commit", "release-date", info)
	if gotVersion != "1.2.3" || gotCommit != "release-commit" || gotDate != "release-date" {
		t.Fatalf("build info = %q, %q, %q", gotVersion, gotCommit, gotDate)
	}
}

func TestExecuteUsage(t *testing.T) {
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), nil, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "mimir setup [--quick] [--json]") {
		t.Fatalf("usage output %q", output.String())
	}
}

func TestParseRecallArgs(t *testing.T) {
	query, budget, jsonOut, err := parseRecallArgs([]string{"session", "storage", "--budget", "1200", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(query, " "); got != "session storage" || budget != 1200 || !jsonOut {
		t.Fatalf("query=%q budget=%d json=%v", got, budget, jsonOut)
	}
}

func TestParseSessionOutcomeArgs(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		reason string
	}{
		{name: "without reason", args: []string{"session-1", "landed"}},
		{name: "reason as next argument", args: []string{"session-1", "discarded", "--reason", "superseded"}, reason: "superseded"},
		{name: "reason with equals", args: []string{"session-1", "abandoned", "--reason=no owner"}, reason: "no owner"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id, outcome, reason, err := parseSessionOutcomeArgs(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if id != "session-1" || outcome != test.args[1] || reason != test.reason {
				t.Fatalf("id=%q outcome=%q reason=%q", id, outcome, reason)
			}
		})
	}
	if _, _, _, err := parseSessionOutcomeArgs([]string{"session-1", "promoted"}); err == nil {
		t.Fatal("canonical command accepted legacy outcome")
	}
}

func TestExecuteSessionCommandsAndReconcile(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
		wantBody   map[string]any
	}{
		{name: "existing session get", args: []string{"session", "session-1"}, wantMethod: http.MethodGet, wantPath: "/sessions/session-1"},
		{name: "session outcome", args: []string{"session", "outcome", "session-1", "landed", "--reason", "merged to main"}, wantMethod: http.MethodPost, wantPath: "/sessions/session-1/outcome", wantBody: map[string]any{"outcome": "landed", "source": "agent", "reason": "merged to main"}},
		{name: "legacy mark", args: []string{"mark", "session-1", "promoted"}, wantMethod: http.MethodPost, wantPath: "/sessions/session-1/mark", wantBody: map[string]any{"outcome": "promoted"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != test.wantMethod || r.URL.Path != test.wantPath {
					t.Fatalf("request %s %s", r.Method, r.URL.Path)
				}
				if test.wantBody != nil {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if got, want := mustJSON(t, body), mustJSON(t, test.wantBody); got != want {
						t.Fatalf("body %s, want %s", got, want)
					}
				}
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			t.Setenv(envMimirHome, t.TempDir())
			if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := ExecuteIO(context.Background(), test.args, IO{Out: &output}); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); got != "{\n  \"ok\": true\n}\n" {
				t.Fatalf("formatted output %q", got)
			}
		})
	}
}

func TestExecuteSessionStatusHumanAndJSON(t *testing.T) {
	oldSchedule := sessionStatusPollSchedule
	sessionStatusPollSchedule = []time.Duration{0}
	t.Cleanup(func() { sessionStatusPollSchedule = oldSchedule })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/sessions/session-1/status" {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"session_id":"session-1","capture":{"status":"saved","saved_exchanges":14,"failed_exchanges":1,"pending_exchanges":0,"last_saved_at":"2026-07-15T23:42:00Z"},"receipt":{"label":"Saved to Mimir","detail":"14 exchanges in this session","action_label":"View session"},"dashboard_url":"https://mimir.example/dashboard/sessions/session-1","outcome":"unresolved"}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	var human bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"session", "status", "session-1"}, IO{Out: &human}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"Saved to Mimir · 14 exchanges in this session", "Session   session-1", "Capture   Saved", "Saved     14", "Failed    1", "Outcome   Unresolved", "Dashboard https://mimir.example/dashboard/sessions/session-1"} {
		if !strings.Contains(human.String(), value) {
			t.Fatalf("human status missing %q: %s", value, human.String())
		}
	}
	var machine bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"session", "status", "session-1", "--json"}, IO{Out: &machine}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(machine.String(), `"saved_exchanges": 14`) {
		t.Fatalf("JSON status %s", machine.String())
	}
}

func TestExecuteSessionStatusRequiresID(t *testing.T) {
	err := ExecuteIO(context.Background(), []string{"session", "status"}, IO{Out: &bytes.Buffer{}})
	if err == nil || err.Error() != "usage: mimir session status <id> [--json]" {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteSessionEndRequiresID(t *testing.T) {
	err := ExecuteIO(context.Background(), []string{"session", "end"}, IO{Out: &bytes.Buffer{}})
	if err == nil || !strings.HasPrefix(err.Error(), "usage: mimir session end <id>") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteSessionEnd(t *testing.T) {
	oldSchedule := sessionStatusPollSchedule
	sessionStatusPollSchedule = []time.Duration{0}
	t.Cleanup(func() { sessionStatusPollSchedule = oldSchedule })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sessions/session-1/end":
			if r.Method != http.MethodPost {
				t.Fatalf("method %s", r.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if got := mustJSON(t, body); got != mustJSON(t, map[string]any{"outcome": "landed", "reason": "verified"}) {
				t.Fatalf("body %s", got)
			}
			_, _ = w.Write([]byte(`{"session":{"id":"session-1","state":"inactive"}}`))
		case "/sessions/session-1/status":
			_, _ = w.Write([]byte(`{"session_id":"session-1","capture":{"status":"saved","saved_exchanges":1,"failed_exchanges":0,"pending_exchanges":0},"receipt":{"label":"Saved to Mimir","detail":"1 exchange in this session"},"outcome":"landed"}`))
		default:
			t.Fatalf("path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"session", "end", "session-1", "--outcome", "landed", "--reason", "verified"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Session ended · Saved to Mimir · 1 exchange in this session\n" {
		t.Fatalf("output %q", output.String())
	}
	if _, _, err := parseSessionEndArgs([]string{"session-1", "--reason", "missing outcome"}); err == nil {
		t.Fatal("reason without outcome was accepted")
	}
}

func TestExecuteReconcileExhaustsDatabaseAndR2Cursors(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/reconcile" {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		if calls == 1 {
			_, _ = w.Write([]byte(`{"scanned":1,"database_cursor":"db-next","finalized":{"exchange_ids":["saved-1"]},"pending":{"exchange_ids":[],"stale_exchange_ids":[]},"missing_saved":{"exchange_ids":[],"session_ids":[]},"orphans":{"r2_keys":["log/orphan-1.json"],"cursor":"r2-next"}}`))
			return
		}
		if calls == 2 {
			if r.URL.Query().Get("database_cursor") != "db-next" || r.URL.Query().Get("cursor") != "r2-next" {
				t.Fatalf("continuation query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"scanned":1,"database_cursor":null,"finalized":{"exchange_ids":[]},"pending":{"exchange_ids":["pending-1"],"stale_exchange_ids":["pending-1"]},"missing_saved":{"exchange_ids":[],"session_ids":[]},"orphans":{"r2_keys":["log/orphan-2.json"],"cursor":"r2-final"}}`))
			return
		}
		if r.URL.Query().Get("scan_database") != "false" || r.URL.Query().Get("cursor") != "r2-final" {
			t.Fatalf("R2 continuation query %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"scanned":0,"database_cursor":null,"finalized":{"exchange_ids":[]},"pending":{"exchange_ids":[],"stale_exchange_ids":[]},"missing_saved":{"exchange_ids":[],"session_ids":[]},"orphans":{"r2_keys":["log/orphan-3.json"],"cursor":null}}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"reconcile"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || !strings.Contains(output.String(), `"pages": 3`) || !strings.Contains(output.String(), `"pending-1"`) || !strings.Contains(output.String(), `"log/orphan-3.json"`) {
		t.Fatalf("calls=%d output=%s", calls, output.String())
	}
}

func TestExecuteReconcilePrintsEmptyLists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"scanned":0,"database_cursor":null,"finalized":{"exchange_ids":[]},"pending":{"exchange_ids":[],"stale_exchange_ids":[]},"missing_saved":{"exchange_ids":[],"session_ids":[]},"orphans":{"r2_keys":[],"cursor":null}}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"reconcile"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), ": null") || !strings.Contains(output.String(), `"orphan_r2_keys": []`) {
		t.Fatalf("output=%s", output.String())
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
