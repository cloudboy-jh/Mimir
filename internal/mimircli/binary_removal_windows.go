//go:build windows

package mimircli

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const (
	detachedProcess = 0x00000008
	createNoWindow  = 0x08000000

	deferredDeletePowerShell = `$parentProcess = Get-Process -Id ([int]$env:MIMIR_UNINSTALL_PID) -ErrorAction SilentlyContinue
if ($null -ne $parentProcess) { Wait-Process -InputObject $parentProcess }
Remove-Item -LiteralPath $env:MIMIR_UNINSTALL_PATH -Force -ErrorAction SilentlyContinue`
)

var startDetachedProcess = func(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func platformLaunchDeferredBinaryRemoval(pid int, path string) error {
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", deferredDeletePowerShell)
	cmd.Env = append(os.Environ(),
		"MIMIR_UNINSTALL_PID="+strconv.Itoa(pid),
		"MIMIR_UNINSTALL_PATH="+path,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNoWindow,
	}
	return startDetachedProcess(cmd)
}
