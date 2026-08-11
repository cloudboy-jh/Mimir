package mimircli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoPrintsURLAndOpensBrowser(t *testing.T) {
	old := openBrowser
	t.Cleanup(func() { openBrowser = old })
	opened := ""
	openBrowser = func(_ context.Context, target string) error {
		opened = target
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	if err := demo(ctx, nil, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if opened == "" || !strings.HasPrefix(opened, "http://127.0.0.1:") {
		t.Fatalf("opened URL = %q", opened)
	}
	for _, want := range []string{"Mimir demo: " + opened, "Sample data only.", "Press Ctrl+C to stop."} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestDemoNoOpenAndBrowserFailure(t *testing.T) {
	old := openBrowser
	t.Cleanup(func() { openBrowser = old })
	calls := 0
	openBrowser = func(context.Context, string) error {
		calls++
		return errors.New("no browser")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	if err := demo(ctx, []string{"--no-open"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || strings.Contains(output.String(), "Browser did not open") {
		t.Fatalf("calls=%d output=%q", calls, output.String())
	}

	output.Reset()
	if err := demo(ctx, nil, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(output.String(), "Browser did not open automatically. Use the URL above.") {
		t.Fatalf("calls=%d output=%q", calls, output.String())
	}
}

func TestDemoRejectsArguments(t *testing.T) {
	if err := demo(context.Background(), []string{"--listen", "0.0.0.0"}, IO{Out: &bytes.Buffer{}}); err == nil || err.Error() != "usage: mimir demo [--no-open]" {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteDemoDoesNotReadInstallationState(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if err := os.MkdirAll(paths.MimirHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.MimirHome, "pending-update.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output, stderr bytes.Buffer
	if err := ExecuteIO(ctx, []string{"demo", "--no-open"}, IO{Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 || !strings.Contains(output.String(), "Mimir demo:") {
		t.Fatalf("stdout=%q stderr=%q", output.String(), stderr.String())
	}
}
