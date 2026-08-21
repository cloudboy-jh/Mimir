package sessions

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
)

func TestReconcileExhaustsDatabaseAndR2Cursors(t *testing.T) {
	calls := 0
	service := New(requestFunc(func(_ context.Context, method, path string, _ any) ([]byte, error) {
		calls++
		if method != "POST" {
			t.Fatalf("method = %q", method)
		}
		parsed, err := url.Parse(path)
		if err != nil || parsed.Path != "/reconcile" {
			t.Fatalf("path = %q err=%v", path, err)
		}
		if calls == 1 {
			return []byte(`{"scanned":1,"database_cursor":"db-next","finalized":{"exchange_ids":["saved-1"]},"pending":{"exchange_ids":[],"stale_exchange_ids":[]},"missing_saved":{"exchange_ids":[],"session_ids":[]},"orphans":{"r2_keys":["log/orphan-1.json"],"cursor":"r2-next"},"empty_sessions_removed":{"session_ids":["empty-pi-1"]}}`), nil
		}
		if calls == 2 {
			if parsed.Query().Get("database_cursor") != "db-next" || parsed.Query().Get("cursor") != "r2-next" {
				t.Fatalf("continuation query %s", parsed.RawQuery)
			}
			return []byte(`{"scanned":1,"database_cursor":null,"finalized":{"exchange_ids":[]},"pending":{"exchange_ids":["pending-1"],"stale_exchange_ids":["pending-1"]},"missing_saved":{"exchange_ids":[],"session_ids":[]},"orphans":{"r2_keys":["log/orphan-2.json"],"cursor":"r2-final"}}`), nil
		}
		if parsed.Query().Get("scan_database") != "false" || parsed.Query().Get("cursor") != "r2-final" {
			t.Fatalf("R2 continuation query %s", parsed.RawQuery)
		}
		return []byte(`{"scanned":0,"database_cursor":null,"finalized":{"exchange_ids":[]},"pending":{"exchange_ids":[],"stale_exchange_ids":[]},"missing_saved":{"exchange_ids":[],"session_ids":[]},"orphans":{"r2_keys":["log/orphan-3.json"],"cursor":null}}`), nil
	}))
	data, err := service.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var report ReconcileReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || report.Pages != 3 || len(report.Pending) != 1 || report.Pending[0] != "pending-1" || len(report.Orphans) != 3 || len(report.RemovedEmptySessions) != 1 || report.RemovedEmptySessions[0] != "empty-pi-1" {
		t.Fatalf("calls=%d report=%#v", calls, report)
	}
}

func TestReconcileReturnsEmptyArrays(t *testing.T) {
	service := New(requestFunc(func(context.Context, string, string, any) ([]byte, error) {
		return []byte(`{"scanned":0,"database_cursor":null,"finalized":{"exchange_ids":[]},"pending":{"exchange_ids":[],"stale_exchange_ids":[]},"missing_saved":{"exchange_ids":[],"session_ids":[]},"orphans":{"r2_keys":[],"cursor":null}}`), nil
	}))
	data, err := service.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"finalized_exchange_ids", "pending_exchange_ids", "stale_pending_exchange_ids", "missing_saved_exchange_ids", "affected_session_ids", "orphan_r2_keys", "removed_empty_session_ids"} {
		if values, ok := output[key].([]any); !ok || len(values) != 0 {
			t.Fatalf("%s = %#v", key, output[key])
		}
	}
}
