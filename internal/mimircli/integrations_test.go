package mimircli

import (
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/harness"
)

func TestIntegrationSummaryExplainsHermesScope(t *testing.T) {
	report := harness.IntegrationReport{
		Hermes: harness.IntegrationState{State: "installed", Provider: "openrouter", Scope: "all-providers", RestartRequired: true},
	}
	summary := integrationSummary(report)
	for _, want := range []string{"Hermes capture installed", "restart Hermes", "OpenRouter proxy plus direct providers"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestIntegrationSummarySkipsAbsentHermes(t *testing.T) {
	if summary := integrationSummary(harness.IntegrationReport{Hermes: harness.IntegrationState{State: "skipped"}}); summary != "" {
		t.Fatalf("unexpected summary %q", summary)
	}
}
