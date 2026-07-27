//go:build windows

package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	swapMaxAttempts = 5
	swapBaseDelay   = 150 * time.Millisecond
)

var renameFile = os.Rename

var (
	kernel32                      = syscall.NewLazyDLL("kernel32.dll")
	lockFileProc                  = kernel32.NewProc("LockFile")
	unlockFileProc                = kernel32.NewProc("UnlockFile")
	queryFullProcessImageNameProc = kernel32.NewProc("QueryFullProcessImageNameW")
)

// platformSwap moves the running executable aside and renames the staged
// binary into place, retrying transient lock failures from antivirus filters
// and process handles. The expected hash lets retries safely recover an
// interrupted target -> .old move before trying again.
func platformSwap(target, staged, expectedHash, stagedHash string) error {
	return withSwapLock(func() error {
		return platformSwapLocked(target, staged, expectedHash, stagedHash)
	})
}

func withSwapLock(run func() error) error {
	paths, err := managedInstallationPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.MimirHome, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(paths.MimirHome, "update-swap.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	locked, _, lockErr := lockFileProc.Call(file.Fd(), 0, 0, 1, 0)
	if locked == 0 {
		return fmt.Errorf("%w: acquiring update swap lock: %v", errExecutableLocked, lockErr)
	}
	defer unlockFileProc.Call(file.Fd(), 0, 0, 1, 0)
	return run()
}

func platformSwapLocked(target, staged, expectedHash, stagedHash string) error {
	old := target + ".old"
	var err error
	for attempt := 0; attempt < swapMaxAttempts; attempt++ {
		if err = ensureSwapTarget(target, old, expectedHash); err != nil {
			if !isExecutableLockError(err) {
				return err
			}
			time.Sleep(time.Duration(attempt+1) * swapBaseDelay)
			continue
		}
		if _, err = readVerifiedPendingFile(target, expectedHash, "target"); err != nil {
			return err
		}
		if _, err = readVerifiedPendingFile(staged, stagedHash, "staged binary"); err != nil {
			return err
		}
		if err = os.Remove(old); err != nil && !os.IsNotExist(err) {
			if !isExecutableLockError(err) {
				return err
			}
			time.Sleep(time.Duration(attempt+1) * swapBaseDelay)
			continue
		}
		if err = swapAside(target, staged, old, expectedHash, stagedHash); err == nil {
			_ = os.Remove(old)
			return nil
		}
		if !isExecutableLockError(err) && !errors.Is(err, errExecutableLocked) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * swapBaseDelay)
	}
	return fmt.Errorf("%w: %v", errExecutableLocked, err)
}

func ensureSwapTarget(target, old, expectedHash string) error {
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("swap target is not a regular file")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := readVerifiedPendingFile(old, expectedHash, "renamed original"); err != nil {
		return err
	}
	return renameFile(old, target)
}

func platformRecoverTarget(target, expectedHash string) error {
	return ensureSwapTarget(target, target+".old", expectedHash)
}

func swapAside(target, staged, old, expectedHash, stagedHash string) error {
	if err := renameFile(target, old); err != nil {
		return err
	}
	if _, err := readVerifiedPendingFile(old, expectedHash, "renamed original"); err != nil {
		if rollbackErr := renameFile(old, target); rollbackErr != nil {
			return fmt.Errorf("%w: original changed during swap: %v; restoring target failed: %v", errExecutableLocked, err, rollbackErr)
		}
		return err
	}
	if err := renameFile(staged, target); err != nil {
		if rollbackErr := renameFile(old, target); rollbackErr != nil {
			return fmt.Errorf("%w: installing staged binary failed: %v; restoring original failed: %v", errExecutableLocked, err, rollbackErr)
		}
		return err
	}
	if _, err := readVerifiedPendingFile(target, stagedHash, "installed binary"); err != nil {
		removeErr := os.Remove(target)
		rollbackErr := renameFile(old, target)
		if removeErr != nil || rollbackErr != nil {
			return fmt.Errorf("%w: installed binary changed during swap: %v; removing it: %v; restoring original: %v", errExecutableLocked, err, removeErr, rollbackErr)
		}
		return err
	}
	return nil
}

func isExecutableLockError(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.Errno(5), syscall.Errno(32), syscall.Errno(33): // access denied, sharing violation, lock violation
			return true
		}
	}
	return false
}

type windowsProcess struct {
	ProcessID      int    `json:"ProcessId"`
	ExecutablePath string `json:"ExecutablePath"`
}

var listWindowsProcesses = func() ([]byte, error) {
	const script = `$processes = @(Get-CimInstance Win32_Process | Where-Object { $null -ne $_.ExecutablePath } | Select-Object ProcessId, ExecutablePath)
ConvertTo-Json -Compress -InputObject $processes`
	return exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script).Output()
}

// siblingMimirProcesses returns only processes executing the exact
// receipt-owned binary path. Same-name executables elsewhere are never killed
// or treated as blockers.
var siblingMimirProcesses = func(self int, target string) ([]int, error) {
	data, err := listWindowsProcesses()
	if err != nil {
		return nil, err
	}
	var processes []windowsProcess
	if err := json.Unmarshal(data, &processes); err != nil {
		return nil, fmt.Errorf("decoding Windows process list: %w", err)
	}
	var pids []int
	for _, process := range processes {
		if process.ProcessID == self || process.ExecutablePath == "" || !sameFilePath(process.ExecutablePath, target) {
			continue
		}
		pids = append(pids, process.ProcessID)
	}
	return pids, nil
}

var killProcess = func(pid int, target string) error {
	const (
		processTerminate               = 0x0001
		processQueryLimitedInformation = 0x1000
	)
	handle, err := syscall.OpenProcess(processTerminate|processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	ok, _, queryErr := queryFullProcessImageNameProc.Call(
		uintptr(handle),
		0,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ok == 0 {
		return fmt.Errorf("querying process %d executable: %v", pid, queryErr)
	}
	if !sameFilePath(syscall.UTF16ToString(buffer[:size]), target) {
		return fmt.Errorf("refusing to terminate process %d after executable path changed", pid)
	}
	return syscall.TerminateProcess(handle, 1)
}

// stopSiblingMimirProcesses force-kills sibling processes using the exact
// managed executable and waits briefly for the image to be released.
func stopSiblingMimirProcesses(self int, target string) []int {
	pids, err := siblingMimirProcesses(self, target)
	if err != nil || len(pids) == 0 {
		return nil
	}
	stopped := []int{}
	for _, pid := range pids {
		if killProcess(pid, target) == nil {
			stopped = append(stopped, pid)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		remaining, err := siblingMimirProcesses(self, target)
		if err != nil || len(remaining) == 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	return stopped
}

func writeExecutableTemp(dir, pattern string, binary []byte) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(binary); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

func cleanupStaleUpdateHelpers() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Hour)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "mimir-update-helper-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(os.TempDir(), entry.Name()))
	}
}

// scheduleDeferredSwap writes the verified release to exclusive random files:
// one next to the target for the atomic swap and one in the temp directory as
// a detached helper. The helper executes the same receipt/hash validation as
// every later CLI run; it never receives mutable target paths as arguments.
func scheduleDeferredSwap(target string, binary []byte, previousHash, newVersion string) (string, error) {
	paths, err := managedInstallationPaths()
	if err != nil {
		return "", err
	}
	cleanupStaleUpdateHelpers()
	staged, err := writeExecutableTemp(filepath.Dir(target), ".mimir-update-pending-*", binary)
	if err != nil {
		return "", err
	}
	helper, err := writeExecutableTemp(os.TempDir(), "mimir-update-helper-*.exe", binary)
	if err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	pending := pendingUpdate{
		Schema: 1, Target: target, Staged: staged,
		PreviousHash: previousHash, NewHash: hashBytes(binary), Version: newVersion,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := savePendingUpdate(paths, pending); err != nil {
		_ = os.Remove(staged)
		_ = os.Remove(helper)
		return "", err
	}
	cmd := exec.Command(helper, "_apply-update")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNoWindow,
	}
	if err := startDetachedProcess(cmd); err != nil {
		_ = os.Remove(staged)
		_ = os.Remove(helper)
		_ = removePendingUpdate(paths)
		return "", fmt.Errorf("launching deferred update helper: %w", err)
	}
	pids, _ := siblingMimirProcesses(os.Getpid(), target)
	if len(pids) == 0 {
		return "the update will apply after the current command exits", nil
	}
	return "blocked by Mimir process(es) " + joinInts(pids) + "; the update will apply after they exit", nil
}
