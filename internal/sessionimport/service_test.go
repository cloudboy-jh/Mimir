package sessionimport

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"
)

type staticSource struct {
	sessions []Session
	err      error
}

func (staticSource) Name() string                                  { return "fixture" }
func (s staticSource) Discover(context.Context) ([]Session, error) { return s.sessions, s.err }

type recordedRequest struct {
	method, path string
	body         any
	headers      http.Header
}
type recordingClient struct {
	requests  []recordedRequest
	responses map[string][][]byte
}

func (c *recordingClient) RequestWithHeaders(_ context.Context, method, path string, body any, headers http.Header) ([]byte, error) {
	c.requests = append(c.requests, recordedRequest{method: method, path: path, body: body, headers: headers.Clone()})
	values := c.responses[path]
	if len(values) == 0 {
		return []byte(`{"ok":true}`), nil
	}
	c.responses[path] = values[1:]
	return values[0], nil
}

func TestServiceUploadsLifecycleAndExistingExchangeEndpoint(t *testing.T) {
	client := &recordingClient{responses: map[string][][]byte{"/sessions/session-1/exchanges": {[]byte(`{"capture_status":"saved","duplicate":false}`)}}}
	start := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	exchange := Exchange{ExchangeID: "a1", Timestamp: start.Add(time.Second), Provider: "anthropic", Model: "claude", Request: map[string]any{"messages": []any{}}, Response: map[string]any{"role": "assistant"}, Tools: []ToolActivity{}, Usage: Usage{}, RequestKind: "primary"}
	source := staticSource{sessions: []Session{{ID: "session-1", SourceID: "local-1", Harness: "opencode", Repo: "mimir", Title: "Old work", StartedAt: start, EndedAt: start.Add(2 * time.Second), Exchanges: []Exchange{exchange}, SkippedOpenRouter: 2}}}
	report, err := (Service{Client: client, Sources: []Source{source}}).Import(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.SessionsImported != 1 || report.ExchangesUploaded != 1 || report.SkippedOpenRouter != 2 {
		t.Fatalf("report = %#v", report)
	}
	paths := []string{}
	for _, request := range client.requests {
		paths = append(paths, request.path)
		if request.headers.Get("x-mimir-harness") != "opencode" || request.headers.Get("x-mimir-repo") != "mimir" {
			t.Fatalf("headers = %#v", request.headers)
		}
	}
	want := []string{"/sessions/session-1/exchanges", "/sessions/session-1/events", "/sessions/session-1/events"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v", paths)
	}
	first, last := client.requests[1].body.(map[string]any), client.requests[2].body.(map[string]any)
	if first["kind"] != "heartbeat" || last["kind"] != "end" || last["reason"] != "historical-import" {
		t.Fatalf("events = %#v / %#v", first, last)
	}
	data, err := json.Marshal(client.requests[0].body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ts"] != "2026-08-20T10:00:01Z" || payload["exchange_id"] != "a1" {
		t.Fatalf("exchange payload = %#v", payload)
	}
}

type fixedCollector struct {
	artifacts []GitArtifact
	calls     int
}

func (c *fixedCollector) Collect(context.Context, Session) ([]GitArtifact, error) {
	c.calls++
	return c.artifacts, nil
}

func TestServiceClassifiesResponsesSuppressesEmptyLifecycleAndUploadsArtifacts(t *testing.T) {
	start := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	exchange := func(id string) Exchange {
		return Exchange{ExchangeID: id, Timestamp: start, Model: "m", RequestKind: "primary", Request: map[string]any{}, Response: map[string]any{}, Tools: []ToolActivity{}, Usage: Usage{}}
	}
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	collector := &fixedCollector{artifacts: []GitArtifact{{CommitSHA: sha, CommittedAt: "2026-08-20T10:00:00.000Z", Subject: "work", Patch: "patch"}}}
	client := &recordingClient{responses: map[string][][]byte{
		"/sessions/s/exchanges": {
			[]byte(`{"capture_status":"skipped","duplicate":false}`),
			[]byte(`{"capture_status":"skipped","duplicate":false}`),
		},
		"/sessions/s/git-artifacts": {[]byte(`{"kind":"ok","artifacts":[{}],"duplicates":0}`)},
	}}
	session := Session{ID: "s", SourceID: "source", Harness: "pi", Directory: "/recorded/checkout", StartedAt: start, Exchanges: []Exchange{exchange("one"), exchange("two")}}
	report, err := (Service{Client: client, Artifacts: collector}).Upload(context.Background(), []Session{session})
	if err != nil {
		t.Fatal(err)
	}
	if report.SessionsSkipped != 1 || report.ExchangesSkipped != 2 || report.GitArtifactsSaved != 1 || collector.calls != 1 {
		t.Fatalf("report = %#v, collector calls = %d", report, collector.calls)
	}
	paths := make([]string, 0, len(client.requests))
	for _, request := range client.requests {
		paths = append(paths, request.path)
	}
	if want := []string{"/sessions/s/exchanges", "/sessions/s/exchanges", "/sessions/s/git-artifacts"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestServiceClassifiesSavedDuplicateAndSkipped(t *testing.T) {
	start := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	exchanges := make([]Exchange, 3)
	for index := range exchanges {
		exchanges[index] = Exchange{ExchangeID: string(rune('a' + index)), Timestamp: start, Model: "m", RequestKind: "primary", Request: map[string]any{}, Response: map[string]any{}, Tools: []ToolActivity{}, Usage: Usage{}}
	}
	client := &recordingClient{responses: map[string][][]byte{"/sessions/s/exchanges": {
		[]byte(`{"capture_status":"saved","duplicate":false}`),
		[]byte(`{"capture_status":"saved","duplicate":true}`),
		[]byte(`{"capture_status":"skipped","duplicate":false}`),
	}}}
	report, err := (Service{Client: client}).Upload(context.Background(), []Session{{ID: "s", SourceID: "s", Harness: "opencode", StartedAt: start, EndedAt: start, Exchanges: exchanges}})
	if err != nil {
		t.Fatal(err)
	}
	if report.ExchangesSaved != 1 || report.ExchangesDuplicate != 1 || report.ExchangesSkipped != 1 || report.SessionsImported != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestDiscoverIsSeparateAndAppliesSelection(t *testing.T) {
	client := &recordingClient{}
	cutoff := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	source := staticSource{sessions: []Session{
		{ID: "before", SourceID: "wanted", Harness: "pi", StartedAt: cutoff.Add(-time.Hour)},
		{ID: "after", SourceID: "wanted", Harness: "pi", StartedAt: cutoff.Add(time.Hour)},
		{ID: "other", SourceID: "other", Harness: "pi", StartedAt: cutoff.Add(-time.Hour)},
	}}
	service := Service{Client: client, Sources: []Source{source}}
	discovery, err := service.Discover(context.Background(), Options{Sources: []string{"fixture"}, SourceIDs: []string{"wanted"}, Before: cutoff})
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Sessions) != 1 || discovery.Sessions[0].ID != "before" || len(client.requests) != 0 {
		t.Fatalf("discovery = %#v, requests = %#v", discovery, client.requests)
	}
}
