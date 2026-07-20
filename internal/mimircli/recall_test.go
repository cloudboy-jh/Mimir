package mimircli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScore(t *testing.T) {
	if got := score("", "anything"); got != 1 {
		t.Fatalf("empty query score %d", got)
	}
	if got := score("main", "main.go"); got != 100+len("main") {
		t.Fatalf("substring score %d", got)
	}
	if got := score("main", "func MAIN()"); got != 100+len("main") {
		t.Fatalf("case-insensitive parts score %d", got)
	}
	if got := score("auth login", "src/auth.ts"); got != 20+len("auth") {
		t.Fatalf("token score %d", got)
	}
	if got := score("zzz", "main.go"); got != 0 {
		t.Fatalf("miss score %d", got)
	}
}

func TestRankOrdersAndLabels(t *testing.T) {
	idx := mimirIndex{
		Files: map[string]fileInfo{
			"auth/login.go": {Symbols: []string{"Login"}, Dependencies: []string{"fmt"}},
			"zz/auth.go":    {},
		},
		Symbols: map[string]symbol{
			"Login": {Type: "func", File: "auth/login.go", Line: 5, Signature: "func Login()"},
		},
	}
	matches := rank(idx, "login")
	if len(matches) != 2 {
		t.Fatalf("matches %+v", matches)
	}
	// Symbol and its declaring file both substring-match and tie on score;
	// the file surface includes its symbol names by design.
	kinds := map[string]int{}
	for _, m := range matches {
		kinds[m.Kind]++
		if m.Score != 100+len("login") {
			t.Fatalf("match score %d: %+v", m.Score, m)
		}
	}
	if kinds["symbol"] != 1 || kinds["file"] != 1 {
		t.Fatalf("kinds %+v", matches)
	}
	if matches[0].File != "auth/login.go" || matches[1].File != "auth/login.go" {
		t.Fatalf("tie-break %+v", matches)
	}
	if got := rank(idx, "no-such-thing"); len(got) != 0 {
		t.Fatalf("miss %+v", got)
	}
}

func TestRankPrefersSubstringOverToken(t *testing.T) {
	idx := mimirIndex{
		Files:   map[string]fileInfo{"a.go": {Symbols: []string{"LoginHandler"}}, "b.go": {Symbols: []string{"Login"}}},
		Symbols: map[string]symbol{
			"LoginHandler": {Type: "func", File: "a.go", Line: 1},
			"Login":        {Type: "func", File: "b.go", Line: 1},
		},
	}
	matches := rank(idx, "login handler")
	if len(matches) == 0 || matches[0].Name != "LoginHandler" {
		t.Fatalf("ranking %+v", matches)
	}
}

func TestRankBreaksTiesByFile(t *testing.T) {
	idx := mimirIndex{Files: map[string]fileInfo{"b/auth.go": {}, "a/auth.go": {}}}
	matches := rank(idx, "auth")
	if len(matches) != 2 || matches[0].File != "a/auth.go" || matches[1].File != "b/auth.go" {
		t.Fatalf("tie-break %+v", matches)
	}
}

func TestFitClampsToBudget(t *testing.T) {
	big := Match{Kind: "symbol", Name: strings.Repeat("n", 300), File: "f.go", Score: 100}
	matches := []Match{big, big, big}
	if got := fit(matches, 1); len(got) != 1 {
		t.Fatalf("tiny budget should keep exactly one match, got %d", len(got))
	}
	if got := fit(matches, 100000); len(got) != 3 {
		t.Fatalf("large budget should keep all, got %d", len(got))
	}
	if got := fit(nil, 10); len(got) != 0 {
		t.Fatalf("empty %d", len(got))
	}
}

func TestRecallEndToEnd(t *testing.T) {
	root := t.TempDir()
	if out, err := runGit(context.Background(), root, "init"); err != nil {
		t.Skipf("git unavailable: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc resolveSession() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runIndex(context.Background(), indexOptions{Dir: root, Full: true}); err != nil {
		t.Fatal(err)
	}
	res, err := queryRecall(context.Background(), root, "resolveSession", 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) == 0 || res.Matches[0].Name != "resolveSession" {
		t.Fatalf("recall %+v", res)
	}
	sym, ok, err := locateSymbol(context.Background(), root, "resolveSession")
	if err != nil || !ok {
		t.Fatalf("locate %v %v", ok, err)
	}
	if !filepath.IsAbs(sym.File) || sym.Line != 3 {
		t.Fatalf("symbol %+v", sym)
	}
	if _, _, err := locateSymbol(context.Background(), root, "missingSymbol"); err != nil {
		t.Fatal(err)
	}
}
