package tailnet

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const statusFixture = `{
  "BackendState": "Running",
  "Self": {"HostName": "laptop", "DNSName": "laptop.tail1234.ts.net.", "TailscaleIPs": ["100.64.0.1", "fd7a::1"], "Online": true},
  "CurrentTailnet": {"Name": "user@example.com"},
  "Peer": {
    "nodekey:b": {"HostName": "server", "DNSName": "server.tail1234.ts.net.", "Online": false, "LastSeen": "2026-11-18T10:00:00Z"},
    "nodekey:a": {"HostName": "desktop", "DNSName": "desktop.tail1234.ts.net.", "Online": true, "LastSeen": "0001-01-01T00:00:00Z"},
    "nodekey:c": {"HostName": "shared", "DNSName": "", "Online": true}
  }
}`

func TestParseTailscaleStatus(t *testing.T) {
	payload, err := Parse([]byte(statusFixture))
	if err != nil {
		t.Fatal(err)
	}
	if payload.Self.HostName != "laptop" || payload.Self.DNSName != "laptop.tail1234.ts.net" || payload.Self.TailnetName != "user@example.com" || len(payload.Self.IPs) != 2 {
		t.Fatalf("self = %#v", payload.Self)
	}
	if len(payload.Peers) != 2 || payload.Peers[0].DNSName != "desktop.tail1234.ts.net" || payload.Peers[1].DNSName != "server.tail1234.ts.net" {
		t.Fatalf("peers = %#v", payload.Peers)
	}
	if payload.Peers[0].LastSeen != nil || !payload.Peers[0].Online {
		t.Fatalf("online peer = %#v", payload.Peers[0])
	}
	if payload.Peers[1].LastSeen == nil || *payload.Peers[1].LastSeen != "2026-11-18T10:00:00Z" || payload.Peers[1].Online {
		t.Fatalf("offline peer = %#v", payload.Peers[1])
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var shape struct {
		Self  map[string]any   `json:"self"`
		Peers []map[string]any `json:"peers"`
	}
	if json.Unmarshal(data, &shape) != nil || shape.Self["dns_name"] != "laptop.tail1234.ts.net" || shape.Peers[0]["last_seen"] != nil {
		t.Fatalf("payload json = %s", data)
	}
}

func TestParseRejectsLoggedOutStatus(t *testing.T) {
	if _, err := Parse([]byte(`{"BackendState":"NeedsLogin","Self":{"HostName":"laptop","DNSName":""}}`)); err == nil {
		t.Fatal("expected logged-out status to be rejected")
	}
}

type recorder struct{ calls int }

func (r *recorder) Request(context.Context, string, string, any) ([]byte, error) {
	r.calls++
	return []byte(`{"ok":true}`), nil
}

func TestReportThrottled(t *testing.T) {
	original := statusJSON
	t.Cleanup(func() { statusJSON = original })
	statusJSON = func(context.Context) ([]byte, error) { return []byte(statusFixture), nil }
	home := t.TempDir()
	api := &recorder{}
	now := time.Now()
	ReportThrottled(context.Background(), api, home, now)
	ReportThrottled(context.Background(), api, home, now.Add(time.Minute))
	if api.calls != 1 {
		t.Fatalf("calls = %d, want 1", api.calls)
	}
	ReportThrottled(context.Background(), api, home, now.Add(ThrottleInterval+time.Second))
	if api.calls != 2 {
		t.Fatalf("calls = %d, want 2", api.calls)
	}
	if _, err := os.Stat(filepath.Join(home, stampFile)); err != nil {
		t.Fatal(err)
	}
}

func TestReportSilentWithoutTailscale(t *testing.T) {
	original := statusJSON
	t.Cleanup(func() { statusJSON = original })
	statusJSON = func(context.Context) ([]byte, error) { return nil, errUnavailable }
	api := &recorder{}
	reported, err := Report(context.Background(), api)
	if reported || err != nil || api.calls != 0 {
		t.Fatalf("reported=%v err=%v calls=%d", reported, err, api.calls)
	}
}
