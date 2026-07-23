//go:build windows

package mimircli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDeferredBinaryRemovalTransportsExactPathOutsideScript(t *testing.T) {
	path := `C:\Users\standard user\mimir $value 'quoted' & cleanup.exe.uninstall`
	pid := 4242
	oldStart := startDetachedProcess
	startDetachedProcess = func(cmd *exec.Cmd) error {
		if got := strings.ToLower(cmd.Args[0]); !strings.HasSuffix(got, `powershell.exe`) {
			t.Fatalf("executable = %q", cmd.Args[0])
		}
		if got := cmd.Args[len(cmd.Args)-1]; got != deferredDeletePowerShell {
			t.Fatalf("PowerShell source changed: %q", got)
		}
		if !strings.Contains(deferredDeletePowerShell, "Remove-Item -LiteralPath $env:MIMIR_UNINSTALL_PATH") {
			t.Fatalf("PowerShell source does not use literal-path semantics: %q", deferredDeletePowerShell)
		}
		if strings.Contains(strings.Join(cmd.Args, "\x00"), path) {
			t.Fatal("path was interpolated into command arguments")
		}
		if got := commandEnvironmentValue(cmd.Env, "MIMIR_UNINSTALL_PID"); got != strconv.Itoa(pid) {
			t.Fatalf("PID environment = %q", got)
		}
		if got := commandEnvironmentValue(cmd.Env, "MIMIR_UNINSTALL_PATH"); got != path {
			t.Fatalf("path environment = %q, want exact %q", got, path)
		}
		if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&(detachedProcess|createNoWindow) != detachedProcess|createNoWindow {
			t.Fatalf("process is not detached and hidden: %#v", cmd.SysProcAttr)
		}
		return nil
	}
	t.Cleanup(func() { startDetachedProcess = oldStart })

	if err := platformLaunchDeferredBinaryRemoval(pid, path); err != nil {
		t.Fatal(err)
	}
}

func TestDeferredBinaryRemovalLaunchFailureRetainsReceiptPath(t *testing.T) {
	paths := isolatedInstallation(t, false)
	binary := filepath.Join(t.TempDir(), "mimir.exe")
	data := []byte("managed binary")
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := syncInstallArtifacts(installReceiptUpdate{
		Source: "test", Method: "bootstrap-copy",
		CLI: installReceiptCLI{Path: binary, Hash: hashBytes(data)},
	}); err != nil {
		t.Fatal(err)
	}
	oldLauncher := launchDeferredBinaryRemoval
	launchDeferredBinaryRemoval = func(int, string) error { return errors.New("launch denied") }
	t.Cleanup(func() { launchDeferredBinaryRemoval = oldLauncher })

	report, err := uninstallManagedInstallation(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Result != "partial" || report.Binary.Status != "pending-removal-unscheduled" || !strings.Contains(report.Binary.Detail, "preserved") {
		t.Fatalf("report = %#v", report)
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.CLI.Path != report.Binary.PendingPath || receipt.CLI.Path == binary {
		t.Fatalf("receipt path = %q, pending path = %q", receipt.CLI.Path, report.Binary.PendingPath)
	}
	if _, err := os.Stat(receipt.CLI.Path); err != nil {
		t.Fatalf("receipt-owned renamed binary was not preserved: %v", err)
	}
	if _, err := os.Stat(paths.Receipt); err != nil {
		t.Fatalf("install receipt missing: %v", err)
	}
}

func commandEnvironmentValue(environment []string, name string) string {
	prefix := strings.ToUpper(name) + "="
	for i := len(environment) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.ToUpper(environment[i]), prefix) {
			return environment[i][len(prefix):]
		}
	}
	return ""
}
