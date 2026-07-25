package mcp

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

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type ToolCallOutput struct {
	Text string
}

type Server struct {
	Version string
	Tools   func() []map[string]any
	Call    func(context.Context, string, map[string]any) (ToolCallOutput, error)
}

func (s Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	for {
		data, contentLengthFraming, err := readMessage(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var request Request
		if err := json.Unmarshal(data, &request); err != nil {
			code, message := -32600, "invalid request"
			if !json.Valid(data) {
				code, message = -32700, "parse error"
			}
			if err := writeMessage(out, rpcError(nil, code, message), contentLengthFraming); err != nil {
				return err
			}
			continue
		}
		if !validRequest(data, request) {
			if err := writeMessage(out, rpcError(validResponseID(request.ID), -32600, "invalid request"), contentLengthFraming); err != nil {
				return err
			}
			continue
		}
		if request.ID == nil {
			continue
		}
		if err := writeMessage(out, s.Handle(ctx, request), contentLengthFraming); err != nil {
			return err
		}
	}
}

func validRequest(data []byte, request Request) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
		return false
	}
	if raw, ok := object["id"]; ok && validResponseID(raw) == nil && string(raw) != "null" {
		return false
	}
	if raw, ok := object["params"]; ok && string(raw) != "null" {
		var structured any
		if json.Unmarshal(raw, &structured) != nil {
			return false
		}
		switch structured.(type) {
		case map[string]any, []any:
		default:
			return false
		}
	}
	return true
}

func validResponseID(id json.RawMessage) any {
	if id == nil || string(id) == "null" {
		return nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(id))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return nil
	}
	switch value.(type) {
	case string, json.Number:
		return id
	default:
		return nil
	}
}

func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}

func (s Server) Handle(ctx context.Context, request Request) map[string]any {
	ok := func(value any) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": value}
	}
	fail := func(code int, message string) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": code, "message": message}}
	}
	switch request.Method {
	case "initialize":
		return ok(map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "mimir", "version": s.Version}, "capabilities": map[string]any{"tools": map[string]any{}}})
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		return ok(map[string]any{"tools": s.Tools()})
	case "tools/call":
		var raw struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if len(request.Params) == 0 || string(request.Params) == "null" || json.Unmarshal(request.Params, &raw) != nil || strings.TrimSpace(raw.Name) == "" {
			return fail(-32602, "invalid params")
		}
		arguments := map[string]any{}
		if len(raw.Arguments) > 0 && string(raw.Arguments) != "null" {
			if err := json.Unmarshal(raw.Arguments, &arguments); err != nil || arguments == nil {
				return fail(-32602, "invalid params")
			}
		}
		output, err := s.Call(ctx, raw.Name, arguments)
		if err != nil {
			return ok(map[string]any{"content": []map[string]string{{"type": "text", "text": err.Error()}}, "isError": true})
		}
		return ok(map[string]any{"content": []map[string]string{{"type": "text", "text": output.Text}}, "isError": false})
	default:
		return map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32601, "message": "method not found"}}
	}
}

func ReadMessage(reader *bufio.Reader) ([]byte, error) {
	data, _, err := readMessage(reader)
	return data, err
}

func readMessage(reader *bufio.Reader) ([]byte, bool, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, false, err
	}
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(strings.ToLower(trimmed), "content-length:") {
		return []byte(trimmed), false, nil
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
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, true, err
		}
	}
	if length <= 0 {
		return nil, true, fmt.Errorf("missing Content-Length")
	}
	data := make([]byte, length)
	_, err = io.ReadFull(reader, data)
	return data, true, err
}

func WriteMessage(writer io.Writer, value any) error {
	return writeMessage(writer, value, false)
}

func writeMessage(writer io.Writer, value any, contentLengthFraming bool) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if contentLengthFraming {
		_, err = fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(data), data)
	} else {
		_, err = fmt.Fprintf(writer, "%s\n", data)
	}
	return err
}
