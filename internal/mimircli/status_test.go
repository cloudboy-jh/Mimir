package mimircli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestGetSessionStatusWaitsForSavedCapture(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		status := "pending"
		pending, saved := 1, 0
		label, detail := "Saving to Mimir...", "1 exchange"
		if calls > 1 {
			status, pending, saved = "saved", 0, 1
			label, detail = "Saved to Mimir", "1 exchange in this session"
		}
		_, _ = w.Write([]byte(`{"session_id":"session-1","capture":{"status":"` + status + `","saved_exchanges":` + strconv.Itoa(saved) + `,"failed_exchanges":0,"pending_exchanges":` + strconv.Itoa(pending) + `},"receipt":{"label":"` + label + `","detail":"` + detail + `","action_label":"View session"},"dashboard_url":"https://mimir.example/dashboard/sessions/session-1","outcome":"unresolved"}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	status, err := getSessionStatusWithSchedule(context.Background(), "session-1", []time.Duration{0})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || status.Capture.Status != "saved" || status.Capture.SavedExchanges != 1 {
		t.Fatalf("calls=%d status=%#v", calls, status)
	}
}

func TestGetSessionStatusReturnsLatestPendingAfterBoundedWait(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"session_id":"session-1","capture":{"status":"pending","saved_exchanges":0,"failed_exchanges":0,"pending_exchanges":1},"receipt":{"label":"Saving to Mimir...","detail":"1 exchange","action_label":"View session"},"dashboard_url":"https://mimir.example/dashboard/sessions/session-1","outcome":"unresolved"}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	status, err := getSessionStatusWithSchedule(context.Background(), "session-1", []time.Duration{0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 || status.Capture.Status != "pending" {
		t.Fatalf("calls=%d status=%#v", calls, status)
	}
}

func TestGetSessionStatusRetriesInitialNotFound(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"session_id":"session-1","capture":{"status":"saved","saved_exchanges":1,"failed_exchanges":0,"pending_exchanges":0},"receipt":{"label":"Saved to Mimir","detail":"1 exchange in this session","action_label":null},"dashboard_url":null,"outcome":"unresolved"}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	status, err := getSessionStatusWithSchedule(context.Background(), "session-1", []time.Duration{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || status.Capture.Status != "saved" {
		t.Fatalf("calls=%d status=%#v", calls, status)
	}
}

func TestGetSessionStatusDoesNotHideVerificationFailure(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 1 {
			http.Error(w, `{"error":"database unavailable"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"session_id":"session-1","capture":{"status":"pending","saved_exchanges":0,"failed_exchanges":0,"pending_exchanges":1},"outcome":"unresolved"}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := getSessionStatusWithSchedule(context.Background(), "session-1", []time.Duration{0}); err == nil {
		t.Fatal("verification failure was hidden")
	}
}

func TestSessionStatusPreservesOldAndFutureJSONFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"session_id":"session-1","capture":{"status":"saved","saved_exchanges":1,"failed_exchanges":0,"pending_exchanges":0,"last_saved_at":null},"outcome":"unresolved","outcome_src":null,"receipt":{"future_receipt_field":"kept"},"future_field":{"kept":true}}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	status, err := getSessionStatusWithSchedule(context.Background(), "session-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := sessionStatusJSON(status)
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
