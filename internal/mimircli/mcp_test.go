package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestServeMCPUsesNewlineDelimitedJSON(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := serveMCP(context.Background(), mcpOptions{In: strings.NewReader(input), Out: &output}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d responses: %q", len(lines), output.String())
	}
	for _, line := range lines {
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("response is not newline-delimited JSON: %q: %v", line, err)
		}
	}
	if strings.Contains(output.String(), "Content-Length") {
		t.Fatalf("response used header framing: %q", output.String())
	}
}

func TestServeMCPReturnsParseError(t *testing.T) {
	var output bytes.Buffer
	if err := serveMCP(context.Background(), mcpOptions{In: strings.NewReader("not-json\n"), Out: &output}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != -32700 {
		t.Fatalf("error code %d", response.Error.Code)
	}
}
