package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

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
		Main:     debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "metadata-commit"}, {Key: "vcs.time", Value: "metadata-date"}},
	}
	gotVersion, gotCommit, gotDate := resolveBuildInfo("1.2.3", "release-commit", "release-date", info)
	if gotVersion != "1.2.3" || gotCommit != "release-commit" || gotDate != "release-date" {
		t.Fatalf("build info = %q, %q, %q", gotVersion, gotCommit, gotDate)
	}
}

func TestExecuteUsageListsQueryCommands(t *testing.T) {
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), nil, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mimir sessions", "mimir turns <id>", "mimir stats --by", "mimir day", "mimir devices", "mimir commits"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("usage missing %q:\n%s", want, output.String())
		}
	}
	for _, removed := range []string{"mimir list", "mimir demo", "recall", "outcome"} {
		if strings.Contains(output.String(), removed) {
			t.Fatalf("usage still lists %q", removed)
		}
	}
}

func TestExecuteRejectsRemovedCommands(t *testing.T) {
	for _, command := range []string{"serve", "tools", "list", "mark", "outcome", "recall", "index", "demo", "_apply-update", "_post-update"} {
		err := ExecuteIO(context.Background(), []string{command}, IO{Out: &bytes.Buffer{}})
		if err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("%s error = %v", command, err)
		}
	}
	for _, args := range [][]string{{"session", "outcome", "s", "landed"}, {"session", "git", "verify", "s", "sha"}, {"session", "end", "s", "--outcome", "landed"}, {"session", "status"}, {"session", "end", "--json"}} {
		if err := ExecuteIO(context.Background(), args, IO{Out: &bytes.Buffer{}}); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Fatalf("%v error = %v", args, err)
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

type recordedRequest struct {
	method, path, query string
	body                map[string]any
}

// connectTestWorker points the CLI at a fake Worker that answers by path.
func connectTestWorker(t *testing.T, responses map[string]string) *[]recordedRequest {
	t.Helper()
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer test-token" {
			t.Errorf("authorization = %q", r.Header.Get("authorization"))
		}
		record := recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}
		data, _ := io.ReadAll(r.Body)
		if len(data) > 0 {
			_ = json.Unmarshal(data, &record.body)
		}
		requests = append(requests, record)
		response, ok := responses[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	t.Setenv(mimirapi.EnvHome, t.TempDir())
	if err := mimirapi.SavePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	return &requests
}

const sessionJSON = `{"id":"s-1","display_title":"Fix the receipt","state":"inactive","harness":"claude-code","repo":"mimir","started_at":"2026-11-18T10:00:00Z","activity_at":"2026-11-18T11:00:00Z","request_count":4,"tokens_in":10,"tokens_out":20,"child_session_count":1,"git_status":"pushed","commit_count":1,"capture":{"status":"saved"}}`

func TestSessionsMapsFlagsToQueryAPI(t *testing.T) {
	requests := connectTestWorker(t, map[string]string{
		"GET /api/sessions": `{"sessions":[` + sessionJSON + `],"next_cursor":"c2"}`,
	})
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"sessions", "--repo", "mimir", "--git-status=pushed", "--limit", "5"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if got := (*requests)[0].query; got != "git_status=pushed&limit=5&repo=mimir" {
		t.Fatalf("query = %q", got)
	}
	for _, want := range []string{"s-1", "pushed", "Fix the receipt", "More: mimir sessions --cursor c2"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
	output.Reset()
	if err := ExecuteIO(context.Background(), []string{"sessions", "--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"next_cursor": "c2"`) {
		t.Fatalf("json output %s", output.String())
	}
	if err := ExecuteIO(context.Background(), []string{"sessions", "--outcome", "landed"}, IO{Out: io.Discard}); err == nil || ExitCode(err) != ExitInvalidInvocation {
		t.Fatalf("removed filter error = %v", err)
	}
}

func TestSessionDetailStatusAndEnd(t *testing.T) {
	receipt := `{"session_id":"s-1","title":"Fix the receipt","line":"Saved to Mimir · 4 exchanges · 1 commit","dashboard_url":"https://mimir.example/sessions/s-1"}`
	requests := connectTestWorker(t, map[string]string{
		"GET /api/sessions/s-1":         `{"session":` + sessionJSON + `,"summary":{"status":"ready","goal":"Fix it","actions":["Edited receipt.ts"],"result":"Done","verification":[],"unresolved":[],"partial":false,"evidence":[]},"receipt":` + receipt + `,"files":[{"path":"receipt.ts","count":2}],"errors":[],"commits":[{"session_id":"s-1","commit_sha":"0123456789abcdef0123456789abcdef01234567","subject":"Fix receipt","pushed":true}],"supporting_sessions":[]}`,
		"GET /api/sessions/s-1/receipt": receipt,
		"POST /sessions/s-1/end":        `{"ok":true}`,
	})
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"session", "s-1"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Fix the receipt", "Goal: Fix it", "- Edited receipt.ts", "0123456789  pushed", "receipt.ts (2)", "Saved to Mimir · 4 exchanges · 1 commit"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("detail missing %q:\n%s", want, output.String())
		}
	}
	output.Reset()
	if err := ExecuteIO(context.Background(), []string{"session", "status", "s-1"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Saved to Mimir · 4 exchanges · 1 commit\nDashboard: https://mimir.example/sessions/s-1\n" {
		t.Fatalf("status output %q", output.String())
	}
	output.Reset()
	if err := ExecuteIO(context.Background(), []string{"session", "end", "s-1"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "Session ended · Saved to Mimir") {
		t.Fatalf("end output %q", output.String())
	}
	end := (*requests)[len(*requests)-2]
	if end.method != http.MethodPost || end.path != "/sessions/s-1/end" || len(end.body) != 0 {
		t.Fatalf("end request %#v", end)
	}
}

func TestSearchStatsDayAndCommitsCallQueryAPI(t *testing.T) {
	requests := connectTestWorker(t, map[string]string{
		"GET /api/search":          `{"query":"failed migration","results":[{"session":` + sessionJSON + `,"score":1.5,"hits":[{"exchange_id":"e","ts":"2026-11-18T10:30:00Z","snippet":"the failed\nmigration"}]}]}`,
		"GET /api/stats":           `{"from":"2026-11-11T00:00:00Z","to":"2026-11-18T00:00:00Z","group_by":"repo","groups":[{"key":"mimir","sessions":3,"requests":9,"tokens_in":1,"tokens_out":2,"commits":4}]}`,
		"GET /api/days/2026-11-18": `{"date":"2026-11-18","time_zone":"UTC","totals":{"sessions":1,"requests":4,"commits":1,"active":0},"workstreams":[{"label":"mimir","session_ids":["s-1"],"requests":4,"git":{"none":0,"committed":0,"pushed":1}}],"sessions":[{"id":"s-1","display_title":"Fix the receipt","state":"inactive","git_status":"pushed","first_at":"2026-11-18T10:00:00Z","requests":4}]}`,
		"GET /api/commits":         `{"commits":[{"session_id":"s-1","commit_sha":"0123456789abcdef0123456789abcdef01234567","subject":"Fix receipt","repository_url":"https://github.com/o/mimir","accepted_at":"2026-11-18T10:00:00Z","pushed":false,"sessions":[{"id":"s-1","display_title":"Fix"}]}],"next_cursor":null}`,
	})
	var output bytes.Buffer
	run := func(args ...string) string {
		t.Helper()
		output.Reset()
		if err := ExecuteIO(context.Background(), args, IO{Out: &output}); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return output.String()
	}
	if got := run("search", "failed", "migration", "--limit", "3"); !strings.Contains(got, "the failed migration") || !strings.Contains(got, "Fix the receipt") {
		t.Fatalf("search output %q", got)
	}
	if got := (*requests)[0].query; got != "limit=3&q=failed+migration" {
		t.Fatalf("search query %q", got)
	}
	if got := run("stats", "--by", "repo", "--tz", "UTC"); !strings.Contains(got, "mimir") || !strings.Contains(got, "REPO") {
		t.Fatalf("stats output %q", got)
	}
	if got := (*requests)[1].query; got != "group_by=repo&tz=UTC" {
		t.Fatalf("stats query %q", got)
	}
	if got := run("day", "2026-11-18", "--tz", "UTC"); !strings.Contains(got, "2026-11-18 (UTC) · 1 sessions") || !strings.Contains(got, "10:00") {
		t.Fatalf("day output %q", got)
	}
	if got := run("commits", "--repo", "mimir"); !strings.Contains(got, "0123456789") || !strings.Contains(got, "committed") || !strings.Contains(got, "mimir") {
		t.Fatalf("commits output %q", got)
	}
	if err := ExecuteIO(context.Background(), []string{"stats"}, IO{Out: io.Discard}); err == nil || ExitCode(err) != ExitInvalidInvocation {
		t.Fatalf("stats without --by error = %v", err)
	}
}

func TestQueryAPIMissingIsIncompatibleWorker(t *testing.T) {
	connectTestWorker(t, map[string]string{
		"GET /whoami": `{"service":"mimir","api_version":1,"capabilities":["session_outcomes"]}`,
	})
	err := ExecuteIO(context.Background(), []string{"sessions"}, IO{Out: io.Discard})
	if err == nil || ExitCode(err) != ExitIncompatibleWorker || !strings.Contains(err.Error(), "mimir deploy") {
		t.Fatalf("error = %v (exit %d)", err, ExitCode(err))
	}
}

func TestHookGitRejectsInvalidInput(t *testing.T) {
	t.Setenv(mimirapi.EnvHome, t.TempDir())
	for _, input := range []string{`{}`, `{"session_id":"s-1","cwd":"relative","harness":"pi"}`, `not json`} {
		err := ExecuteIO(context.Background(), []string{"_hook", "git"}, IO{In: strings.NewReader(input), Out: io.Discard})
		if err == nil || ExitCode(err) != ExitInvalidInvocation {
			t.Fatalf("input %s error = %v", input, err)
		}
	}
}
