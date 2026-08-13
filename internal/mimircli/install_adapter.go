package mimircli

import (
	"context"
	"os"

	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

var executablePath = os.Executable

func configureInstall() {
	installpkg.SetBuildInfo(installpkg.BuildInfo{Version: version, Commit: commit, Date: date})
}

func installManaged(ctx context.Context, explicitDir string, step func(string)) (lifecyclepkg.InstallReport, error) {
	return installManagedHarnesses(ctx, explicitDir, nil, step)
}

func installManagedHarnesses(ctx context.Context, explicitDir string, harnesses []string, step func(string)) (lifecyclepkg.InstallReport, error) {
	configureInstall()
	service := lifecycleService()
	service.Step = step
	return service.InstallSelected(ctx, explicitDir, harnesses, executablePath)
}

var runLifecycleInstall = installManaged
var runLifecycleHarnessInstall = installManagedHarnesses
