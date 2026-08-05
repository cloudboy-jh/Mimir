package mimircli

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeGitRemote(t *testing.T) {
	for input, want := range map[string]string{
		"git@github.com:owner/repo.git":                 "https://github.com/owner/repo",
		"ssh://git@gitlab.example.com/group/repo.git":   "https://gitlab.example.com/group/repo",
		"https://user:secret@github.com/owner/repo.git": "https://github.com/owner/repo",
		"C:\\repos\\mimir":                              "",
	} {
		if got := normalizeGitRemote(input); got != want {
			t.Errorf("normalizeGitRemote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRedactAndBoundGitEvidence(t *testing.T) {
	redacted := redactGitEvidence("+token=supersecretvalue\n+Bearer abc.def.ghi\n+sk_live_1234567890abcdef")
	if strings.Contains(redacted, "supersecretvalue") || strings.Contains(redacted, "abc.def.ghi") || strings.Contains(redacted, "sk_live") {
		t.Fatalf("secrets remained in %q", redacted)
	}
	bounded := boundedGitEvidence(strings.Repeat("é", 20_000), 20*1024)
	if len(bounded) > 20*1024 || !utf8.ValidString(bounded) {
		t.Fatalf("bounded evidence bytes=%d valid=%v", len(bounded), utf8.ValidString(bounded))
	}
}
