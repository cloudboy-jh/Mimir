package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/sessions"
)

type Requester interface {
	Request(context.Context, string, string, any) ([]byte, error)
}

type SearchFunc func(context.Context, string) ([]byte, error)

type Domain struct {
	API      Requester
	Sessions sessions.Service
	Search   SearchFunc
}

func New(version string, api Requester, sessionService sessions.Service, search SearchFunc) Server {
	domain := Domain{API: api, Sessions: sessionService, Search: search}
	return Server{Version: version, Tools: Tools, Call: domain.CallTool}
}

func Tools() []map[string]any {
	canonical := map[string]any{"type": "string", "enum": []string{"landed", "discarded", "abandoned", "unresolved"}}
	legacy := map[string]any{"type": "string", "enum": []string{"landed", "discarded", "abandoned", "unresolved", "promoted", "unknown"}}
	return []map[string]any{
		{"name": "whoami", "description": "Return deployment identity and counts.", "inputSchema": schema(map[string]any{})},
		{"name": "sessions_list", "description": "List the 20 most recent sessions as compact receipts (time, id, outcome, capture state, model, intent); use session_status to verify saved capture state.", "inputSchema": schema(map[string]any{})},
		{"name": "sessions_get", "description": "Read one saved session capture and its exchanges; this does not describe whether the work landed.", "inputSchema": schema(map[string]any{"id": stringSchema()})},
		{"name": "session_status", "description": "Wait briefly for capture to settle, then return a compact receipt from authoritative session storage with a dashboard link when Access is configured. Work outcome is tracked separately.", "inputSchema": schema(map[string]any{"id": stringSchema()})},
		{"name": "session_end", "description": "End an active session, optionally record its outcome, then return the authoritative capture receipt. Repeated calls are safe.", "inputSchema": optionalSchema(map[string]any{"id": stringSchema(), "outcome": canonical, "reason": stringSchema(), "evidence": map[string]any{}}, "id")},
		{"name": "session_set_outcome", "description": "Record the result of the work for a session; this does not verify that capture was saved.", "inputSchema": optionalSchema(map[string]any{"id": stringSchema(), "outcome": canonical, "reason": stringSchema(), "evidence": map[string]any{}}, "id", "outcome")},
		{"name": "search", "description": "Search session memory.", "inputSchema": schema(map[string]any{"query": stringSchema()})},
		{"name": "mark", "description": "Deprecated alias for setting a work outcome. Accepts legacy promoted and unknown values; does not verify saved capture.", "inputSchema": optionalSchema(map[string]any{"id": stringSchema(), "outcome": legacy}, "id", "outcome")},
		{"name": "config_get", "description": "Read deployment config.", "inputSchema": schema(map[string]any{})},
		{"name": "config_set", "description": "Set deployment config values.", "inputSchema": schema(map[string]any{"values": map[string]string{"type": "object"}})},
	}
}

func schema(props map[string]any) map[string]any {
	required := make([]string, 0, len(props))
	for key := range props {
		required = append(required, key)
	}
	sort.Strings(required)
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func optionalSchema(props map[string]any, required ...string) map[string]any {
	sort.Strings(required)
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func stringSchema() map[string]string { return map[string]string{"type": "string"} }

func (d Domain) CallTool(ctx context.Context, name string, args map[string]any) (ToolCallOutput, error) {
	method, path := "GET", ""
	var body any
	switch name {
	case "whoami":
		path = "/whoami"
	case "sessions_list":
		receipts, err := d.Sessions.FetchReceipts(ctx, "", "")
		if err != nil {
			return ToolCallOutput{}, err
		}
		return ToolCallOutput{Text: sessions.FormatReceipts(receipts, 20)}, nil
	case "sessions_get":
		id, err := requiredString(args, "id")
		if err != nil {
			return ToolCallOutput{}, err
		}
		path = "/sessions/" + url.PathEscape(id)
	case "session_status":
		id, err := requiredString(args, "id")
		if err != nil {
			return ToolCallOutput{}, err
		}
		status, err := d.Sessions.GetStatus(ctx, id)
		if err != nil {
			return ToolCallOutput{}, err
		}
		return ToolCallOutput{Text: sessions.ReceiptText(status)}, nil
	case "session_set_outcome":
		id, err := requiredString(args, "id")
		if err != nil {
			return ToolCallOutput{}, err
		}
		outcome, err := requiredString(args, "outcome")
		if err != nil {
			return ToolCallOutput{}, err
		}
		options := sessions.SetOutcomeOptions{Outcome: outcome}
		if value, ok := args["reason"].(string); ok && strings.TrimSpace(value) != "" {
			options.Reason = value
		}
		if value, ok := args["evidence"]; ok && value != nil {
			options.Evidence, options.EvidenceSet = value, true
		}
		data, err := d.Sessions.SetOutcome(ctx, id, options)
		if err != nil {
			return ToolCallOutput{}, err
		}
		return formatOutput(data), nil
	case "session_end":
		id, err := requiredString(args, "id")
		if err != nil {
			return ToolCallOutput{}, err
		}
		options := sessions.EndOptions{}
		if value, ok := args["outcome"].(string); ok && strings.TrimSpace(value) != "" {
			options.Outcome = value
		}
		if value, ok := args["reason"].(string); ok && strings.TrimSpace(value) != "" {
			options.Reason = value
		}
		if value, ok := args["evidence"]; ok && value != nil {
			options.Evidence, options.EvidenceSet = value, true
		}
		status, err := d.Sessions.End(ctx, id, options)
		if err != nil {
			return ToolCallOutput{}, err
		}
		return ToolCallOutput{Text: sessions.EndedReceiptText(status)}, nil
	case "search":
		query, err := requiredString(args, "query")
		if err != nil {
			return ToolCallOutput{}, err
		}
		data, err := d.Search(ctx, query)
		if err != nil {
			return ToolCallOutput{}, err
		}
		return ToolCallOutput{Text: string(data)}, nil
	case "mark":
		id, err := requiredString(args, "id")
		if err != nil {
			return ToolCallOutput{}, err
		}
		outcome, err := requiredString(args, "outcome")
		if err != nil {
			return ToolCallOutput{}, err
		}
		method, path, body = "POST", "/sessions/"+url.PathEscape(id)+"/mark", map[string]any{"outcome": outcome}
	case "config_get":
		path = "/config"
	case "config_set":
		method, path, body = "PUT", "/config", args["values"]
	default:
		return ToolCallOutput{}, fmt.Errorf("unknown tool: %s", name)
	}
	data, err := d.API.Request(ctx, method, path, body)
	if err != nil {
		return ToolCallOutput{}, err
	}
	return formatOutput(data), nil
}

func formatOutput(data []byte) ToolCallOutput {
	var output bytes.Buffer
	if json.Indent(&output, data, "", "  ") == nil {
		return ToolCallOutput{Text: output.String()}
	}
	return ToolCallOutput{Text: string(data)}
}

func requiredString(args map[string]any, key string) (string, error) {
	value, ok := args[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}
