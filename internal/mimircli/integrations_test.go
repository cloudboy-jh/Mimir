package mimircli

import (
	"strings"
	"testing"
)

func TestIntegrationSummaryExplainsHermesScope(t *testing.T) {
	report := harnessIntegrationReport{
		Hermes: harnessIntegrationState{State: "installed", Provider: "openrouter", Scope: "openrouter", RestartRequired: true, Detail: "direct providers are not captured"},
	}
	summary := integrationSummary(report)
	for _, want := range []string{"Hermes OpenRouter capture installed", "restart Hermes", "built-in OpenRouter models only", "direct providers are not captured"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestIntegrationSummarySkipsAbsentHermes(t *testing.T) {
	if summary := integrationSummary(harnessIntegrationReport{Hermes: harnessIntegrationState{State: "skipped"}}); summary != "" {
		t.Fatalf("unexpected summary %q", summary)
	}
}
