package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mimirassets "github.com/cloudboy-jh/mimir"
)

func TestOfficialPayloadsProduceCanonicalExchanges(t *testing.T) {
	tests := []struct {
		harness, prompt, complete string
	}{
		{"claude-code", `{"hook_event_name":"UserPromptSubmit","session_id":"claude-1","prompt_id":"p1","cwd":"/repo/mimir","prompt":"fix it"}`, `{"hook_event_name":"Stop","session_id":"claude-1","prompt_id":"p1","last_assistant_message":"fixed"}`},
		{"codex", `{"hook_event_name":"UserPromptSubmit","session_id":"codex-1","turn_id":"t1","cwd":"/repo/mimir","model":"gpt-5","prompt":"fix it"}`, `{"hook_event_name":"Stop","session_id":"codex-1","turn_id":"t1","model":"gpt-5","last_assistant_message":"fixed"}`},
		{"cursor", `{"hook_event_name":"beforeSubmitPrompt","conversation_id":"cursor-1","generation_id":"g1","workspace_roots":["/repo/mimir"],"model":"claude","prompt":"fix it"}`, `{"hook_event_name":"afterAgentResponse","conversation_id":"cursor-1","generation_id":"g1","model":"claude","text":"fixed"}`},
	}
	for _, test := range tests {
		t.Run(test.harness, func(t *testing.T) {
			var sent []Delivery
			service := Service{Home: t.TempDir(), Now: func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) }, Deliver: func(_ context.Context, delivery Delivery) error {
				sent = append(sent, delivery)
				return nil
			}}
			if err := service.Ingest(context.Background(), test.harness, strings.NewReader(test.prompt)); err != nil {
				t.Fatal(err)
			}
			if err := service.Ingest(context.Background(), test.harness, strings.NewReader(test.complete)); err != nil {
				t.Fatal(err)
			}
			if len(sent) != 1 || sent[0].Kind != "exchange" || sent[0].SessionID == "" {
				t.Fatalf("deliveries = %#v", sent)
			}
			request := sent[0].Body["request"].(map[string]any)
			response := sent[0].Body["response"].(map[string]any)
			if !strings.Contains(toJSON(request), "fix it") || response["content"] != "fixed" || sent[0].Body["request_kind"] != "primary" {
				t.Fatalf("exchange = %#v", sent[0].Body)
			}
		})
	}
}

func TestInputIsBounded(t *testing.T) {
	service := Service{Home: t.TempDir()}
	err := service.Ingest(context.Background(), "codex", bytes.NewReader(bytes.Repeat([]byte("x"), MaxInputBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v", err)
	}
}

func TestEmbeddedManifestsUseHiddenHookCommand(t *testing.T) {
	for _, path := range []string{
		"plugins/claude-code/.claude-plugin/plugin.json",
		"plugins/claude-code/hooks/hooks.json",
		"plugins/codex/hooks.json",
		"plugins/cursor/hooks.json",
	} {
		data, err := mimirassets.Bundle.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if strings.HasSuffix(path, "hooks.json") && !strings.Contains(string(data), "mimir _hook") {
			t.Fatalf("%s does not invoke the managed hook adapter", path)
		}
	}
	data, err := mimirassets.Bundle.ReadFile("plugins/codex/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var codex struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &codex); err != nil {
		t.Fatal(err)
	}
	for event, groups := range codex.Hooks {
		if len(groups) == 0 || len(groups[0].Hooks) == 0 || groups[0].Hooks[0].Type != "command" || groups[0].Hooks[0].Command == "" {
			t.Fatalf("Codex %s handler is not a typed command", event)
		}
	}
}

func TestOutboxRetriesOnNextInvocationAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	failing := Service{Home: home, Now: time.Now, Deliver: func(context.Context, Delivery) error { return errors.New("offline") }}
	input := `{"hook_event_name":"SessionStart","session_id":"session-1","source":"startup"}`
	if err := failing.Ingest(context.Background(), "codex", strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	if err := failing.Ingest(context.Background(), "codex", strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "hook-outbox"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 { // one stable load plus heartbeat payloads with distinct timestamps
		t.Fatalf("outbox entries = %d", len(entries))
	}
	var delivered int
	recovered := Service{Home: home, Deliver: func(context.Context, Delivery) error { delivered++; return nil }}
	if err := recovered.Ingest(context.Background(), "codex", strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"session-1","turn_id":"t1","prompt":"next"}`)); err != nil {
		t.Fatal(err)
	}
	entries, _ = os.ReadDir(filepath.Join(home, "hook-outbox"))
	if delivered != 3 || len(entries) != 0 {
		t.Fatalf("delivered=%d remaining=%d", delivered, len(entries))
	}
}

func TestOutboxUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	home := t.TempDir()
	service := Service{Home: home, Deliver: func(context.Context, Delivery) error { return errors.New("offline") }}
	if err := service.Ingest(context.Background(), "cursor", strings.NewReader(`{"hook_event_name":"sessionStart","conversation_id":"c1"}`)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "hook-outbox")
	info, _ := os.Stat(dir)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	info, _ = entries[0].Info()
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o", info.Mode().Perm())
	}
}

func TestCurrentInputIsPersistedBeforeOfflineBacklogFlush(t *testing.T) {
	home := t.TempDir()
	var inputRead atomic.Bool
	reader := &observedReader{Reader: strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"new"}`), done: &inputRead}
	service := Service{Home: home, Now: time.Now, Deliver: func(context.Context, Delivery) error {
		if !inputRead.Load() {
			t.Fatal("delivery started before current stdin was fully read")
		}
		return errors.New("offline")
	}}
	if err := service.Ingest(context.Background(), "codex", reader); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "hook-outbox"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("persisted entries=%d, err=%v", len(entries), err)
	}
}

func TestFlushStopsAtFirstFailureAndHonorsTimeout(t *testing.T) {
	home := t.TempDir()
	service := Service{Home: home, Now: time.Now, Deliver: func(context.Context, Delivery) error { return errors.New("offline") }}
	if err := service.Ingest(context.Background(), "codex", strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"s1"}`)); err != nil {
		t.Fatal(err)
	}
	calls := 0
	service.Deliver = func(context.Context, Delivery) error { calls++; return errors.New("still offline") }
	if err := service.Flush(context.Background()); err == nil || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
	service.Deliver = func(ctx context.Context, _ Delivery) error { <-ctx.Done(); return ctx.Err() }
	started := time.Now()
	if err := service.Flush(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("flush exceeded bound: %v", elapsed)
	}
}

func TestFlushCountIsBounded(t *testing.T) {
	home := t.TempDir()
	service := Service{Home: home, Now: time.Now}
	if err := service.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxFlushItems+5; i++ {
		delivery := Delivery{Kind: "event", Harness: "codex", SessionID: fmt.Sprintf("s-%d", i), Body: map[string]any{"sequence": i}}
		if err := service.queue(delivery); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	service.Deliver = func(context.Context, Delivery) error { calls++; return nil }
	if err := service.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "hook-outbox"))
	if err != nil || calls != maxFlushItems || len(entries) != 5 {
		t.Fatalf("calls=%d remaining=%d err=%v", calls, len(entries), err)
	}
}

func TestCompactionAndSessionStartPreservePromptStateAndTitle(t *testing.T) {
	home := t.TempDir()
	var sent []Delivery
	service := Service{Home: home, Now: time.Now, key: storageKey("machine-token"), Deliver: func(_ context.Context, delivery Delivery) error {
		sent = append(sent, delivery)
		return nil
	}}
	inputs := []string{
		`{"hook_event_name":"UserPromptSubmit","session_id":"s1","prompt_id":"p1","prompt":"keep me","session_title":"Hook title"}`,
		`{"hook_event_name":"PreCompact","session_id":"s1"}`,
		`{"hook_event_name":"SessionStart","session_id":"s1","source":"compact"}`,
		`{"hook_event_name":"Stop","session_id":"s1","last_assistant_message":"kept"}`,
	}
	for _, input := range inputs {
		if err := service.Ingest(context.Background(), "claude-code", strings.NewReader(input)); err != nil {
			t.Fatal(err)
		}
	}
	var exchange Delivery
	for _, delivery := range sent {
		if delivery.Kind == "exchange" {
			exchange = delivery
		}
	}
	if exchange.Body == nil || exchange.Body["title"] != "Hook title" || !strings.Contains(toJSON(exchange.Body), "keep me") {
		t.Fatalf("exchange = %#v", exchange)
	}
	if _, err := os.Stat(service.statePath("s1")); !os.IsNotExist(err) {
		t.Fatalf("prompt state remains after queued exchange: %v", err)
	}
}

func TestOutboxFullPreservesPromptState(t *testing.T) {
	home := t.TempDir()
	service := Service{Home: home, Now: time.Now, Deliver: func(context.Context, Delivery) error { return errors.New("offline") }}
	if err := service.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := service.Ingest(context.Background(), "codex", strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","turn_id":"t1","prompt":"keep me"}`)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "hook-outbox")
	for i := 0; i < maxOutboxItems; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%04d.json", i)), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	err := service.Ingest(context.Background(), "codex", strings.NewReader(`{"hook_event_name":"Stop","session_id":"s1","turn_id":"t1","last_assistant_message":"response"}`))
	if err == nil || !strings.Contains(err.Error(), "outbox is full") {
		t.Fatalf("error = %v", err)
	}
	state, err := service.readState("s1")
	if err != nil || state.Prompt != "keep me" {
		t.Fatalf("state=%#v err=%v", state, err)
	}
}

func TestStaleStateAndOutboxAreRemoved(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	service := Service{Home: home, Now: func() time.Time { return now }}
	if err := service.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	statePath := service.statePath("stale")
	outboxPath := filepath.Join(home, "hook-outbox", "stale.json")
	if err := os.WriteFile(statePath, []byte(`{"prompt":"secret","at":"2020-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outboxPath, []byte(`{"kind":"event"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-outboxMaxAge - time.Hour)
	for _, path := range []string{statePath, outboxPath} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.cleanupStale(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{statePath, outboxPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale path remains %s: %v", path, err)
		}
	}
}

func TestProtectedStorageAndPlaintextMigration(t *testing.T) {
	home := t.TempDir()
	service := Service{Home: home, Now: time.Now, key: storageKey("machine-token"), Deliver: func(context.Context, Delivery) error { return errors.New("offline") }}
	if err := service.Ingest(context.Background(), "codex", strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","prompt":"prompt secret"}`)); err != nil {
		t.Fatal(err)
	}
	stateData, err := os.ReadFile(service.statePath("s1"))
	if err != nil || bytes.Contains(stateData, []byte("prompt secret")) {
		t.Fatalf("state plaintext exposed, err=%v", err)
	}
	if err := service.Ingest(context.Background(), "codex", strings.NewReader(`{"hook_event_name":"Stop","session_id":"s1","last_assistant_message":"response secret"}`)); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(home, "hook-outbox"))
	if len(entries) != 1 {
		t.Fatalf("outbox entries = %d", len(entries))
	}
	queuedPath := filepath.Join(home, "hook-outbox", entries[0].Name())
	queued, _ := os.ReadFile(queuedPath)
	if bytes.Contains(queued, []byte("prompt secret")) || bytes.Contains(queued, []byte("response secret")) {
		t.Fatal("queued exchange content is plaintext")
	}

	plain := Delivery{Kind: "event", Harness: "codex", SessionID: "legacy", Body: map[string]any{"secret": "legacy secret"}}
	plainData, _ := json.Marshal(plain)
	legacyPath := filepath.Join(home, "hook-outbox", "000-legacy.json")
	if err := os.WriteFile(legacyPath, plainData, 0o600); err != nil {
		t.Fatal(err)
	}
	service.Deliver = func(context.Context, Delivery) error { return errors.New("offline") }
	_ = service.Flush(context.Background())
	migrated, err := os.ReadFile(legacyPath)
	if err != nil || bytes.Contains(migrated, []byte("legacy secret")) {
		t.Fatalf("legacy queue was not protected before delivery: err=%v", err)
	}
}

func TestOutboxReplaysExchangeBeforeSessionEnd(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	service := Service{Home: home, Now: func() time.Time {
		now = now.Add(time.Microsecond)
		return now
	}, Deliver: func(context.Context, Delivery) error { return errors.New("offline") }}
	for _, input := range []string{
		`{"hook_event_name":"SessionStart","session_id":"ordered"}`,
		`{"hook_event_name":"UserPromptSubmit","session_id":"ordered","turn_id":"t1","prompt":"fix it"}`,
		`{"hook_event_name":"Stop","session_id":"ordered","turn_id":"t1","last_assistant_message":"fixed"}`,
		`{"hook_event_name":"SessionEnd","session_id":"ordered"}`,
	} {
		if err := service.Ingest(context.Background(), "codex", strings.NewReader(input)); err != nil {
			t.Fatal(err)
		}
	}
	var kinds []string
	service.Deliver = func(_ context.Context, delivery Delivery) error {
		kinds = append(kinds, delivery.Kind+":"+stringValue(delivery.Body["kind"]))
		return nil
	}
	if err := service.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	exchange, end := -1, -1
	for index, kind := range kinds {
		if kind == "exchange:" {
			exchange = index
		}
		if kind == "event:end" {
			end = index
		}
	}
	if exchange < 0 || end < 0 || exchange > end {
		t.Fatalf("delivery order = %v", kinds)
	}
}

func TestPersistentStorageKeySurvivesTokenRotation(t *testing.T) {
	home := t.TempDir()
	keys := make(chan []byte, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			key, err := loadStorageKey(home)
			keys <- key
			errs <- err
		}()
	}
	first, second := <-keys, <-keys
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("concurrent initialization produced different storage keys")
	}
	second, err := loadStorageKey(home)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || bytes.Equal(first, storageKey("rotated-token")) {
		t.Fatal("storage key was not stable and independent from the machine token")
	}
}

func TestConcurrentQueueAndFlushDoNotReorderOrDuplicate(t *testing.T) {
	home := t.TempDir()
	service := Service{Home: home, key: bytes.Repeat([]byte{7}, 32)}
	if err := service.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	const total = 20
	var queued sync.WaitGroup
	for index := range total {
		queued.Add(1)
		go func() {
			defer queued.Done()
			delivery := Delivery{Kind: "event", Harness: "codex", SessionID: fmt.Sprintf("s-%02d", index), Body: map[string]any{"sequence": index}}
			if err := service.queue(delivery); err != nil {
				t.Errorf("queue: %v", err)
			}
		}()
	}
	queued.Wait()
	entries, err := os.ReadDir(filepath.Join(home, "hook-outbox"))
	if err != nil || len(entries) != total {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
	for index, entry := range entries {
		expected := fmt.Sprintf("%020d", index+1)
		if !strings.HasPrefix(entry.Name(), expected) {
			t.Fatalf("entry %d = %s, want prefix %s", index, entry.Name(), expected)
		}
	}

	var delivered atomic.Int32
	service.Deliver = func(context.Context, Delivery) error {
		delivered.Add(1)
		time.Sleep(time.Millisecond)
		return nil
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- service.Flush(context.Background())
		}()
	}
	close(start)
	<-results
	<-results
	if delivered.Load() != total {
		t.Fatalf("delivered=%d want=%d", delivered.Load(), total)
	}
}

type observedReader struct {
	*strings.Reader
	done *atomic.Bool
}

func (r *observedReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		r.done.Store(true)
	}
	return n, err
}

func toJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
