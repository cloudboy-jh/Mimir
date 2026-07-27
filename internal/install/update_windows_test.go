//go:build windows

package install

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPlatformSwapReportsPersistentLock(t *testing.T) {
	isolatedInstallation(t, false)
	target := filepath.Join(t.TempDir(), "mimir.exe")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	staged := target + ".staged"
	if err := os.WriteFile(staged, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Go opens files without FILE_SHARE_DELETE, so this handle denies the
	// rename the same way another process using the binary does.
	lock, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	if err := platformSwap(target, staged, hashBytes([]byte("old-binary")), hashBytes([]byte("new-binary"))); !errors.Is(err, errExecutableLocked) {
		t.Fatalf("error = %v, want errExecutableLocked", err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "old-binary" {
		t.Fatalf("locked binary changed: %q", data)
	}
}

func TestPlatformSwapPreservesRecoverableOldBinaryWhenRollbackIsLocked(t *testing.T) {
	isolatedInstallation(t, false)
	target := filepath.Join(t.TempDir(), "mimir.exe")
	oldData := []byte("old-binary")
	if err := os.WriteFile(target, oldData, 0o755); err != nil {
		t.Fatal(err)
	}
	staged := target + ".staged"
	if err := os.WriteFile(staged, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldRename := renameFile
	t.Cleanup(func() { renameFile = oldRename })
	calls := 0
	renameFile = func(from, to string) error {
		calls++
		if calls == 1 {
			return os.Rename(from, to)
		}
		return syscall.Errno(32)
	}
	err := platformSwap(target, staged, hashBytes(oldData), hashBytes([]byte("new-binary")))
	renameFile = oldRename
	if !errors.Is(err, errExecutableLocked) {
		t.Fatalf("error = %v, want errExecutableLocked", err)
	}
	if data, readErr := os.ReadFile(target + ".old"); readErr != nil || string(data) != string(oldData) {
		t.Fatalf("recoverable old binary = %q, %v", data, readErr)
	}
	if err := platformRecoverTarget(target, hashBytes(oldData)); err != nil {
		t.Fatal(err)
	}
	if data, readErr := os.ReadFile(target); readErr != nil || string(data) != string(oldData) {
		t.Fatalf("recovered target = %q, %v", data, readErr)
	}
}

func TestUpdateSchedulesDeferredSwapWhenLocked(t *testing.T) {
	isolatedInstallation(t, false)
	server := stubReleaseServer(t, "9.9.9", []byte("new-binary"), false)
	oldBase := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	target := filepath.Join(t.TempDir(), "mimir.exe")
	ownExecutable(t, target, []byte("old-binary"))
	oldExec := executablePath
	executablePath = func() (string, error) { return target, nil }
	t.Cleanup(func() { executablePath = oldExec })
	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })
	stubSiblings(t, []int{4321})

	launched := false
	oldStart := startDetachedProcess
	startDetachedProcess = func(cmd *exec.Cmd) error {
		launched = true
		if len(cmd.Args) != 2 || cmd.Args[1] != "_apply-update" {
			t.Fatalf("helper command = %#v", cmd.Args)
		}
		t.Cleanup(func() { _ = os.Remove(cmd.Args[0]) })
		return nil
	}
	t.Cleanup(func() { startDetachedProcess = oldStart })

	lock, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Update(context.Background(), UpdateOptions{AfterReplace: func(context.Context, string) (ArtifactReport, error) {
		return checkManagedArtifacts()
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Binary.Status != "scheduled" || report.Binary.Current != "1.0.0" || report.Binary.Latest != "9.9.9" {
		t.Fatalf("report %#v", report.Binary)
	}
	if !strings.Contains(report.Binary.Detail, "4321") {
		t.Fatalf("detail %q does not name the blocking process", report.Binary.Detail)
	}
	if !launched {
		t.Fatal("deferred swap helper was not launched")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "old-binary" {
		t.Fatalf("locked binary changed: %q", data)
	}
	paths, err := managedInstallationPaths()
	if err != nil {
		t.Fatal(err)
	}
	pending, found, err := loadPendingUpdate(paths)
	if err != nil || !found {
		t.Fatalf("pending update found=%v err=%v", found, err)
	}
	staged, err := os.ReadFile(pending.Staged)
	if err != nil || string(staged) != "new-binary" {
		t.Fatalf("staged binary %q err %v", staged, err)
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.CLI.Hash != hashBytes([]byte("old-binary")) {
		t.Fatal("receipt moved to the new hash before the swap happened")
	}
	if applied, _, err := finalizePendingUpdate(); err != nil || applied {
		t.Fatalf("locked finalize applied=%v err=%v", applied, err)
	}

	// Once the lock is released, finalization completes the swap and repairs
	// the receipt without another download.
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	stubSiblings(t, nil)
	applied, pendingVersion, err := finalizePendingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !applied || pendingVersion != "9.9.9" {
		t.Fatalf("applied=%v version=%q", applied, pendingVersion)
	}
	data, _ = os.ReadFile(target)
	if string(data) != "new-binary" {
		t.Fatalf("binary %q", data)
	}
	receipt, err = loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.CLI.Hash != hashBytes([]byte("new-binary")) || receipt.CLI.Version != "9.9.9" {
		t.Fatalf("receipt %#v", receipt.CLI)
	}
}

func TestStopSiblingMimirProcessesKillsAndWaits(t *testing.T) {
	calls := 0
	oldSiblings := siblingMimirProcesses
	siblingMimirProcesses = func(int, string) ([]int, error) {
		calls++
		if calls == 1 {
			return []int{42, 43}, nil
		}
		return nil, nil
	}
	t.Cleanup(func() { siblingMimirProcesses = oldSiblings })
	killed := []int{}
	oldKill := killProcess
	killProcess = func(pid int, target string) error {
		killed = append(killed, pid)
		return nil
	}
	t.Cleanup(func() { killProcess = oldKill })

	stopped := stopSiblingMimirProcesses(os.Getpid(), `C:\Tools\mimir.exe`)
	if len(stopped) != 2 || stopped[0] != 42 || stopped[1] != 43 {
		t.Fatalf("stopped %v", stopped)
	}
	if len(killed) != 2 || killed[0] != 42 || killed[1] != 43 {
		t.Fatalf("killed %v", killed)
	}
}

func TestSiblingMimirProcessesMatchesExactExecutablePath(t *testing.T) {
	target := filepath.Join(t.TempDir(), "mimir.exe")
	other := filepath.Join(t.TempDir(), "mimir.exe")
	data, err := json.Marshal([]windowsProcess{
		{ProcessID: 42, ExecutablePath: target},
		{ProcessID: 43, ExecutablePath: other},
		{ProcessID: 44, ExecutablePath: target},
	})
	if err != nil {
		t.Fatal(err)
	}
	oldList := listWindowsProcesses
	listWindowsProcesses = func() ([]byte, error) { return data, nil }
	t.Cleanup(func() { listWindowsProcesses = oldList })
	pids, err := siblingMimirProcesses(44, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(pids) != 1 || pids[0] != 42 {
		t.Fatalf("pids = %v", pids)
	}
}

func TestScheduleDeferredSwapLaunchFailureCleansUp(t *testing.T) {
	paths := isolatedInstallation(t, false)
	target := filepath.Join(t.TempDir(), "mimir.exe")
	oldStart := startDetachedProcess
	startDetachedProcess = func(*exec.Cmd) error { return errors.New("launch denied") }
	t.Cleanup(func() { startDetachedProcess = oldStart })

	if _, err := scheduleDeferredSwap(target, []byte("new-binary"), hashBytes([]byte("old-binary")), "9.9.9"); err == nil {
		t.Fatal("launch failure was not reported")
	}
	if _, found, _ := loadPendingUpdate(paths); found {
		t.Fatal("pending marker survived a failed helper launch")
	}
	staged, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".mimir-update-pending-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 0 {
		t.Fatalf("staged binary survived failed helper launch: %v", staged)
	}
}
