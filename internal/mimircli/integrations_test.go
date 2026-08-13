package mimircli

import (
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/harness"
)

func TestIntegrationSummaryUsesRestartRequiredForBothHarnesses(t *testing.T) {
	report := harness.IntegrationReport{
		Pi:       harness.IntegrationState{State: "current", RestartRequired: true},
		OpenCode: harness.IntegrationState{State: "current", RestartRequired: true},
		Hermes:   harness.IntegrationState{State: "installed", Provider: "openrouter", Scope: "all-providers", RestartRequired: true},
	}
	summary := integrationSummary(report)
	for _, want := range []string{"Pi: restart required", "OpenCode: restart required", "Hermes: restart required", "OpenRouter proxy plus direct providers"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestIntegrationSummaryDoesNotInferRestartFromState(t *testing.T) {
	summary := integrationSummary(harness.IntegrationReport{Hermes: harness.IntegrationState{State: "installed"}})
	if strings.Contains(summary, "restart") {
		t.Fatalf("restart inferred without RestartRequired: %q", summary)
	}
}

func TestIntegrationSummarySkipsAbsentHermes(t *testing.T) {
	if summary := integrationSummary(harness.IntegrationReport{Hermes: harness.IntegrationState{State: "skipped"}}); summary != "" {
		t.Fatalf("unexpected summary %q", summary)
	}
}
