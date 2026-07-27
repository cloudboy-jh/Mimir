package mimircli

import (
	"context"
	"os/exec"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness"
	hermesintegration "github.com/cloudboy-jh/mimir/internal/harness/hermes"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
)

var findOpenCode = func() (string, error) { return exec.LookPath("opencode") }

func lifecycleService() lifecyclepkg.Service {
	service := lifecyclepkg.New()
	service.ExecutablePath = executablePath
	service.OpenCode.LookPath = func(string) (string, error) { return findOpenCode() }
	service.Hermes = hermesintegration.New()
	service.Hermes.RunPluginCommand = runHermesPluginCommand
	service.Hermes.ListPlugins = listHermesPlugins
	return service
}

func refreshConnectedLifecycleIntegrations(ctx context.Context, operation string) lifecyclepkg.Report {
	configureInstall()
	return lifecycleService().RefreshConnected(ctx, operation)
}

func integrationSummary(report harness.IntegrationReport) string {
	var lines []string
	if report.OpenCode.RestartRequired {
		lines = append(lines, "OpenCode: restart required")
	}
	if report.Hermes.RestartRequired {
		lines = append(lines, "Hermes: restart required")
	}
	if report.Hermes.State == "installed" && report.Hermes.Scope == "all-providers" {
		lines = append(lines, "Hermes scope: OpenRouter proxy plus direct providers")
	}
	return strings.Join(lines, "\n")
}
