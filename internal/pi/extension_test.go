package pi

import (
	"os"
	"strings"
	"testing"
)

func TestWriteMimirExtension(t *testing.T) {
	path, err := WriteMimirExtension(t.TempDir(), `C:\Program Files\mimir.exe`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"list_sessions", "get_session", "search_memory", "set_outcome", "doctor_check", `C:\\Program Files\\mimir.exe`} {
		if !strings.Contains(text, want) {
			t.Fatalf("extension missing %q", want)
		}
	}
}
