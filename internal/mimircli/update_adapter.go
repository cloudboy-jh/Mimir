package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

var runLifecycleUpdate = func(ctx context.Context, check bool) (lifecyclepkg.UpdateReport, error) {
	configureInstall()
	return lifecycleService().Update(ctx, check)
}

func cmdUpdate(ctx context.Context, args []string, out io.Writer) error {
	check, jsonOutput := false, false
	for _, arg := range args {
		switch arg {
		case "--check":
			check = true
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("usage: mimir update [--check] [--json]")
		}
	}
	report, err := runLifecycleUpdate(ctx, check)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	message := ""
	switch report.Binary.Status {
	case "current":
		message = fmt.Sprintf("mimir %s is up to date", report.Binary.Current)
	case "available":
		message = fmt.Sprintf("mimir %s available (current %s)", report.Binary.Latest, report.Binary.Current)
	case "updated":
		message = fmt.Sprintf("updated mimir %s → %s", report.Binary.Current, report.Binary.Latest)
	}
	message += "\n" + artifactDetails(report.Artifacts)
	if summary := integrationSummary(report.Integrations); strings.TrimSpace(summary) != "" {
		message += "\n" + summary
	}
	if activation := activationRequired(report.Artifacts, report.Integrations); activation != "" {
		message += "\nActivation required:\n" + activation
	}
	if report.Binary.Status == "updated" {
		message += "\nDeployment:\n  Worker bundle may be behind this CLI version\n  Run: mimir deploy"
	}
	_, err = fmt.Fprintln(out, message)
	return err
}

func artifactDetails(report installpkg.ArtifactReport) string {
	lines := []string{installpkg.ArtifactSummary(report)}
	for _, artifact := range report.Artifacts {
		line := fmt.Sprintf("  %s  %s · %s", artifact.Status, artifact.Source, artifact.Path)
		if artifact.Detail != "" {
			line += " · " + artifact.Detail
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func activationRequired(artifacts installpkg.ArtifactReport, integrations harness.IntegrationReport) string {
	changed := map[string]bool{}
	for _, artifact := range artifacts.Artifacts {
		if artifact.Status != installpkg.ArtifactInstalled && artifact.Status != installpkg.ArtifactMigrated && artifact.Status != installpkg.ArtifactUpdated {
			continue
		}
		switch {
		case artifact.Source == "plugins/opencode/mimir.ts":
			changed["OpenCode"] = true
		case strings.HasPrefix(artifact.Source, "plugins/hermes/"):
			changed["Hermes"] = true
		}
	}
	var lines []string
	if changed["OpenCode"] && integrations.OpenCode.RestartRequired {
		lines = append(lines, "  OpenCode · restart OpenCode to load the updated managed plugin")
	}
	if changed["Hermes"] && integrations.Hermes.RestartRequired {
		lines = append(lines, "  Hermes · restart Hermes to load the updated managed plugin")
	}
	return strings.Join(lines, "\n")
}
