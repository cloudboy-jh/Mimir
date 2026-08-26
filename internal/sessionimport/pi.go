package sessionimport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PiAdapter struct {
	Roots            []string
	MaxFiles         int
	MaxFileBytes     int64
	MaxLineBytes     int
	MaxLines         int
	MaxMessages      int
	MaxExchangeBytes int
}

func NewPiAdapter(roots ...string) PiAdapter { return PiAdapter{Roots: roots} }
func (PiAdapter) Name() string               { return "pi" }

func (a PiAdapter) Discover(ctx context.Context) ([]Session, error) {
	return a.DiscoverWithOptions(ctx, Options{})
}

func (a PiAdapter) DiscoverWithOptions(ctx context.Context, options Options) ([]Session, error) {
	if !selected(options.Sources, a.Name()) {
		return nil, nil
	}
	roots := a.Roots
	if len(roots) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR"))
		if base == "" {
			base = filepath.Join(home, ".pi", "agent")
		}
		roots = []string{filepath.Join(base, "sessions")}
	}
	maxFiles := a.MaxFiles
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	paths := []string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) && path == root {
					return fs.SkipDir
				}
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
				return nil
			}
			if len(paths) >= maxFiles {
				return errTooManyPiFiles
			}
			paths = append(paths, path)
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	sort.Strings(paths)
	result := make([]Session, 0, len(paths))
	for _, path := range paths {
		session, ok, err := a.readSession(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("reading Pi session %s: %w", path, err)
		}
		if ok && (len(options.SourceIDs) == 0 || exactSelected(options.SourceIDs, session.SourceID)) &&
			(options.Since.IsZero() || (!session.StartedAt.IsZero() && !session.StartedAt.Before(options.Since))) &&
			(options.Before.IsZero() || (!session.StartedAt.IsZero() && session.StartedAt.Before(options.Before))) {
			result = append(result, session)
		}
	}
	return result, nil
}

var errTooManyPiFiles = errors.New("Pi session file count exceeds limit")

type piRecord struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	Timestamp any            `json:"timestamp"`
	CWD       string         `json:"cwd"`
	Name      string         `json:"name"`
	Message   map[string]any `json:"message"`
}

func (a PiAdapter) readSession(ctx context.Context, path string) (Session, bool, error) {
	maxFile := a.MaxFileBytes
	if maxFile <= 0 {
		maxFile = DefaultMaxFileBytes
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Session{}, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFile {
		return Session{}, false, fmt.Errorf("file exceeds %d bytes or is not regular", maxFile)
	}
	file, err := os.Open(path)
	if err != nil {
		return Session{}, false, err
	}
	defer file.Close()
	maxLine := a.MaxLineBytes
	if maxLine <= 0 {
		maxLine = DefaultMaxLineBytes
	}
	maxLines := a.MaxLines
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxLine)
	records := make([]piRecord, 0)
	line := 0
	for scanner.Scan() {
		line++
		if line > maxLines {
			return Session{}, false, fmt.Errorf("line count exceeds %d", maxLines)
		}
		if err := ctx.Err(); err != nil {
			return Session{}, false, err
		}
		var record piRecord
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.UseNumber()
		if err := decoder.Decode(&record); err != nil {
			return Session{}, false, fmt.Errorf("line %d: %w", line, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return Session{}, false, fmt.Errorf("scanning JSONL: %w", err)
	}
	if len(records) == 0 {
		return Session{}, false, nil
	}
	var sourceID, cwd, title string
	var started, ended time.Time
	for _, record := range records {
		at := piTimestamp(record.Timestamp, record.Message["timestamp"])
		if !at.IsZero() {
			if started.IsZero() || at.Before(started) {
				started = at
			}
			if ended.IsZero() || at.After(ended) {
				ended = at
			}
		}
		if record.Type == "session" {
			if record.ID != "" {
				sourceID = record.ID
			}
			if record.CWD != "" {
				cwd = record.CWD
			}
		}
		if (record.Type == "session_info" || record.Type == "session_name") && record.Name != "" {
			title = record.Name
		}
	}
	if sourceID == "" {
		sourceID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if sourceID == "" {
		return Session{}, false, errors.New("session has no id")
	}
	sessionID := canonicalID("pi", sourceID)
	session := Session{ID: sessionID, SourceID: sourceID, Harness: "pi", Directory: cwd, Repo: repoName(cwd), Title: title, StartedAt: started, EndedAt: ended}
	contextMessages := make([]any, 0)
	turnIndex := 0
	maxMessages := a.MaxMessages
	if maxMessages <= 0 {
		maxMessages = DefaultMaxMessages
	}
	for recordIndex, record := range records {
		if record.Type != "message" || len(record.Message) == 0 {
			continue
		}
		message := record.Message
		if text(message["role"]) != "assistant" {
			contextMessages = appendBoundedMessage(contextMessages, boundedValue(message), maxMessages)
			continue
		}
		provider, model := text(message["provider"]), text(message["model"])
		timestamp := piTimestamp(message["timestamp"], record.Timestamp)
		currentTurn := turnIndex
		turnIndex++
		if strings.EqualFold(provider, "openrouter") {
			session.SkippedOpenRouter++
			contextMessages = appendBoundedMessage(contextMessages, boundedValue(message), maxMessages)
			continue
		}
		if provider == "" || model == "" || timestamp.IsZero() {
			session.SkippedInvalid++
			contextMessages = appendBoundedMessage(contextMessages, boundedValue(message), maxMessages)
			continue
		}
		usage := object(message["usage"])
		exchange := Exchange{ExchangeID: deterministicID("pi:", sessionID, strconv.FormatInt(timestamp.UnixMilli(), 10), strconv.Itoa(currentTurn)), Timestamp: timestamp, Provider: truncateUTF8(provider, 256), Model: truncateUTF8(model, 256), Request: map[string]any{"messages": append([]any(nil), contextMessages...)}, Response: map[string]any{"message": boundedValue(message)}, Tools: piTools(message, followingPiToolResults(records, recordIndex)), Usage: Usage{InputTokens: number(usage["input"]) + number(usage["cacheRead"]), OutputTokens: number(usage["output"])}, RequestKind: "primary", Title: truncateUTF8(title, 200)}
		fitted, err := fitExchange(exchange, a.MaxExchangeBytes)
		if err == nil {
			session.Exchanges = append(session.Exchanges, fitted)
		} else {
			session.SkippedInvalid++
		}
		contextMessages = appendBoundedMessage(contextMessages, boundedValue(message), maxMessages)
	}
	if session.StartedAt.IsZero() && len(session.Exchanges) != 0 {
		session.StartedAt = session.Exchanges[0].Timestamp
	}
	return session, true, nil
}

func piTimestamp(values ...any) time.Time {
	for _, value := range values {
		if parsed := parseTime(value); !parsed.IsZero() {
			return parsed
		}
	}
	return time.Time{}
}

func appendBoundedMessage(messages []any, value any, max int) []any {
	if value == nil {
		return messages
	}
	messages = append(messages, value)
	if len(messages) > max {
		messages = messages[len(messages)-max:]
	}
	return messages
}

func followingPiToolResults(records []piRecord, assistantIndex int) []map[string]any {
	results := []map[string]any{}
	for index := assistantIndex + 1; index < len(records); index++ {
		if records[index].Type != "message" || len(records[index].Message) == 0 {
			continue
		}
		role := text(records[index].Message["role"])
		if role == "assistant" || role == "user" {
			break
		}
		if role == "toolResult" || role == "tool" {
			results = append(results, records[index].Message)
		}
	}
	return results
}

func piTools(message map[string]any, results []map[string]any) []ToolActivity {
	result := []ToolActivity{}
	consumed := make(map[int]bool)
	for _, raw := range array(message["content"]) {
		block := object(raw)
		kind := text(block["type"])
		if kind != "toolCall" && kind != "tool_use" {
			continue
		}
		name := text(block["name"])
		if name == "" {
			name = text(block["toolName"])
		}
		if !safeToolName.MatchString(name) {
			continue
		}
		inputValue := block["arguments"]
		if inputValue == nil {
			inputValue = block["input"]
		}
		if encoded, ok := inputValue.(string); ok {
			var decoded any
			if json.Unmarshal([]byte(encoded), &decoded) == nil {
				inputValue = decoded
			}
		}
		input, _ := boundedValue(inputValue).(map[string]any)
		if input == nil {
			input = map[string]any{}
		}
		callID := text(block["id"])
		if callID == "" {
			callID = text(block["toolCallId"])
		}
		matched := -1
		for index, candidate := range results {
			if consumed[index] {
				continue
			}
			candidateID := text(candidate["toolCallId"])
			if candidateID == "" {
				candidateID = text(candidate["tool_call_id"])
			}
			candidateName := text(candidate["toolName"])
			if candidateName == "" {
				candidateName = text(candidate["name"])
			}
			if callID != "" && candidateID == callID || candidateName == name {
				matched = index
				break
			}
		}
		activity := ToolActivity{Name: name, Input: input, Status: "succeeded"}
		if matched >= 0 {
			consumed[matched] = true
			candidate := results[matched]
			if piToolResultFailed(candidate) {
				activity.Status = "failed"
			}
			if content := candidate["content"]; content != nil {
				encoded, _ := json.Marshal(boundedValue(content))
				activity.Output = truncateUTF8(strings.Trim(string(encoded), `"`), 64<<10)
			}
		}
		result = append(result, activity)
	}
	for index, candidate := range results {
		if consumed[index] {
			continue
		}
		name := text(candidate["toolName"])
		if name == "" {
			name = text(candidate["name"])
		}
		if !safeToolName.MatchString(name) {
			continue
		}
		activity := ToolActivity{Name: name, Input: map[string]any{}, Status: "succeeded"}
		if piToolResultFailed(candidate) {
			activity.Status = "failed"
		}
		if content := candidate["content"]; content != nil {
			encoded, _ := json.Marshal(boundedValue(content))
			activity.Output = truncateUTF8(strings.Trim(string(encoded), `"`), 64<<10)
		}
		result = append(result, activity)
	}
	return result
}

func piToolResultFailed(result map[string]any) bool {
	if result["isError"] == true || result["is_error"] == true || text(result["status"]) == "error" {
		return true
	}
	switch value := result["exitCode"].(type) {
	case float64:
		return value != 0
	case json.Number:
		parsed, err := value.Int64()
		return err == nil && parsed != 0
	default:
		return false
	}
}
