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
	if report.Hermes.State == "installed" {
		lines = append(lines, "Hermes capture installed · restart Hermes", "Hermes scope: OpenRouter proxy plus direct providers")
	}
	return strings.Join(lines, "\n")
}
