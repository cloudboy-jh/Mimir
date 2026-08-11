package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
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
	if got := output.String(); !strings.Contains(got, "Mimir") || !strings.Contains(got, "Version") || !strings.Contains(got, "1.2.3 (abc123)") {
		t.Fatalf("version output %q", got)
	}
}

func TestExecuteWarnsAboutMalformedPendingUpdateWithoutBlockingCommands(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if err := os.MkdirAll(paths.MimirHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.MimirHome, "pending-update.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output, stderr bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"--version"}, IO{Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) == "" || !strings.Contains(stderr.String(), "pending update") {
		t.Fatalf("stdout=%q stderr=%q", output.String(), stderr.String())
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
	configureInstall()
	if _, err := installpkg.Install(t.TempDir(), executablePath); err != nil {
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
	if report.BundleVersion == "" || report.ReceiptPath == "" || report.ArtifactCounts[installpkg.ArtifactCurrent] == 0 {
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
	var report lifecyclepkg.InstallReport
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

func TestExecuteInstallPreservesConflictingHermesPluginWithoutEnablingIt(t *testing.T) {
	paths := isolatedInstallation(t, true)
	conflict := filepath.Join(paths.HermesHome, "plugins", "mimir", "plugin.yaml")
	if err := os.MkdirAll(filepath.Dir(conflict), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte("name: user-plugin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldRunHermesPluginCommand := runHermesPluginCommand
	commands := 0
	runHermesPluginCommand = func(context.Context, string, ...string) error { commands++; return nil }
	t.Cleanup(func() {
		runHermesPluginCommand = oldRunHermesPluginCommand
	})
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"install", "--bin-dir", t.TempDir(), "--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var report lifecyclepkg.InstallReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.ActionRequired || commands != 0 {
		t.Fatalf("report=%#v Hermes commands=%d", report, commands)
	}
}

func TestExecuteUninstallJSONReportsBinaryAndArtifacts(t *testing.T) {
	isolatedInstallation(t, false)
	binary := filepath.Join(t.TempDir(), "mimir")
	binaryData := []byte("managed binary")
	if err := os.WriteFile(binary, binaryData, 0o755); err != nil {
		t.Fatal(err)
	}
	configureInstall()
	installed, err := installpkg.Install(t.TempDir(), func() (string, error) { return binary, nil })
	if err != nil {
		t.Fatal(err)
	}
	binary = installed.Binary.Path
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"uninstall", "--keep-binary", "--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var report lifecyclepkg.UninstallReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Operation != "uninstall" || report.Binary.Status != "kept" || len(report.Artifacts) == 0 {
		t.Fatalf("uninstall report %#v", report)
	}
	for _, artifact := range report.Artifacts {
		if artifact.Status != installpkg.ArtifactRemoved {
			t.Fatalf("artifact status = %s", artifact.Status)
		}
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("--keep-binary removed binary: %v", err)
	}
}

func TestPostUpdateCommandUsesJSONProtocol(t *testing.T) {
	isolatedInstallation(t, false)
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"_post-update"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var report lifecyclepkg.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Artifacts.Operation != "update" || !strings.Contains(report.Integrations.OpenCode.Detail, "not connected") {
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
	if !strings.Contains(output.String(), "mimir setup [--json]") || !strings.Contains(output.String(), "mimir demo [--no-open]") {
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
			id, outcome, reason, evidence, err := parseSessionOutcomeArgs(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if id != "session-1" || outcome != test.args[1] || reason != test.reason || evidence != nil {
				t.Fatalf("id=%q outcome=%q reason=%q", id, outcome, reason)
			}
		})
	}
	if _, _, _, _, err := parseSessionOutcomeArgs([]string{"session-1", "promoted"}); err == nil {
		t.Fatal("canonical command accepted legacy outcome")
	}
	_, _, _, evidence, err := parseSessionOutcomeArgs([]string{"session-1", "landed", "--evidence", `{"commit":"abc"}`})
	if err != nil || evidence.Value.(map[string]any)["commit"] != "abc" {
		t.Fatalf("evidence=%#v err=%v", evidence, err)
	}
	_, _, _, evidence, err = parseSessionOutcomeArgs([]string{"session-1", "landed", "--evidence", "null"})
	if err != nil || evidence == nil || evidence.Value != nil {
		t.Fatalf("null evidence=%#v err=%v", evidence, err)
	}
	if _, _, _, _, err := parseSessionOutcomeArgs([]string{"session-1", "landed", "--evidence", "null", "--evidence", `{}`}); err == nil {
		t.Fatal("duplicate null evidence was accepted")
	}
}

func TestParseSessionEndEvidence(t *testing.T) {
	id, options, err := parseSessionEndArgs([]string{"session-1", "--outcome", "landed", "--evidence", `{"commit":"abc"}`})
	if err != nil {
		t.Fatal(err)
	}
	if id != "session-1" || !options.EvidenceSet || options.Evidence.(map[string]any)["commit"] != "abc" {
		t.Fatalf("id=%q options=%#v", id, options)
	}
	if _, _, err := parseSessionEndArgs([]string{"session-1", "--evidence", `{"commit":"abc"}`}); err == nil {
		t.Fatal("evidence without outcome was accepted")
	}
}

func TestSessionSubcommandsRejectJSONAsMissingID(t *testing.T) {
	for _, args := range [][]string{{"get", "--json"}, {"status", "--json"}, {"outcome"}, {"outcome", "--json"}} {
		if err := cmdSession(context.Background(), args, io.Discard); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Fatalf("cmdSession(%v) error = %v", args, err)
		}
	}
}

func TestPrintRemoteDataPreservesLargeIntegers(t *testing.T) {
	var output bytes.Buffer
	if err := printRemoteData(&output, []byte(`{"value":9007199254740993}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "9007199254740993") {
		t.Fatalf("output = %q", output.String())
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
		{name: "target session get", args: []string{"session", "get", "session-1", "--json"}, wantMethod: http.MethodGet, wantPath: "/sessions/session-1"},
		{name: "session outcome", args: []string{"session", "outcome", "session-1", "landed", "--reason", "merged to main", "--json"}, wantMethod: http.MethodPost, wantPath: "/sessions/session-1/outcome", wantBody: map[string]any{"outcome": "landed", "source": "agent", "reason": "merged to main"}},
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
			if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
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
	oldSchedule := sessionStatusPollScheduleOverride
	sessionStatusPollScheduleOverride = []time.Duration{0}
	t.Cleanup(func() { sessionStatusPollScheduleOverride = oldSchedule })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/sessions/session-1/status" {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"session_id":"session-1","capture":{"status":"saved","saved_exchanges":14,"failed_exchanges":1,"pending_exchanges":0,"last_saved_at":"2026-07-15T23:42:00Z"},"receipt":{"label":"Saved to Mimir","detail":"14 exchanges in this session","action_label":"View session"},"dashboard_url":"https://mimir.example/dashboard/sessions/session-1","outcome":"unresolved"}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	var human bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"session", "status", "session-1"}, IO{Out: &human}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"Session status", "Saved to Mimir · 14 exchanges in this session", "Session", "session-1", "[SAVED]", "Saved", "14", "Failed", "1", "[UNRESOLVED]", "https://mimir.example/dashboard/sessions/session-1"} {
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
	oldSchedule := sessionStatusPollScheduleOverride
	sessionStatusPollScheduleOverride = []time.Duration{0}
	t.Cleanup(func() { sessionStatusPollScheduleOverride = oldSchedule })
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
	if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"session", "end", "session-1", "--outcome", "landed", "--reason", "verified"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Capture finalized") || !strings.Contains(output.String(), "Saved to Mimir · 1 exchange in this session") || !strings.Contains(output.String(), "[LANDED]") {
		t.Fatalf("output %q", output.String())
	}
	var machine bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"session", "end", "session-1", "--outcome", "landed", "--reason", "verified", "--json"}, IO{Out: &machine}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(machine.String(), `"session_id": "session-1"`) || !strings.Contains(machine.String(), `"saved_exchanges": 1`) {
		t.Fatalf("JSON end output %s", machine.String())
	}
	if _, _, err := parseSessionEndArgs([]string{"session-1", "--reason", "missing outcome"}); err == nil {
		t.Fatal("reason without outcome was accepted")
	}
}

func TestExecuteSearchJSONDoesNotIncludeFlagInQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["query"] != "failed migration" {
			t.Fatalf("query %#v", body["query"])
		}
		_, _ = w.Write([]byte(`{"query":"failed migration","matches":[]}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"search", "failed", "migration", "--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"query": "failed migration"`) {
		t.Fatalf("search output %s", output.String())
	}
}

func TestExecuteRejectsRemovedLocalServerCommands(t *testing.T) {
	for _, command := range []string{"serve", "tools"} {
		err := ExecuteIO(context.Background(), []string{command}, IO{Out: &bytes.Buffer{}})
		if err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("%s error = %v", command, err)
		}
	}
}

func TestExecuteRejectsManualUpdateHelperInvocation(t *testing.T) {
	err := ExecuteIO(context.Background(), []string{"_apply-update"}, IO{Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "non-helper executable") {
		t.Fatalf("error = %v", err)
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
