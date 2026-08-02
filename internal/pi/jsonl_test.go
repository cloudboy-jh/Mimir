package pi

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadJSONLArbitrarySizeAndCRLF(t *testing.T) {
	large := strings.Repeat("x", 128<<10)
	input := `{"type":"message_update","value":"` + large + `"}` + "\r\n" +
		`{"type":"agent_end"}`
	var records []string
	err := readJSONL(strings.NewReader(input), func(record []byte) error {
		records = append(records, string(record))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if !strings.Contains(records[0], large) {
		t.Fatal("large record was truncated")
	}
	if records[1] != `{"type":"agent_end"}` {
		t.Fatalf("unexpected final record %q", records[1])
	}
}

func TestReadJSONLRejectsOversizedRecord(t *testing.T) {
	data := bytes.Repeat([]byte{'x'}, maxJSONLRecord+1)
	err := readJSONL(bytes.NewReader(data), func([]byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}
