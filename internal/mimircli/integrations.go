package mimircli

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
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
			OpenCode: harnessIntegrationState{State: "skipped", Detail: "Mimir is not connected"},
			Hermes:   harnessIntegrationState{State: "skipped", Detail: "Mimir is not connected"},
		}
		return report
	}
	integrations, err := installCurrentHarnessIntegrations(ctx, report.Artifacts)
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

func installCurrentHarnessIntegrations(ctx context.Context, artifacts managedArtifactReport) (harnessIntegrationReport, error) {
	report := harnessIntegrationReport{}
	var failures []string
	paths, pathsErr := managedInstallationPaths()
	if pathsErr != nil {
		return report, pathsErr
	}
	if harnessArtifactsReady(artifacts, paths.OpenCodeHome, "plugins/opencode/", "skills/mimir-") {
		openCode, openCodeErr := installCurrentOpenCodeMCP(ctx)
		report.OpenCode = openCode
		if openCodeErr != nil {
			failures = append(failures, openCodeErr.Error())
		}
	} else {
		report.OpenCode = harnessIntegrationState{State: "failed", Scope: "mcp", Detail: "conflicting or modified OpenCode files were preserved"}
		failures = append(failures, report.OpenCode.Detail)
	}
	if _, found, err := discoverHermesHome(); err != nil {
		failures = append(failures, err.Error())
	} else if !found {
		report.Hermes = harnessIntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	} else if !harnessArtifactsReady(artifacts, paths.HermesHome, "plugins/hermes/") {
		report.Hermes = harnessIntegrationState{State: "failed", Scope: "all-providers", Detail: "conflicting or modified Hermes plugin files were preserved"}
		failures = append(failures, report.Hermes.Detail)
	} else if installed, err := installCurrentHermesIntegration(ctx); err != nil {
		report.Hermes = harnessIntegrationState{State: "failed", Provider: "openrouter", Scope: "openrouter", Detail: err.Error()}
		failures = append(failures, err.Error())
	} else if installed {
		report.Hermes = harnessIntegrationState{State: "installed", Provider: "openrouter", Scope: "all-providers", RestartRequired: true, Detail: "OpenRouter proxy and direct-provider lifecycle capture installed"}
	} else {
		report.Hermes = harnessIntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	}
	if len(failures) > 0 {
		return report, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return report, nil
}

var findOpenCode = func() (string, error) { return exec.LookPath("opencode") }

func installCurrentOpenCodeMCP(ctx context.Context) (harnessIntegrationState, error) {
	_, err := findOpenCode()
	if err != nil {
		return harnessIntegrationState{State: "skipped", Detail: "OpenCode is not installed"}, nil
	}
	executable, err := manifestExecutable()
	if err != nil {
		return harnessIntegrationState{State: "failed", Scope: "mcp", Detail: err.Error()}, err
	}
	command := []string{executable, "serve"}
	_ = ctx
	return harnessIntegrationState{State: "installed", Scope: "mcp", RestartRequired: true, Detail: "managed OpenCode plugin injects Mimir MCP: " + strings.Join(command, " ")}, nil
}

func harnessArtifactsReady(report managedArtifactReport, root string, prefixes ...string) bool {
	found := false
	for _, artifact := range report.Artifacts {
		if root != "" {
			rel, err := filepath.Rel(root, artifact.Path)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
		}
		matched := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(filepath.ToSlash(artifact.Source), prefix) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		found = true
		switch artifact.Status {
		case artifactCurrent, artifactInstalled, artifactAdopted, artifactMigrated, artifactUpdated:
		default:
			return false
		}
	}
	return found
}

func integrationSummary(report harnessIntegrationReport) string {
	var lines []string
	if report.Hermes.State == "installed" {
		lines = append(lines, "Hermes capture installed · restart Hermes", "Hermes scope: OpenRouter proxy plus direct providers")
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
		if status != artifactCurrent && status != artifactInstalled && status != artifactAdopted && status != artifactMigrated && status != artifactUpdated && status != artifactRemoved {
			issues += count
		}
	}
	return issues
}

func artifactSummary(report managedArtifactReport) string {
	return fmt.Sprintf("Managed artifacts: %d total, %d issue(s) · receipt %s", len(report.Artifacts), artifactIssueCount(report), report.ReceiptPath)
}
