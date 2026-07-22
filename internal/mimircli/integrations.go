package mimircli

import (
	"context"
	"errors"
	"fmt"
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
	report := harnessIntegrationReport{}
	var errs []error
	if err := installCurrentOpenCodeIntegration(); err != nil {
		report.OpenCode = harnessIntegrationState{State: "failed", Detail: err.Error()}
		errs = append(errs, fmt.Errorf("opencode: %w", err))
	} else {
		report.OpenCode = harnessIntegrationState{State: "installed", Provider: "openrouter", Scope: "openrouter", RestartRequired: true}
	}
	if installed, err := installCurrentHermesIntegration(ctx); err != nil {
		report.Hermes = harnessIntegrationState{State: "failed", Provider: "openrouter", Scope: "openrouter", Detail: err.Error()}
		errs = append(errs, fmt.Errorf("Hermes: %w", err))
	} else if installed {
		report.Hermes = harnessIntegrationState{State: "installed", Provider: "openrouter", Scope: "openrouter", RestartRequired: true, Detail: "direct providers are not captured"}
	} else {
		report.Hermes = harnessIntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	}
	return report, errors.Join(errs...)
}

func integrationSummary(report harnessIntegrationReport) string {
	var lines []string
	if report.OpenCode.State == "installed" {
		lines = append(lines, "opencode OpenRouter capture installed · restart opencode")
	}
	if report.Hermes.State == "installed" {
		lines = append(lines, "Hermes OpenRouter capture installed · restart Hermes", "Hermes scope: built-in OpenRouter models only · direct providers are not captured")
	}
	return strings.Join(lines, "\n")
}
