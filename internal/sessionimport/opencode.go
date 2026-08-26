package sessionimport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
)

type CommandFunc func(context.Context, string, ...string) ([]byte, error)

type OpenCodeAdapter struct {
	Command          CommandFunc
	MaxSessions      int
	MaxCommandBytes  int
	MaxExchangeBytes int
}

func NewOpenCodeAdapter() OpenCodeAdapter { return OpenCodeAdapter{} }
func (OpenCodeAdapter) Name() string      { return "opencode" }

func (a OpenCodeAdapter) Discover(ctx context.Context) ([]Session, error) {
	return a.DiscoverWithOptions(ctx, Options{})
}

func (a OpenCodeAdapter) DiscoverWithOptions(ctx context.Context, options Options) ([]Session, error) {
	if !selected(options.Sources, a.Name()) {
		return nil, nil
	}
	limit := a.MaxSessions
	if limit <= 0 {
		limit = DefaultMaxSessions
	}
	var items []any
	if len(options.SourceIDs) != 0 {
		ids := uniqueSorted(options.SourceIDs)
		items = make([]any, 0, len(ids))
		for _, id := range ids {
			items = append(items, map[string]any{"id": id})
		}
	} else {
		output, err := a.run(ctx, "opencode", "session", "list", "--format", "json", "--max-count", fmt.Sprint(limit))
		if err != nil {
			return nil, fmt.Errorf("listing OpenCode sessions: %w", err)
		}
		items, err = decodeArray(output)
		if err != nil {
			return nil, fmt.Errorf("decoding OpenCode session list: %w", err)
		}
	}
	if len(items) > limit {
		return nil, fmt.Errorf("OpenCode session count exceeds %d", limit)
	}
	sort.SliceStable(items, func(i, j int) bool { return text(object(items[i])["id"]) < text(object(items[j])["id"]) })
	result := make([]Session, 0, len(items))
	for _, item := range items {
		listed := object(item)
		id := text(listed["id"])
		if id == "" {
			continue
		}
		exported, err := a.run(ctx, "opencode", "export", id)
		if err != nil {
			return nil, fmt.Errorf("exporting OpenCode session %s: %w", id, err)
		}
		session, err := a.decodeExport(exported, listed)
		if err != nil {
			return nil, fmt.Errorf("decoding OpenCode session %s: %w", id, err)
		}
		if (options.Since.IsZero() || (!session.StartedAt.IsZero() && !session.StartedAt.Before(options.Since))) &&
			(options.Before.IsZero() || (!session.StartedAt.IsZero() && session.StartedAt.Before(options.Before))) {
			result = append(result, session)
		}
	}
	return result, nil
}

func (a OpenCodeAdapter) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	limit := a.MaxCommandBytes
	if limit <= 0 {
		limit = DefaultMaxCommandBytes
	}
	if a.Command != nil {
		output, err := a.Command(ctx, name, args...)
		if len(output) > limit {
			return nil, fmt.Errorf("command output exceeds %d bytes", limit)
		}
		return output, err
	}
	command := exec.CommandContext(ctx, name, args...)
	stdout := &limitBuffer{remaining: limit}
	stderr := &limitBuffer{remaining: 64 << 10}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		if errors.Is(stdout.err, errOutputLimit) {
			return nil, fmt.Errorf("command output exceeds %d bytes", limit)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (a OpenCodeAdapter) decodeExport(data []byte, listed map[string]any) (Session, error) {
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return Session{}, err
	}
	info := object(root["info"])
	if len(info) == 0 {
		info = root
	}
	sourceID := text(info["id"])
	if sourceID == "" {
		sourceID = text(listed["id"])
	}
	if sourceID == "" {
		return Session{}, errors.New("export has no session id")
	}
	session := Session{ID: canonicalID("opencode", sourceID), SourceID: sourceID, Harness: "opencode"}
	session.Title = text(info["title"])
	if session.Title == "" {
		session.Title = text(listed["title"])
	}
	directory := text(info["directory"])
	if directory == "" {
		directory = text(listed["directory"])
	}
	session.Repo = repoName(directory)
	session.Directory = directory
	times := object(info["time"])
	session.StartedAt = parseTime(times["created"])
	session.EndedAt = parseTime(times["updated"])
	messages := array(root["messages"])
	if len(messages) == 0 {
		messages = array(root["data"])
	}
	records := make(map[string]map[string]any, len(messages))
	parts := make(map[string][]any, len(messages))
	for _, raw := range messages {
		record := object(raw)
		messageInfo := object(record["info"])
		if len(messageInfo) == 0 {
			messageInfo = record
		}
		id := text(messageInfo["id"])
		if id == "" {
			continue
		}
		records[id] = messageInfo
		parts[id] = array(record["parts"])
	}
	for _, raw := range messages {
		record := object(raw)
		assistant := object(record["info"])
		if len(assistant) == 0 {
			assistant = record
		}
		if text(assistant["role"]) != "assistant" {
			continue
		}
		provider := text(assistant["providerID"])
		if strings.EqualFold(provider, "openrouter") {
			session.SkippedOpenRouter++
			continue
		}
		id, parentID := text(assistant["id"]), text(assistant["parentID"])
		user := records[parentID]
		if id == "" || parentID == "" || text(user["role"]) != "user" {
			session.SkippedInvalid++
			continue
		}
		timeInfo := object(assistant["time"])
		created, completed := parseTime(timeInfo["created"]), parseTime(timeInfo["completed"])
		if created.IsZero() || completed.IsZero() {
			session.SkippedInvalid++
			continue
		}
		model := text(assistant["modelID"])
		if provider == "" || model == "" {
			session.SkippedInvalid++
			continue
		}
		tokens, cache := object(assistant["tokens"]), object(object(assistant["tokens"])["cache"])
		exchange := Exchange{ExchangeID: canonicalID("opencode", id), Timestamp: completed, Provider: truncateUTF8(provider, 256), Model: truncateUTF8(model, 256), RequestKind: "primary", Tools: openCodeTools(parts[id]), Usage: Usage{InputTokens: number(tokens["input"]) + number(cache["read"]), OutputTokens: number(tokens["output"])}, LatencyMS: completed.Sub(created).Milliseconds(), Title: truncateUTF8(session.Title, 200)}
		if exchange.LatencyMS < 0 {
			exchange.LatencyMS = 0
		}
		exchange.Request = map[string]any{"message_id": parentID, "messages": []any{map[string]any{"role": "user", "content": boundedValue(parts[parentID])}}}
		exchange.Response = map[string]any{"message_id": id, "parent_message_id": parentID, "role": "assistant", "parts": boundedValue(parts[id])}
		fitted, err := fitExchange(exchange, a.MaxExchangeBytes)
		if err != nil {
			session.SkippedInvalid++
			continue
		}
		session.Exchanges = append(session.Exchanges, fitted)
	}
	if session.StartedAt.IsZero() && len(session.Exchanges) != 0 {
		session.StartedAt = session.Exchanges[0].Timestamp
	}
	return session, nil
}

func openCodeTools(parts []any) []ToolActivity {
	result := []ToolActivity{}
	for _, raw := range parts {
		part := object(raw)
		if text(part["type"]) != "tool" {
			continue
		}
		name := text(part["tool"])
		if !safeToolName.MatchString(name) {
			continue
		}
		state := object(part["state"])
		input, _ := boundedValue(state["input"]).(map[string]any)
		if input == nil {
			input = map[string]any{}
		}
		status := "succeeded"
		if value := text(state["status"]); value == "error" || value == "failed" {
			status = "failed"
		}
		output := state["output"]
		if status == "failed" && state["error"] != nil {
			output = state["error"]
		}
		activity := ToolActivity{Name: name, Input: input, Status: status}
		if output != nil {
			encoded, _ := json.Marshal(boundedValue(output))
			activity.Output = truncateUTF8(strings.Trim(string(encoded), `"`), 64<<10)
		}
		result = append(result, activity)
	}
	return result
}

func decodeArray(data []byte) ([]any, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if values := array(value); values != nil {
		return values, nil
	}
	root := object(value)
	for _, key := range []string{"sessions", "data"} {
		if values := array(root[key]); values != nil {
			return values, nil
		}
	}
	return nil, errors.New("expected a JSON array")
}

var errOutputLimit = errors.New("output limit exceeded")

type limitBuffer struct {
	bytes.Buffer
	remaining int
	err       error
}

func (b *limitBuffer) Write(data []byte) (int, error) {
	if len(data) > b.remaining {
		b.err = errOutputLimit
		return 0, b.err
	}
	b.remaining -= len(data)
	return b.Buffer.Write(data)
}

var _ io.Writer = (*limitBuffer)(nil)
