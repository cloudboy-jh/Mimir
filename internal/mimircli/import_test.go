package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudboy-jh/mimir/internal/sessionimport"
)

type fakeSessionImportService struct {
	discovery sessionimport.Discovery
	report    sessionimport.Report
	options   sessionimport.Options
	uploaded  []sessionimport.Session
}

func (f *fakeSessionImportService) Discover(_ context.Context, options sessionimport.Options) (sessionimport.Discovery, error) {
	f.options = options
	return f.discovery, nil
}

func (f *fakeSessionImportService) Upload(_ context.Context, sessions []sessionimport.Session) (sessionimport.Report, error) {
	f.uploaded = append([]sessionimport.Session(nil), sessions...)
	return f.report, nil
}

func withImportService(t *testing.T, service *fakeSessionImportService) {
	t.Helper()
	original := importServiceFactory
	importServiceFactory = func(_ int, _ bool) (sessionImportService, error) { return service, nil }
	t.Cleanup(func() { importServiceFactory = original })
}

func importFixtureSession() sessionimport.Session {
	return sessionimport.Session{
		ID:        "ses_1",
		SourceID:  "ses_1",
		Harness:   "opencode",
		Title:     "Recovered work",
		StartedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
		Exchanges: []sessionimport.Exchange{{ExchangeID: "msg_1"}},
	}
}

func TestImportExactSessionJSON(t *testing.T) {
	service := &fakeSessionImportService{
		discovery: sessionimport.Discovery{Sessions: []sessionimport.Session{importFixtureSession()}},
		report:    sessionimport.Report{SessionsDiscovered: 1, SessionsImported: 1, ExchangesSaved: 1},
	}
	withImportService(t, service)
	var out bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"import", "opencode", "ses_1", "--yes", "--json"}, IO{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if len(service.uploaded) != 1 || service.options.Sources[0] != "opencode" || service.options.SourceIDs[0] != "ses_1" {
		t.Fatalf("options=%#v uploaded=%#v", service.options, service.uploaded)
	}
	var report sessionimport.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.SessionsImported != 1 {
		t.Fatalf("output=%s error=%v", out.String(), err)
	}
}

func TestImportListReturnsBoundedMetadata(t *testing.T) {
	service := &fakeSessionImportService{discovery: sessionimport.Discovery{Sessions: []sessionimport.Session{importFixtureSession()}}}
	withImportService(t, service)
	var out bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"import", "list", "opencode", "--json"}, IO{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "msg_1") || !strings.Contains(out.String(), `"source_id":"ses_1"`) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestImportRequiresExplicitApprovalWithoutTTY(t *testing.T) {
	service := &fakeSessionImportService{}
	withImportService(t, service)
	err := ExecuteIO(context.Background(), []string{"import", "opencode", "ses_1", "--json"}, IO{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("error=%v", err)
	}
}

func TestBackfillSinceAndExplicitAll(t *testing.T) {
	service := &fakeSessionImportService{
		discovery: sessionimport.Discovery{Sessions: []sessionimport.Session{importFixtureSession()}},
		report:    sessionimport.Report{SessionsDiscovered: 1, SessionsImported: 1},
	}
	withImportService(t, service)
	originalNow := importNow
	importNow = func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { importNow = originalNow })
	if err := ExecuteIO(context.Background(), []string{"backfill", "opencode", "--since", "7d", "--all", "--yes", "--json"}, IO{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	if !service.options.Since.Equal(want) || len(service.uploaded) != 1 {
		t.Fatalf("options=%#v uploaded=%d", service.options, len(service.uploaded))
	}
}

func TestBackfillRejectsBroadNonInteractiveRun(t *testing.T) {
	err := ExecuteIO(context.Background(), []string{"backfill", "--json"}, IO{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "--all --yes") {
		t.Fatalf("error=%v", err)
	}
}

func TestBackfillPreservesPartialSourceFailure(t *testing.T) {
	service := &fakeSessionImportService{
		discovery: sessionimport.Discovery{
			Sessions: []sessionimport.Session{importFixtureSession()},
			Sources:  []sessionimport.SourceReport{{Source: "opencode", Status: "failed", Error: "not installed"}, {Source: "pi", Status: "ok", Count: 1}},
		},
		report: sessionimport.Report{SessionsDiscovered: 1, SessionsImported: 1},
	}
	withImportService(t, service)
	var out bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"backfill", "--all", "--yes", "--json"}, IO{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	var report sessionimport.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.SourcesFailed != 1 || len(report.Sessions) != 1 || report.Sessions[0].Source != "opencode" {
		t.Fatalf("report=%#v", report)
	}
}

func TestOMPParentRepairPreviewAndApproval(t *testing.T) {
	session := sessionimport.Session{ID: "child", SourceID: "child", Harness: "oh-my-pi", ParentSessionID: "parent", ParentLinkStatus: "pending", ParentLinkEvidence: "omp-artifact-tree"}
	service := &fakeSessionImportService{discovery: sessionimport.Discovery{Sessions: []sessionimport.Session{session}}}
	withImportService(t, service)
	var out bytes.Buffer
	ioctx := IO{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}}
	if err := ExecuteIO(context.Background(), []string{"import", "inspect", "oh-my-pi", "child", "--json"}, ioctx); err != nil {
		t.Fatal(err)
	}
	var preview importCandidate
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.ParentSessionID != "parent" || preview.ParentLinkStatus != "pending" || len(service.uploaded) != 0 {
		t.Fatalf("preview=%#v uploads=%#v", preview, service.uploaded)
	}
	if err := ExecuteIO(context.Background(), []string{"backfill", "oh-my-pi", "--all", "--json"}, ioctx); err == nil || len(service.uploaded) != 0 {
		t.Fatalf("unapproved repair: error=%v uploads=%#v", err, service.uploaded)
	}
	out.Reset()
	if err := ExecuteIO(context.Background(), []string{"backfill", "oh-my-pi", "--all", "--yes", "--json"}, ioctx); err != nil {
		t.Fatal(err)
	}
	if len(service.uploaded) != 1 {
		t.Fatalf("approved repair did not apply selected history: %#v", service.uploaded)
	}
}

func TestSelectImportHarnessReadsOneNumber(t *testing.T) {
	var out bytes.Buffer
	harness, err := selectImportHarness(IO{In: strings.NewReader("3\n"), Out: &out})
	if err != nil || harness != "oh-my-pi" {
		t.Fatalf("harness=%q error=%v", harness, err)
	}
	if !strings.Contains(out.String(), "  1. OpenCode\n") {
		t.Fatalf("output=%q", out.String())
	}
	if harness, err := selectImportHarness(IO{In: strings.NewReader("\n"), Out: &out}); err != nil || harness != "" {
		t.Fatalf("empty answer: harness=%q error=%v", harness, err)
	}
	if _, err := selectImportHarness(IO{In: strings.NewReader("4\n"), Out: &out}); err == nil {
		t.Fatal("out-of-range harness accepted")
	}
}

func TestSelectImportSessionsParsesNumbers(t *testing.T) {
	first, second := importFixtureSession(), importFixtureSession()
	second.SourceID = "ses_2"
	sessions := []sessionimport.Session{first, second}
	for _, tc := range []struct {
		answer string
		want   []string
	}{{"\n", nil}, {"2\n", []string{"ses_2"}}, {"2, 1,2\n", []string{"ses_1", "ses_2"}}, {"ALL\n", []string{"ses_1", "ses_2"}}} {
		chosen, err := selectImportSessions(IO{In: strings.NewReader(tc.answer), Out: &bytes.Buffer{}}, sessions)
		if err != nil {
			t.Fatalf("%q: %v", tc.answer, err)
		}
		var got []string
		for _, session := range chosen {
			got = append(got, session.SourceID)
		}
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("%q: got %v want %v", tc.answer, got, tc.want)
		}
	}
	for _, answer := range []string{"0\n", "3\n", "1,x\n"} {
		if _, err := selectImportSessions(IO{In: strings.NewReader(answer), Out: &bytes.Buffer{}}, sessions); err == nil {
			t.Fatalf("invalid selection %q accepted", answer)
		}
	}
}

func TestImportReportPlainOutput(t *testing.T) {
	var out bytes.Buffer
	report := sessionimport.Report{SessionsImported: 1, SessionsFailed: 1, Sessions: []sessionimport.SessionReport{{Source: "pi", SourceID: "s1", Status: "failed", Error: "boom"}}}
	if err := printImportReport(&out, report, false, "Import"); err != nil {
		t.Fatal(err)
	}
	want := "! Import complete: 1 imported, 0 partial, 0 already present, 0 skipped, 1 failed\n  pi s1: boom\n"
	if out.String() != want {
		t.Fatalf("output=%q", out.String())
	}
}
