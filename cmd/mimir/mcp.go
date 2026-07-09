package main

import (
	"bufio"
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
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func serveMCP(ctx context.Context, opts mcpOptions) error {
	if opts.In == nil {
		return fmt.Errorf("missing MCP input")
	}
	if opts.Out == nil {
		return fmt.Errorf("missing MCP output")
	}
	r := bufio.NewReader(opts.In)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, err := readMessage(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}
		if req.ID == nil && strings.HasPrefix(req.Method, "notifications/") {
			continue
		}
		res := handle(ctx, opts.Dir, req)
		if err := writeMessage(opts.Out, res); err != nil {
			return err
		}
	}
}

func handle(ctx context.Context, dir string, req request) map[string]any {
	success := func(result any) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}
	}
	fail := func(code int, msg string) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": msg}}
	}
	switch req.Method {
	case "initialize":
		return success(map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "mimir", "version": versionString()}, "capabilities": map[string]any{"tools": map[string]any{}}})
	case "tools/list":
		return success(map[string]any{"tools": tools()})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(-32602, err.Error())
		}
		out, err := callTool(ctx, dir, p.Name, p.Arguments)
		if err != nil {
			return fail(-32000, err.Error())
		}
		return success(map[string]any{"content": []map[string]any{{"type": "text", "text": out}}})
	default:
		return fail(-32601, "method not found")
	}
}

func tools() []map[string]any {
	return []map[string]any{
		{"name": "mimir_status", "description": "Return current indexing metrics and freshness.", "inputSchema": schema(map[string]any{})},
		{"name": "mimir_recall", "description": "Return ranked code memory for a query inside a token budget.", "inputSchema": schema(map[string]any{"query": str(), "token_budget": num()})},
		{"name": "mimir_get_file_deps", "description": "Return immediate imports and downstream dependency linkages for a file.", "inputSchema": schema(map[string]any{"file_path": str()})},
		{"name": "mimir_locate_symbol", "description": "Return absolute file path, line, type, and signature for a symbol.", "inputSchema": schema(map[string]any{"symbol_name": str()})},
	}
}

func schema(props map[string]any) map[string]any {
	req := []string{}
	for k := range props {
		req = append(req, k)
	}
	return map[string]any{"type": "object", "properties": props, "required": req}
}
func str() map[string]string { return map[string]string{"type": "string"} }
func num() map[string]string { return map[string]string{"type": "number"} }

func callTool(ctx context.Context, dir, name string, args map[string]any) (string, error) {
	switch name {
	case "mimir_status":
		info, err := detectRepo(ctx, dir)
		if err != nil {
			return "", err
		}
		idx, _ := loadIndex(info.Root)
		data, _ := json.MarshalIndent(map[string]any{"root": info.Root, "head": info.HeadSHA, "indexed_commit": info.IndexedSHA, "stale": info.Stale, "files": len(idx.Files), "symbols": len(idx.Symbols), "updated": idx.Timestamp}, "", "  ")
		return string(data), nil
	case "mimir_recall":
		query, _ := args["query"].(string)
		budget := 4000
		if v, ok := args["token_budget"].(float64); ok {
			budget = int(v)
		}
		res, err := queryRecall(ctx, dir, query, budget)
		if err != nil {
			return "", err
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return string(data), nil
	case "mimir_get_file_deps":
		file, _ := args["file_path"].(string)
		fi, downstream, err := fileDeps(ctx, dir, file)
		if err != nil {
			return "", err
		}
		data, _ := json.MarshalIndent(map[string]any{"file": file, "dependencies": fi.Dependencies, "downstream": downstream}, "", "  ")
		return string(data), nil
	case "mimir_locate_symbol":
		name, _ := args["symbol_name"].(string)
		sym, ok, err := locateSymbol(ctx, dir, name)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("symbol not found: %s", name)
		}
		data, _ := json.MarshalIndent(sym, "", "  ")
		return string(data), nil
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func readMessage(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "{") {
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
	buf := make([]byte, length)
	_, err = io.ReadFull(r, buf)
	return buf, err
}

func writeMessage(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}
