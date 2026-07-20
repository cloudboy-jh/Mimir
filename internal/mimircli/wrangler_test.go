package mimircli

import (
	"encoding/json"
	"testing"
)

func TestStripJSONC(t *testing.T) {
	input := []byte(`{
  // line comment
  "name": "mimir", // trailing line comment
  "url": "https://example.com/a//b", /* block comment */
  "escaped": "quote \" not a comment //",
  "list": [1, 2,],
  "nested": {"a": true,},
}`)
	var parsed map[string]any
	if err := json.Unmarshal(stripJSONC(input), &parsed); err != nil {
		t.Fatalf("stripped JSONC did not parse: %v", err)
	}
	if parsed["name"] != "mimir" || parsed["url"] != "https://example.com/a//b" || parsed["escaped"] != "quote \" not a comment //" {
		t.Fatalf("values %#v", parsed)
	}
	list, ok := parsed["list"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("list %#v", parsed["list"])
	}
	nested, ok := parsed["nested"].(map[string]any)
	if !ok || nested["a"] != true {
		t.Fatalf("nested %#v", parsed["nested"])
	}
}
