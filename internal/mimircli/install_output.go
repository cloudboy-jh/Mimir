package mimircli

import (
	"fmt"
	"io"
	"strings"

	doctorpkg "github.com/cloudboy-jh/mimir/internal/doctor"
	"github.com/cloudboy-jh/mimir/internal/harness"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

func renderDoctor(out io.Writer, report doctorpkg.Report) error {
	lines := []string{"Mimir doctor"}
	for _, check := range report.Checks {
		lines = append(lines, statusLine(check.Status, check.Name, check.Detail))
		if check.Repair != "" {
			lines = append(lines, "            repair: "+check.Repair)
		}
	}
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

type namedIntegration struct {
	name  string
	state harness.IntegrationState
}

func namedIntegrations(report harness.IntegrationReport) []namedIntegration {
	return []namedIntegration{
		{"Pi", report.Pi}, {"Oh My Pi", report.OhMyPi}, {"OpenCode", report.OpenCode}, {"Hermes", report.Hermes},
		{"Claude Code", report.ClaudeCode}, {"Codex", report.Codex}, {"Cursor", report.Cursor},
	}
}

func renderInstall(out io.Writer, report lifecyclepkg.InstallReport) error {
	verb := map[string]string{"installed": "installed", "updated": "updated", "current": "already installed"}[report.Binary.Status]
	lines := []string{fmt.Sprintf("Mimir %s %s: %s", report.Binary.Version, verb, report.Binary.Path)}
	integrations := namedIntegrations(report.Integrations)
	var attention, restarts []string
	for _, artifact := range report.Artifacts.Artifacts {
		if !installpkg.ArtifactUsable(artifact.Status) && artifact.Status != installpkg.ArtifactRemoved && artifact.Snippet == "" {
			attention = append(attention, statusLine(string(artifact.Status), artifact.Path, artifact.Detail))
		}
	}
	for _, integration := range integrations {
		if integration.state.State == "failed" {
			attention = append(attention, statusLine("failed", integration.name, integration.state.Detail))
		}
		if integration.state.RestartRequired {
			restarts = append(restarts, integration.name)
		}
	}
	if len(attention) > 0 {
		lines = append(append(lines, "Needs attention"), attention...)
	}
	lines = append(lines, manualMCPLines(report.Artifacts)...)
	for _, warning := range report.Warnings {
		lines = append(lines, "Warning: "+warning)
	}
	if len(restarts) > 0 {
		lines = append(lines, "Next: restart "+strings.Join(restarts, ", ")+" to load Mimir")
	}
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

// manualMCPLines prints the MCP configuration the user must merge into a
// shared file Mimir does not own.
func manualMCPLines(report installpkg.ArtifactReport) []string {
	var lines []string
	for _, step := range installpkg.ManualSteps(report) {
		lines = append(lines, fmt.Sprintf("Manual MCP setup (%s): add the mimir server to %s:", harnessDisplayName(step.Harness), step.Path))
		for _, line := range strings.Split(step.Snippet, "\n") {
			lines = append(lines, "    "+line)
		}
	}
	return lines
}
