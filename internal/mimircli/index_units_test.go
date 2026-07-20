package mimircli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveIndexAtomicRoundTrips(t *testing.T) {
	root := t.TempDir()
	idx := mimirIndex{
		Repo:          "repo",
		IndexedCommit: "abc123",
		Files:         map[string]fileInfo{"main.go": {Hash: "h", Symbols: []string{"main"}, Dependencies: []string{"fmt"}}},
		Symbols:       map[string]symbol{"main": {Type: "func", File: "main.go", Line: 3, Signature: "func main()"}},
	}
	if err := saveIndexAtomic(root, idx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(indexPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatal("index missing trailing newline")
	}
	loaded, err := loadIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.IndexedCommit != "abc123" || len(loaded.Files) != 1 || loaded.Symbols["main"].Line != 3 {
		t.Fatalf("loaded %+v", loaded)
	}
	idx.IndexedCommit = "def456"
	if err := saveIndexAtomic(root, idx); err != nil {
		t.Fatal(err)
	}
	loaded, err = loadIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.IndexedCommit != "def456" {
		t.Fatalf("overwrite failed: %s", loaded.IndexedCommit)
	}
	matches, err := filepath.Glob(filepath.Join(root, mimirDirName, ".index-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestParseFileExtractsSymbolsAndDeps(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("lib/util.ts", "export function helper() {}\n")
	write("main.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println()\n}\n\ntype Config struct{}\n")
	write("app.ts", "import { helper } from './lib/util';\nexport const value = 1;\nexport interface Shape { x: number }\n")

	parsed, err := parseFile(root, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(parsed.info.Symbols, ","); got != "Config,main" {
		t.Fatalf("go symbols %s", got)
	}
	if parsed.symbols["main"].Type != "func" || parsed.symbols["Config"].Type != "type" {
		t.Fatalf("go types %+v", parsed.symbols)
	}
	if len(parsed.info.Dependencies) != 1 || parsed.info.Dependencies[0] != "fmt" {
		t.Fatalf("go deps %v", parsed.info.Dependencies)
	}

	ts, err := parseFile(root, "app.ts")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ts.info.Symbols, ","); got != "Shape,value" {
		t.Fatalf("ts symbols %s", got)
	}
	if len(ts.info.Dependencies) != 1 || ts.info.Dependencies[0] != "lib/util.ts" {
		t.Fatalf("ts deps %v", ts.info.Dependencies)
	}
}

func TestSymbolType(t *testing.T) {
	cases := map[string]string{
		"func main() {":           "func",
		"type Config struct{}":    "type",
		"export interface Shape":  "interface",
		"class Thing extends Base": "class",
		"def run(x):":             "def",
		"pub fn build() {}":       "fn",
		"something else entirely": "symbol",
	}
	for line, want := range cases {
		if got := symbolType(line); got != want {
			t.Fatalf("symbolType(%q) = %q, want %q", line, got, want)
		}
	}
}
