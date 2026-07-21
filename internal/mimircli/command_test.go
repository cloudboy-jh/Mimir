package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestExecuteVersion(t *testing.T) {
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
