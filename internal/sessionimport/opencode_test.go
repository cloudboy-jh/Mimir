package sessionimport

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeDiscoverUsesBoundedCommandsAndReconstructsDirectTurns(t *testing.T) {
	var calls []string
	adapter := OpenCodeAdapter{Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch strings.Join(args, " ") {
		case "session list --format json --max-count 1000":
			return []byte(`[{"id":"ses_1"}]`), nil
		case "export ses_1":
			return []byte(`{
                    "info":{"id":"ses_1","title":"Historical work","directory":"/work/mimir","time":{"created":1000,"updated":5000}},
                    "messages":[
                      {"info":{"id":"u1","role":"user","time":{"created":1100}},"parts":[{"type":"text","text":"fix it"}]},
                      {"info":{"id":"a1","parentID":"u1","role":"assistant","providerID":"anthropic","modelID":"claude","time":{"created":2000,"completed":3000},"tokens":{"input":7,"output":3,"cache":{"read":2}}},"parts":[{"type":"text","text":"fixed"},{"type":"tool","tool":"edit","state":{"status":"completed","input":{"path":"x"},"output":"ok"}}]},
                      {"info":{"id":"a2","parentID":"u1","role":"assistant","providerID":"openrouter","modelID":"openai/gpt-5","time":{"created":3500,"completed":4000}},"parts":[]}
                    ]}`), nil
		default:
			return nil, fmt.Errorf("unexpected command")
		}
	}, MaxCommandBytes: 1 << 20}
	sessions, err := adapter.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"opencode session list --format json --max-count 1000", "opencode export ses_1"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v", calls)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d", len(sessions))
	}
	session := sessions[0]
	if session.ID != "ses_1" || session.Repo != "mimir" || session.Title != "Historical work" || session.SkippedOpenRouter != 1 {
		t.Fatalf("session = %#v", session)
	}
	if len(session.Exchanges) != 1 {
		t.Fatalf("exchanges = %d", len(session.Exchanges))
	}
	exchange := session.Exchanges[0]
	if exchange.ExchangeID != "a1" || exchange.Provider != "anthropic" || exchange.Usage != (Usage{InputTokens: 9, OutputTokens: 3}) || exchange.LatencyMS != 1000 {
		t.Fatalf("exchange = %#v", exchange)
	}
	if len(exchange.Tools) != 1 || exchange.Tools[0].Name != "edit" {
		t.Fatalf("tools = %#v", exchange.Tools)
	}
}

func TestOpenCodeCommandOutputIsBounded(t *testing.T) {
	adapter := OpenCodeAdapter{MaxCommandBytes: 4, Command: func(context.Context, string, ...string) ([]byte, error) { return []byte("12345"), nil }}
	if _, err := adapter.Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenCodeSessionCountIsBounded(t *testing.T) {
	adapter := OpenCodeAdapter{MaxSessions: 1, Command: func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`[{"id":"a"},{"id":"b"}]`), nil
	}}
	if _, err := adapter.Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeds 1") {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenCodeExactIDsExportDirectlyAndApplyBefore(t *testing.T) {
	var calls []string
	adapter := OpenCodeAdapter{Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		id := args[len(args)-1]
		created := 1000
		if id == "new" {
			created = 3000
		}
		return []byte(fmt.Sprintf(`{"info":{"id":%q,"time":{"created":%d}},"messages":[]}`, id, created)), nil
	}}
	sessions, err := adapter.DiscoverWithOptions(context.Background(), Options{SourceIDs: []string{"new", "old", "old"}, Before: time.UnixMilli(2000)})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"export new", "export old"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v", calls)
	}
	if len(sessions) != 1 || sessions[0].SourceID != "old" {
		t.Fatalf("sessions = %#v", sessions)
	}
}
