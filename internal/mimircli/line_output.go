package mimircli

import (
	"fmt"
	"io"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/ui/lineoutput"
)

type namedIntegration struct {
	name  string
	state harness.IntegrationState
}

func writeAttention(out io.Writer, artifacts installpkg.ArtifactReport, integrations []namedIntegration) error {
	issues := artifactIssues(artifacts)
	failed := make([]namedIntegration, 0, len(integrations))
	for _, integration := range integrations {
		if integration.state.State == "failed" {
			failed = append(failed, integration)
		}
	}
	if len(issues) == 0 && len(failed) == 0 {
		return nil
	}
	lines := lineoutput.New(out)
	if err := lines.Warning("Needs attention"); err != nil {
		return err
	}
	for _, artifact := range issues {
		line := fmt.Sprintf("  %s (%s)", artifact.Path, artifact.Status)
		if detail := strings.TrimSpace(artifact.Detail); detail != "" {
			line += ": " + detail
		}
		if err := lines.Detail(strings.TrimSpace(line)); err != nil {
			return err
		}
	}
	for _, integration := range failed {
		detail := strings.TrimSpace(integration.state.Detail)
		if detail == "" {
			detail = integration.state.State
		}
		if err := lines.Detail(fmt.Sprintf("%s: %s", integration.name, detail)); err != nil {
			return err
		}
	}
	return nil
}

func artifactIssues(report installpkg.ArtifactReport) []installpkg.ArtifactResult {
	issues := make([]installpkg.ArtifactResult, 0)
	for _, artifact := range report.Artifacts {
		switch artifact.Status {
		case installpkg.ArtifactCurrent, installpkg.ArtifactInstalled, installpkg.ArtifactAdopted,
			installpkg.ArtifactMigrated, installpkg.ArtifactUpdated, installpkg.ArtifactRemoved:
			continue
		default:
			issues = append(issues, artifact)
		}
	}
	return issues
}

func restartNames(integrations []namedIntegration) []string {
	names := make([]string, 0, len(integrations))
	for _, integration := range integrations {
		if integration.state.RestartRequired {
			names = append(names, integration.name)
		}
	}
	return names
}
