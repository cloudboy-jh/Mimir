package sessions

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGetReturnsTypedDetailAndPreservesUnknownData(t *testing.T) {
	service := New(requestFunc(func(_ context.Context, method, path string, body any) ([]byte, error) {
		if method != "GET" || path != "/sessions/root%2Fchild" || body != nil {
			t.Fatalf("request %s %s %#v", method, path, body)
		}
		return []byte(`{
			"session":{"id":"root/child","started_at":"2026-07-31T10:00:00Z","outcome":"landed","request_count":2,"future_session_field":true},
			"capture":{"status":"saved","saved_exchanges":2,"failed_exchanges":0,"pending_exchanges":0,"last_saved_at":"2026-07-31T10:01:00Z"},
			"outcome_events":[{"id":"event-1","outcome":"landed","source":"agent","reason":"shipped","evidence_json":"{\"commit\":\"abc\"}","created_at":"2026-07-31T10:02:00Z"}],
			"supporting_sessions":[{"id":"child-1","started_at":"2026-07-31T10:00:30Z","outcome":"unresolved"}],
			"files":["internal/search/search.go"],"errors":["timeout"],
			"exchanges":[{"id":"exchange-1","session_id":"root/child","ts":"2026-07-31T10:00:10Z","model":"model-1","input_tokens":10,"output_tokens":5,"request_excerpt":"request","response_excerpt":"response","capture_status":"saved"}],
			"future_sequence":9007199254740993123456789
		}`), nil
	}))

	detail, err := service.Get(context.Background(), "root/child")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.ID != "root/child" || detail.Session.RequestCount != 2 || detail.Capture.SavedExchanges != 2 {
		t.Fatalf("detail = %#v", detail)
	}
	if len(detail.Exchanges) != 1 || detail.Exchanges[0].Model != "model-1" || len(detail.OutcomeEvents) != 1 {
		t.Fatalf("detail = %#v", detail)
	}
	if got := string(detail.Raw["future_sequence"]); got != "9007199254740993123456789" {
		t.Fatalf("future_sequence = %s", got)
	}
	var rawSession map[string]json.RawMessage
	if err := json.Unmarshal(detail.Raw["session"], &rawSession); err != nil {
		t.Fatal(err)
	}
	if string(rawSession["future_session_field"]) != "true" {
		t.Fatalf("raw session = %s", detail.Raw["session"])
	}
}

func TestGetRejectsInvalidDetailJSON(t *testing.T) {
	service := New(requestFunc(func(context.Context, string, string, any) ([]byte, error) {
		return []byte(`{"session":`), nil
	}))
	if _, err := service.Get(context.Background(), "session-1"); err == nil {
		t.Fatal("expected decode error")
	}
}
