package mimircli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecuteVersion(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	t.Cleanup(func() { SetBuildInfo(oldVersion, oldCommit, oldDate) })
	SetBuildInfo("1.2.3", "abc123", "2026-07-15")

	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"version"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "1.2.3 (abc123)\n"; got != want {
		t.Fatalf("version output %q, want %q", got, want)
	}
}

func TestExecuteUsage(t *testing.T) {
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), nil, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "mimir setup [--quick] [--json]") {
		t.Fatalf("usage output %q", output.String())
	}
}

func TestParseRecallArgs(t *testing.T) {
	query, budget, jsonOut, err := parseRecallArgs([]string{"session", "storage", "--budget", "1200", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(query, " "); got != "session storage" || budget != 1200 || !jsonOut {
		t.Fatalf("query=%q budget=%d json=%v", got, budget, jsonOut)
	}
}
