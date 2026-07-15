package mimircli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func remoteRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	p, err := loadPointer()
	if err != nil {
		return nil, err
	}
	return remoteRequestWithPointer(ctx, p, method, path, body)
}

func remoteRequestWithPointer(ctx context.Context, p Pointer, method, path string, body any) ([]byte, error) {
	if err := validateDeploymentURL(p.URL); err != nil {
		return nil, err
	}
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.URL+path, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+p.Token)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("Mimir API %s: %s", res.Status, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func validateDeploymentURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid Mimir deployment URL")
	}
	host := parsed.Hostname()
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")) {
		return fmt.Errorf("Mimir deployment URL must use HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid Mimir deployment URL")
	}
	return nil
}

func federatedSearch(ctx context.Context, query string) ([]byte, error) {
	remote, err := remoteRequest(ctx, "POST", "/search", map[string]any{"query": query})
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(remote, &result); err != nil {
		return nil, err
	}
	if code, err := queryRecall(ctx, ".", query, 4000); err == nil {
		result["code"] = code
	}
	return json.MarshalIndent(result, "", "  ")
}

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

type remoteSession struct {
	Session struct {
		ID        string `json:"id"`
		StartedAt string `json:"started_at"`
		SourceRef string `json:"source_ref"`
	} `json:"session"`
	Files []string `json:"files"`
}

// markGitOutcome only applies evidence visible in the current checkout. The
// Worker remains the source of truth; this adapter sends its conclusion there.
func markGitOutcome(ctx context.Context, id string) ([]byte, error) {
	data, err := remoteRequest(ctx, "GET", "/sessions/"+id, nil)
	if err != nil {
		return nil, err
	}
	var remote remoteSession
	if err := json.Unmarshal(data, &remote); err != nil {
		return nil, err
	}
	if remote.Session.ID == "" {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	started, err := time.Parse(time.RFC3339, remote.Session.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid session start time: %w", err)
	}
	commits, err := runGit(ctx, ".", "log", "--all", "--format=%H", "--since="+started.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	outcome := "unknown"
	for _, commit := range strings.Fields(commits) {
		changed, err := runGit(ctx, ".", "show", "--format=", "--name-only", commit)
		if err != nil || !overlaps(remote.Files, strings.Fields(changed)) {
			continue
		}
		branches, err := runGit(ctx, ".", "branch", "-r", "--contains", commit)
		if err == nil && durableBranch(strings.Fields(branches)) {
			outcome = "promoted"
			break
		}
	}
	if outcome == "unknown" && remote.Session.SourceRef != "" {
		if _, err := runGit(ctx, ".", "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+remote.Session.SourceRef); err != nil {
			outcome = "discarded"
		}
	}
	if outcome == "unknown" && time.Since(started) >= 7*24*time.Hour {
		outcome = "abandoned"
	}
	return remoteRequest(ctx, "POST", "/sessions/"+id+"/outcome", map[string]string{"outcome": outcome, "source": "git"})
}

func overlaps(expected, changed []string) bool {
	if len(expected) == 0 {
		return false
	}
	for _, left := range expected {
		for _, right := range changed {
			if left == right || strings.HasSuffix(left, "/"+right) || strings.HasSuffix(right, "/"+left) {
				return true
			}
		}
	}
	return false
}

func durableBranch(branches []string) bool {
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if strings.HasSuffix(branch, "/main") || strings.HasSuffix(branch, "/master") || strings.HasSuffix(branch, "/HEAD") {
			return true
		}
	}
	return false
}
