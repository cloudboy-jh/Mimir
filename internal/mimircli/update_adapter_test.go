package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/harness"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

func TestCmdUpdateForwardsLifecycleCheckAndPreservesJSONContract(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	called := false
	runLifecycleUpdate = func(_ context.Context, check bool) (lifecyclepkg.UpdateReport, error) {
		called = true
		if !check {
			t.Fatal("--check was not forwarded to the lifecycle service")
		}
		return lifecyclepkg.UpdateReport{
			Check:        true,
			Binary:       installpkg.UpdateBinaryReport{Status: "available", Current: "1.0.0", Latest: "2.0.0"},
			Artifacts:    installpkg.ArtifactReport{Operation: "check"},
			Integrations: harness.IntegrationReport{OpenCode: harness.IntegrationState{State: "current"}},
		}, nil
	}
	var output bytes.Buffer
	if err := cmdUpdate(context.Background(), []string{"--check", "--json"}, &output); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("lifecycle update was not called")
	}
	var report lifecyclepkg.UpdateReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON output %q: %v", output.String(), err)
	}
	if !report.Check || report.Binary.Status != "available" || report.Binary.Current != "1.0.0" || report.Binary.Latest != "2.0.0" || report.Artifacts.Operation != "check" || report.Integrations.OpenCode.State != "current" {
		t.Fatalf("report = %#v", report)
	}
}

func TestCmdUpdateRejectsInvalidArgumentsBeforeLifecycle(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	runLifecycleUpdate = func(context.Context, bool) (lifecyclepkg.UpdateReport, error) {
		t.Fatal("lifecycle update called for invalid arguments")
		return lifecyclepkg.UpdateReport{}, nil
	}
	if err := cmdUpdate(context.Background(), []string{"--unknown"}, &bytes.Buffer{}); err == nil {
		t.Fatal("invalid update argument was accepted")
	}
}
