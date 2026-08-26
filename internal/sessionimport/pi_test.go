package sessionimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPiDiscoverReconstructsBoundedTurnsAndSkipsOpenRouter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	lines := []string{
		`{"type":"session","id":"pi session/unsafe","timestamp":"2026-08-20T10:00:00Z","cwd":"/work/mimir"}`,
		`{"type":"session_info","name":"Imported Pi session","timestamp":"2026-08-20T10:00:01Z"}`,
		`{"type":"message","timestamp":"2026-08-20T10:00:02Z","message":{"role":"user","content":"do work","timestamp":1787220002000}}`,
		`{"type":"message","timestamp":"2026-08-20T10:00:03Z","message":{"role":"assistant","provider":"openrouter","model":"openai/gpt-5","content":"proxied","timestamp":1787220003000}}`,
		`{"type":"message","timestamp":"2026-08-20T10:00:04Z","message":{"role":"assistant","provider":"anthropic","model":"claude","content":[{"type":"toolCall","name":"read","arguments":{"path":"x"}}],"usage":{"input":5,"cacheRead":2,"output":3},"timestamp":1787220004000}}`,
		`{"type":"message","timestamp":"2026-08-20T10:00:05Z","message":{"role":"toolResult","toolName":"read","isError":true,"content":"read failed"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := NewPiAdapter(root)
	sessions, err := adapter.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d", len(sessions))
	}
	session := sessions[0]
	if session.ID != canonicalID("pi", "pi session/unsafe") || session.Repo != "mimir" || session.SkippedOpenRouter != 1 {
		t.Fatalf("session = %#v", session)
	}
	if len(session.Exchanges) != 1 {
		t.Fatalf("exchanges = %d", len(session.Exchanges))
	}
	exchange := session.Exchanges[0]
	wantID := deterministicID("pi:", session.ID, "1787220004000", "1")
	if exchange.ExchangeID != wantID || exchange.Usage != (Usage{InputTokens: 7, OutputTokens: 3}) {
		t.Fatalf("exchange = %#v", exchange)
	}
	if len(exchange.Tools) != 1 || exchange.Tools[0].Name != "read" || exchange.Tools[0].Status != "failed" || exchange.Tools[0].Output != "read failed" {
		t.Fatalf("tools = %#v", exchange.Tools)
	}
	again, err := adapter.Discover(context.Background())
	if err != nil || again[0].Exchanges[0].ExchangeID != exchange.ExchangeID {
		t.Fatalf("repeat ID = %v, %v", again, err)
	}
}

func TestPiDiscoveryFiltersSourceIDAndBefore(t *testing.T) {
	root := t.TempDir()
	for _, fixture := range []struct{ name, id, timestamp string }{
		{"a.jsonl", "wanted", "2026-08-19T10:00:00Z"},
		{"b.jsonl", "other", "2026-08-19T10:00:00Z"},
		{"c.jsonl", "wanted", "2026-08-21T10:00:00Z"},
	} {
		body := `{"type":"session","id":"` + fixture.id + `","timestamp":"` + fixture.timestamp + `"}` + "\n"
		if err := os.WriteFile(filepath.Join(root, fixture.name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sessions, err := (PiAdapter{Roots: []string{root}}).DiscoverWithOptions(context.Background(), Options{SourceIDs: []string{"wanted"}, Before: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].SourceID != "wanted" {
		t.Fatalf("sessions = %#v", sessions)
	}
}

func TestPiDiscoveryBoundsFilesAndLines(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.jsonl", "b.jsonl"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(`{"type":"session","id":"s"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (PiAdapter{Roots: []string{root}, MaxFiles: 1}).Discover(context.Background()); !errors.Is(err, errTooManyPiFiles) {
		t.Fatalf("file bound error = %v", err)
	}
	one := t.TempDir()
	if err := os.WriteFile(filepath.Join(one, "a.jsonl"), []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (PiAdapter{Roots: []string{one}, MaxLines: 1}).Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "line count exceeds 1") {
		t.Fatalf("line bound error = %v", err)
	}
}
