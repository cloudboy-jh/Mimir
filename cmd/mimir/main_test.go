package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	"github.com/cloudboy-jh/mimir/internal/mimircli"
)

func TestRunEmitsStateErrorAsStructuredJSON(t *testing.T) {
	original := execute
	execute = func(context.Context, []string) error {
		return deployment.StateError{State: "deployment_url_missing", Message: "run mimir deploy, then rerun mimir login"}
	}
	t.Cleanup(func() { execute = original })

	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"login", "--json"}, &stderr); code != mimircli.ExitRemoteFailure {
		t.Fatalf("exit code = %d", code)
	}
	var result map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["state"] != "deployment_url_missing" || result["message"] == nil || result["exit_code"] != float64(mimircli.ExitRemoteFailure) {
		t.Fatalf("result = %#v", result)
	}
	if _, exists := result["error"]; exists {
		t.Fatalf("state error was double-encoded: %s", stderr.String())
	}
}

func TestRunKeepsGenericJSONErrorEnvelope(t *testing.T) {
	original := execute
	execute = func(context.Context, []string) error { return errors.New("remote failed") }
	t.Cleanup(func() { execute = original })

	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"whoami", "--json"}, &stderr); code != mimircli.ExitRemoteFailure {
		t.Fatalf("exit code = %d", code)
	}
	if got := stderr.String(); got != "{\"error\":\"remote failed\",\"exit_code\":4}\n" {
		t.Fatalf("stderr = %q", got)
	}
}
