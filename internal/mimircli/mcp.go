package mimircli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type mcpOptions struct {
	Dir string
	In  io.Reader
	Out io.Writer
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func serveMCP(ctx context.Context, opts mcpOptions) error {
	r := bufio.NewReader(opts.In)
	for {
		data, err := readMessage(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var req request
		if err := json.Unmarshal(data, &req); err != nil {
			if err := writeMessage(opts.Out, map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "parse error"}}); err != nil {
				return err
			}
			continue
		}
		if req.ID == nil {
			continue
		}
		response := handle(ctx, req)
		if err := writeMessage(opts.Out, response); err != nil {
			return err
		}
	}
}

func handle(ctx context.Context, req request) map[string]any {
	ok := func(value any) map[string]any { return map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": value} }
	fail := func(err error) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": err.Error()}}
	}
	switch req.Method {
	case "initialize":
		return ok(map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "mimir", "version": versionString()}, "capabilities": map[string]any{"tools": map[string]any{}}})
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		return ok(map[string]any{"tools": tools()})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(err)
		}
		out, err := callTool(ctx, p.Name, p.Arguments)
		if err != nil {
			return fail(err)
		}
		return ok(map[string]any{"content": []map[string]string{{"type": "text", "text": out}}})
	default:
		return map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32601, "message": "method not found"}}
	}
}

func tools() []map[string]any {
	return []map[string]any{
		{"name": "whoami", "description": "Return deployment identity and counts.", "inputSchema": schema(map[string]any{})},
		{"name": "sessions_list", "description": "List remembered sessions.", "inputSchema": schema(map[string]any{})},
		{"name": "sessions_get", "description": "Get one session and its exchanges.", "inputSchema": schema(map[string]any{"id": str()})},
		{"name": "search", "description": "Search session memory.", "inputSchema": schema(map[string]any{"query": str()})},
		{"name": "mark", "description": "Set a session outcome.", "inputSchema": schema(map[string]any{"id": str(), "outcome": str()})},
		{"name": "config_get", "description": "Read deployment config.", "inputSchema": schema(map[string]any{})},
		{"name": "config_set", "description": "Set deployment config values.", "inputSchema": schema(map[string]any{"values": map[string]string{"type": "object"}})},
	}
}

func schema(props map[string]any) map[string]any {
	required := []string{}
	for key := range props {
		required = append(required, key)
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str() map[string]string { return map[string]string{"type": "string"} }

func callTool(ctx context.Context, name string, args map[string]any) (string, error) {
	method, path := "GET", ""
	var body any
	switch name {
	case "whoami":
		path = "/whoami"
	case "sessions_list":
		path = "/sessions"
	case "sessions_get":
		path = "/sessions/" + fmt.Sprint(args["id"])
	case "search":
		data, err := federatedSearch(ctx, fmt.Sprint(args["query"]))
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "mark":
		method, path, body = "POST", "/sessions/"+fmt.Sprint(args["id"])+"/mark", map[string]any{"outcome": args["outcome"]}
	case "config_get":
		path = "/config"
	case "config_set":
		method, path, body = "PUT", "/config", args["values"]
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	data, err := remoteRequest(ctx, method, path, body)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	if json.Indent(&output, data, "", "  ") == nil {
		return output.String(), nil
	}
	return string(data), nil
}

func readMessage(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(strings.ToLower(trimmed), "content-length:") {
		return []byte(trimmed), nil
	}
	length := 0
	for {
		if strings.TrimSpace(line) == "" {
			break
		}
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Content-Length") {
			length, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		}
		line, err = r.ReadString('\n')
		if err != nil {
			return nil, err
		}
	}
	if length <= 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	data := make([]byte, length)
	_, err = io.ReadFull(r, data)
	return data, err
}

func writeMessage(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}
