package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

type requestFunc func(context.Context, string, string, any) ([]byte, error)

func (f requestFunc) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	return f(ctx, method, path, body)
}

func TestGetStatusWaitsForSavedCapture(t *testing.T) {
	calls := 0
	service := New(requestFunc(func(_ context.Context, _, path string, _ any) ([]byte, error) {
		calls++
		if path != "/sessions/session-1/status" {
			t.Fatalf("path = %q", path)
		}
		if calls == 1 {
			return []byte(`{"session_id":"session-1","capture":{"status":"pending","saved_exchanges":0,"failed_exchanges":0,"pending_exchanges":1},"receipt":{"label":"Saving to Mimir...","detail":"1 exchange"},"outcome":"unresolved"}`), nil
		}
		return []byte(`{"session_id":"session-1","capture":{"status":"saved","saved_exchanges":1,"failed_exchanges":0,"pending_exchanges":0},"receipt":{"label":"Saved to Mimir","detail":"1 exchange in this session"},"outcome":"unresolved"}`), nil
	}))
	status, err := service.GetStatusWithSchedule(context.Background(), "session-1", []time.Duration{0})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || status.Capture.Status != "saved" || status.Capture.SavedExchanges != 1 {
		t.Fatalf("calls=%d status=%#v", calls, status)
	}
}

func TestNewOwnsDefaultPollingSchedule(t *testing.T) {
	service := New(nil)
	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}
	if len(service.PollSchedule) != len(want) {
		t.Fatalf("schedule = %v", service.PollSchedule)
	}
	for i := range want {
		if service.PollSchedule[i] != want[i] {
			t.Fatalf("schedule = %v", service.PollSchedule)
		}
	}
}

func TestGetStatusReturnsLatestPendingAfterBoundedWait(t *testing.T) {
	calls := 0
	service := New(requestFunc(func(context.Context, string, string, any) ([]byte, error) {
		calls++
		return []byte(`{"session_id":"session-1","capture":{"status":"pending","saved_exchanges":0,"failed_exchanges":0,"pending_exchanges":1},"receipt":{"label":"Saving to Mimir...","detail":"1 exchange"},"outcome":"unresolved"}`), nil
	}))
	status, err := service.GetStatusWithSchedule(context.Background(), "session-1", []time.Duration{0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 || status.Capture.Status != "pending" {
		t.Fatalf("calls=%d status=%#v", calls, status)
	}
}

func TestGetStatusRetriesInitialNotFound(t *testing.T) {
	calls := 0
	service := New(requestFunc(func(context.Context, string, string, any) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, &mimirapi.Error{StatusCode: 404, Status: "404 Not Found", Body: `{"error":"session not found"}`}
		}
		return []byte(`{"session_id":"session-1","capture":{"status":"saved","saved_exchanges":1,"failed_exchanges":0,"pending_exchanges":0},"receipt":{"label":"Saved to Mimir","detail":"1 exchange in this session"},"outcome":"unresolved"}`), nil
	}))
	status, err := service.GetStatusWithSchedule(context.Background(), "session-1", []time.Duration{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || status.Capture.Status != "saved" {
		t.Fatalf("calls=%d status=%#v", calls, status)
	}
}

func TestGetStatusDoesNotHideVerificationFailure(t *testing.T) {
	calls := 0
	wantErr := errors.New("database unavailable")
	service := New(requestFunc(func(context.Context, string, string, any) ([]byte, error) {
		calls++
		if calls > 1 {
			return nil, wantErr
		}
		return []byte(`{"session_id":"session-1","capture":{"status":"pending","saved_exchanges":0,"failed_exchanges":0,"pending_exchanges":1},"outcome":"unresolved"}`), nil
	}))
	if _, err := service.GetStatusWithSchedule(context.Background(), "session-1", []time.Duration{0}); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v", err)
	}
}

func TestStatusJSONPreservesOldAndFutureFields(t *testing.T) {
	service := New(requestFunc(func(context.Context, string, string, any) ([]byte, error) {
		return []byte(`{"session_id":"session-1","capture":{"status":"saved","saved_exchanges":1,"failed_exchanges":0,"pending_exchanges":0,"last_saved_at":null},"outcome":"unresolved","outcome_src":null,"receipt":{"future_receipt_field":"kept"},"future_field":{"kept":true}}`), nil
	}))
	status, err := service.GetStatusWithSchedule(context.Background(), "session-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := StatusJSON(status)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	receipt := output["receipt"].(map[string]any)
	if output["future_field"] == nil || receipt["future_receipt_field"] != "kept" || output["outcome_src"] != nil || status.Receipt.Label != "Saved to Mimir" {
		t.Fatalf("output=%s status=%#v", data, status)
	}
}

func TestStatusJSONPreservesUnknownLargeIntegersExactly(t *testing.T) {
	const large = "9007199254740993123456789"
	service := New(requestFunc(func(context.Context, string, string, any) ([]byte, error) {
		return []byte(`{"session_id":"session-1","capture":{"status":"saved","saved_exchanges":1,"failed_exchanges":0,"pending_exchanges":0},"outcome":"unresolved","receipt":{"future_sequence":` + large + `},"future_sequence":` + large + `}`), nil
	}))
	status, err := service.GetStatusWithSchedule(context.Background(), "session-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := StatusJSON(status)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]json.RawMessage
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	var receipt map[string]json.RawMessage
	if err := json.Unmarshal(output["receipt"], &receipt); err != nil {
		t.Fatal(err)
	}
	if string(output["future_sequence"]) != large || string(receipt["future_sequence"]) != large {
		t.Fatalf("large integer changed: %s", data)
	}
}
