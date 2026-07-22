package mimircli

import (
	"context"
	"strings"
)

type harnessIntegrationState struct {
	State           string `json:"state"`
	Provider        string `json:"provider,omitempty"`
	Scope           string `json:"scope,omitempty"`
	RestartRequired bool   `json:"restart_required,omitempty"`
	Detail          string `json:"detail,omitempty"`
}

type harnessIntegrationReport struct {
	OpenCode harnessIntegrationState `json:"opencode"`
	Hermes   harnessIntegrationState `json:"hermes"`
}

func installCurrentHarnessIntegrations(ctx context.Context) (harnessIntegrationReport, error) {
	report := harnessIntegrationReport{OpenCode: harnessIntegrationState{State: "skipped", Detail: "automatic OpenCode configuration is disabled"}}
	if installed, err := installCurrentHermesIntegration(ctx); err != nil {
		report.Hermes = harnessIntegrationState{State: "failed", Provider: "openrouter", Scope: "openrouter", Detail: err.Error()}
		return report, err
	} else if installed {
		report.Hermes = harnessIntegrationState{State: "installed", Provider: "openrouter", Scope: "openrouter", RestartRequired: true, Detail: "direct providers are not captured"}
	} else {
		report.Hermes = harnessIntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	}
	return report, nil
}

func integrationSummary(report harnessIntegrationReport) string {
	var lines []string
	if report.Hermes.State == "installed" {
		lines = append(lines, "Hermes OpenRouter capture installed · restart Hermes", "Hermes scope: built-in OpenRouter models only · direct providers are not captured")
	}
	return strings.Join(lines, "\n")
}
