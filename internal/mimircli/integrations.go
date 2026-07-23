package mimircli

import (
	"context"
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

type lifecycleIntegrationReport struct {
	OK           bool                     `json:"ok"`
	Artifacts    managedArtifactReport    `json:"artifacts"`
	Integrations harnessIntegrationReport `json:"integrations"`
	Error        string                   `json:"error,omitempty"`
}

func refreshLifecycleIntegrations(ctx context.Context, operation string) lifecycleIntegrationReport {
	return refreshLifecycleIntegrationsWith(ctx, operation, func(_ bool, operation string) (managedArtifactReport, error) {
		return refreshManagedInstallation(true, operation)
	})
}

func refreshConnectedLifecycleIntegrations(ctx context.Context, operation string) lifecycleIntegrationReport {
	report := lifecycleIntegrationReport{OK: true}
	artifacts, err := syncPreviouslyManagedArtifacts(operation)
	report.Artifacts = artifacts
	if err != nil {
		report.OK = false
		report.Error = fmt.Sprintf("refreshing managed artifacts: %v", err)
		return report
	}
	managed, err := hasManagedInstallReceipt()
	if err != nil {
		report.OK = false
		report.Error = fmt.Sprintf("reading managed installation: %v", err)
		return report
	}
	if !managed {
		report.Integrations = harnessIntegrationReport{
			OpenCode: harnessIntegrationState{State: "skipped", Detail: "no managed installation receipt; setup and login do not enroll artifacts"},
			Hermes:   harnessIntegrationState{State: "skipped", Detail: "no managed installation receipt"},
		}
		return report
	}
	return finishLifecycleIntegrations(ctx, report)
}

func refreshLifecycleIntegrationsWith(ctx context.Context, operation string, syncArtifacts func(bool, string) (managedArtifactReport, error)) lifecycleIntegrationReport {
	report := lifecycleIntegrationReport{OK: true}
	artifacts, err := syncArtifacts(false, operation)
	report.Artifacts = artifacts
	if err != nil {
		report.OK = false
		report.Error = fmt.Sprintf("refreshing managed artifacts: %v", err)
	}
	return finishLifecycleIntegrations(ctx, report)
}

func finishLifecycleIntegrations(ctx context.Context, report lifecycleIntegrationReport) lifecycleIntegrationReport {
	if _, err := loadPointer(); err != nil {
		report.Integrations = harnessIntegrationReport{
			OpenCode: harnessIntegrationState{State: "skipped", Detail: "managed artifacts are synchronized without rewriting OpenCode configuration"},
			Hermes:   harnessIntegrationState{State: "skipped", Detail: "Mimir is not connected"},
		}
		return report
	}
	integrations, err := installCurrentHarnessIntegrations(ctx)
	report.Integrations = integrations
	if err != nil {
		report.OK = false
		if report.Error != "" {
			report.Error += "; "
		}
		report.Error += fmt.Sprintf("refreshing harness configuration: %v", err)
	}
	return report
}

func installCurrentHarnessIntegrations(ctx context.Context) (harnessIntegrationReport, error) {
	report := harnessIntegrationReport{OpenCode: harnessIntegrationState{State: "skipped", Detail: "managed artifacts are synchronized separately; OpenCode configuration is not rewritten"}}
	if _, found, err := discoverHermesHome(); err != nil {
		return report, err
	} else if !found {
		report.Hermes = harnessIntegrationState{State: "skipped", Detail: "Hermes is not installed"}
		return report, nil
	}
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

func managedArtifactCounts(report managedArtifactReport) map[managedArtifactStatus]int {
	counts := make(map[managedArtifactStatus]int)
	for _, artifact := range report.Artifacts {
		counts[artifact.Status]++
	}
	return counts
}

func artifactIssueCount(report managedArtifactReport) int {
	issues := 0
	for status, count := range managedArtifactCounts(report) {
		if status != artifactCurrent && status != artifactInstalled && status != artifactAdopted && status != artifactUpdated && status != artifactRemoved {
			issues += count
		}
	}
	return issues
}

func artifactSummary(report managedArtifactReport) string {
	return fmt.Sprintf("Managed artifacts: %d total, %d issue(s) · receipt %s", len(report.Artifacts), artifactIssueCount(report), report.ReceiptPath)
}
