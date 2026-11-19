package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	installpkg "github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

var latestRelease = installpkg.LatestRelease

// runInstaller reruns the release installer into dir. Unix runs install.sh in
// the foreground; Windows hands off to a hidden PowerShell that waits for this
// process to exit so mimir.exe can be replaced. It reports whether the update
// already finished.
var runInstaller = func(ctx context.Context, ioctx IO, dir string) (bool, error) {
	if runtime.GOOS == "windows" {
		script := fmt.Sprintf("Start-Sleep -Seconds 2; irm %s | iex", installpkg.InstallPowerShellURL)
		outer := fmt.Sprintf("Start-Process powershell -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-Command','%s'", script)
		command := exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", outer)
		command.Env = append(os.Environ(), "MIMIR_INSTALL_DIR="+dir)
		if output, err := command.CombinedOutput(); err != nil {
			return false, fmt.Errorf("starting the Mimir installer: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return false, nil
	}
	command := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("curl -fsSL %s | sh", installpkg.InstallScriptURL))
	command.Env = append(os.Environ(), "MIMIR_INSTALL_DIR="+dir)
	command.Stdin, command.Stdout, command.Stderr = ioctx.In, ioctx.Out, ioctx.Err
	if err := command.Run(); err != nil {
		return false, fmt.Errorf("running the Mimir installer: %w", err)
	}
	return true, nil
}

func cmdUpdateIO(ctx context.Context, args []string, ioctx IO) error {
	check, jsonOutput := false, false
	for _, arg := range args {
		switch arg {
		case "--check":
			check = true
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("usage: mimir update [--check [--json]]")
		}
	}
	if jsonOutput && !check {
		return fmt.Errorf("usage: mimir update [--check [--json]]")
	}
	configureInstall()
	if check {
		return checkForUpdate(ctx, ioctx, jsonOutput)
	}
	executable, err := executablePath()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	finished, err := runInstaller(ctx, ioctx, filepath.Dir(executable))
	if err != nil {
		return err
	}
	if !finished {
		_, err = fmt.Fprintln(ioctx.Out, "Mimir update continues in the background; run mimir version in a few seconds to confirm.")
		return err
	}
	if err := mimirapi.SaveExecutable(executable); err != nil && ioctx.Err != nil {
		_, _ = fmt.Fprintf(ioctx.Err, "warning: recording executable path: %v\n", err)
	}
	return nil
}

func checkForUpdate(ctx context.Context, ioctx IO, jsonOutput bool) error {
	latest, err := latestRelease(ctx)
	if err != nil {
		return err
	}
	current := strings.TrimPrefix(version, "v")
	status := "current"
	if installpkg.CompareVersions(current, latest) < 0 {
		status = "available"
	}
	if jsonOutput {
		return json.NewEncoder(ioctx.Out).Encode(map[string]any{"check": true, "current": current, "latest": latest, "status": status})
	}
	if status == "available" {
		_, err = fmt.Fprintf(ioctx.Out, "Mimir update available: %s -> %s\n", current, latest)
	} else {
		_, err = fmt.Fprintf(ioctx.Out, "Mimir is up to date: %s\n", current)
	}
	return err
}
