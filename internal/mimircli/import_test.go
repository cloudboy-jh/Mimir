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
