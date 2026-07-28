package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func buildReleaseArchive(t *testing.T, goos string, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if goos == "windows" {
		writer := zip.NewWriter(&buf)
		entry, err := writer.Create("mimir.exe")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(binary); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gz)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "mimir", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func stubReleaseServer(t *testing.T, version string, binary []byte, corrupt bool) *httptest.Server {
	t.Helper()
	archive := buildReleaseArchive(t, runtime.GOOS, binary)
	sum := sha256.Sum256(archive)
	checksum := fmt.Sprintf("%x  %s\n", sum, releaseAssetName(version, runtime.GOOS, runtime.GOARCH))
	if corrupt {
		checksum = fmt.Sprintf("%064x  %s\n", 0, releaseAssetName(version, runtime.GOOS, runtime.GOARCH))
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + updateRepo + "/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v" + version,
				"assets": []map[string]string{
					{"name": releaseAssetName(version, runtime.GOOS, runtime.GOARCH), "browser_download_url": server.URL + "/asset"},
					{"name": "checksums.txt", "browser_download_url": server.URL + "/checksums"},
				},
			})
		case "/asset":
			_, _ = w.Write(archive)
		case "/checksums":
			_, _ = w.Write([]byte(checksum))
		default:
			t.Fatalf("request %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestUpdateInstallsVerifiedBinary(t *testing.T) {
	isolatedInstallation(t, false)
	server := stubReleaseServer(t, "9.9.9", []byte("new-binary"), false)
	oldBase := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	target := filepath.Join(t.TempDir(), "mimir")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := syncInstallArtifacts(installReceiptUpdate{Source: "test", Method: "bootstrap-copy", CLI: installReceiptCLI{Path: target, Version: "1.0.0", Hash: hashBytes([]byte("old-binary"))}}); err != nil {
		t.Fatal(err)
	}
	oldExec := executablePath
	executablePath = func() (string, error) { return target, nil }
	t.Cleanup(func() { executablePath = oldExec })
	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })

	var progress []string
	report, err := Update(context.Background(), UpdateOptions{Progress: func(message string) { progress = append(progress, message) }, AfterReplace: func(context.Context, string) (ArtifactReport, error) {
		return checkManagedArtifacts()
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Binary.Status != "updated" || report.Binary.Current != "1.0.0" || report.Binary.Latest != "9.9.9" {
		t.Fatalf("report %#v", report)
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new-binary" {
		t.Fatalf("binary %q", contents)
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.CLI.Version != "9.9.9" || receipt.CLI.Hash != hashBytes([]byte("new-binary")) {
		t.Fatalf("updated receipt %#v", receipt.CLI)
	}
	joinedProgress := strings.Join(progress, "\n")
	for _, expected := range []string{"Checking latest release", "Downloading Mimir 9.9.9", "Verifying release checksum", "Replacing Mimir executable", "Refreshing managed integrations"} {
		if !strings.Contains(joinedProgress, expected) {
			t.Fatalf("progress missing %q:\n%s", expected, joinedProgress)
		}
	}
}

func TestUpdateRejectsChecksumMismatch(t *testing.T) {
	isolatedInstallation(t, false)
	server := stubReleaseServer(t, "9.9.9", []byte("new-binary"), true)
	oldBase := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	target := filepath.Join(t.TempDir(), "mimir")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExec := executablePath
	executablePath = func() (string, error) { return target, nil }
	t.Cleanup(func() { executablePath = oldExec })

	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })

	if _, err := Update(context.Background(), UpdateOptions{}); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error %v", err)
	}
	contents, _ := os.ReadFile(target)
	if string(contents) != "old-binary" {
		t.Fatalf("binary replaced despite mismatch: %q", contents)
	}
}

func TestInstallBinaryRequiredHashPreservesConcurrentReplacement(t *testing.T) {
	target := filepath.Join(t.TempDir(), "mimir")
	if err := os.WriteFile(target, []byte("external-replacement"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installBinary(target, []byte("rollback"), hashBytes([]byte("expected-update"))); err == nil {
		t.Fatal("concurrent replacement was overwritten")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "external-replacement" {
		t.Fatalf("concurrent replacement changed to %q", data)
	}
}

func TestRollbackUpdatedBinaryRestoresWindowsOldPath(t *testing.T) {
	target := filepath.Join(t.TempDir(), "mimir")
	previous := []byte("previous-binary")
	updated := []byte("updated-binary")
	if err := os.WriteFile(target, updated, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+".old", previous, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := rollbackUpdatedBinary(target, previous, hashBytes(updated)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(previous) {
		t.Fatalf("rollback restored %q", data)
	}
}

func TestUpdateCheckAndCurrent(t *testing.T) {
	isolatedInstallation(t, false)
	server := stubReleaseServer(t, "1.0.0", []byte("binary"), false)
	oldBase := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })

	report, err := Update(context.Background(), UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Binary.Status != "current" {
		t.Fatalf("report %#v", report)
	}

	version = "0.9.0"
	report, err = Update(context.Background(), UpdateOptions{Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Binary.Status != "available" || report.Binary.Current != "0.9.0" || report.Binary.Latest != "1.0.0" {
		t.Fatalf("report %#v", report)
	}
}

func TestUpdateCheckIsReadOnlyAndReportsArtifactDrift(t *testing.T) {
	paths := isolatedInstallation(t, false)
	server := stubReleaseServer(t, "1.0.0", []byte("binary"), false)
	oldBase := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = oldBase })
	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })

	report, err := Update(context.Background(), UpdateOptions{Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Binary.Status != "current" || artifactIssueCount(report.Artifacts) == 0 {
		t.Fatalf("update report %#v", report)
	}
	for _, path := range []string{paths.Receipt, paths.Log} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("update check created %s", path)
		}
	}
}

func TestUpdateCurrentRefreshesManagedArtifacts(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if _, err := syncManagedArtifacts(true, "install"); err != nil {
		t.Fatal(err)
	}
	server := stubReleaseServer(t, "1.0.0", []byte("binary"), false)
	oldBase := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = oldBase })
	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })

	if _, err := Update(context.Background(), UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if lines := jsonLines(t, paths.Log); lines != 2 {
		t.Fatalf("install log lines = %d, want 2", lines)
	}
}

func TestUpdateCurrentEnrollsNewGlobalHarnessArtifact(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if _, err := syncManagedArtifacts(true, "install"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(paths.OpenCodeHome, "skills", "mimir-use", "SKILL.md")
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	delete(receipt.Artifacts, target)
	if err := writeJSONAtomic(paths.Receipt, receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	server := stubReleaseServer(t, "1.0.0", []byte("binary"), false)
	oldBase := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = oldBase })
	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })
	if _, err := Update(context.Background(), UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	receipt, err = loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("explicit update did not install newly detected artifact: %v", err)
	}
	if receipt.Artifacts[target].Hash == "" {
		t.Fatal("explicit update did not enroll newly detected artifact")
	}
}

func TestSemverComparePreventsDowngrade(t *testing.T) {
	if got := semverCompare("9.0.0-dev", "1.2.3"); got <= 0 {
		t.Fatalf("comparison = %d, want newer local build", got)
	}
}

func TestParseChecksum(t *testing.T) {
	sum, ok := parseChecksum("abc123  mimir_1.0.0_linux_amd64.tar.gz\ndef456  checksums.txt\n", "mimir_1.0.0_linux_amd64.tar.gz")
	if !ok || sum != "abc123" {
		t.Fatalf("sum %q ok %v", sum, ok)
	}
	if _, ok := parseChecksum("abc123  other.tar.gz\n", "mimir_1.0.0_linux_amd64.tar.gz"); ok {
		t.Fatal("matched wrong asset")
	}
}

func TestManagedByPackageManager(t *testing.T) {
	for path, want := range map[string]bool{
		"/opt/homebrew/bin/mimir":              true,
		"/home/linuxbrew/.linuxbrew/bin/mimir": true,
		`C:\Users\me\scoop\shims\mimir.exe`:    true,
		"/nix/store/abc-mimir/bin/mimir":       true,
		"/usr/local/bin/mimir":                 false,
		`C:\Tools\mimir.exe`:                   false,
	} {
		if got := managedByPackageManager(path); got != want {
			t.Fatalf("managedByPackageManager(%q) = %v, want %v", path, got, want)
		}
	}
}

func ownExecutable(t *testing.T, target string, data []byte) {
	t.Helper()
	if err := os.WriteFile(target, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := syncInstallArtifacts(installReceiptUpdate{Source: "test", Method: "bootstrap-copy", CLI: installReceiptCLI{Path: target, Version: "1.0.0", Hash: hashBytes(data)}}); err != nil {
		t.Fatal(err)
	}
}

func stubSiblings(t *testing.T, pids []int) {
	t.Helper()
	old := siblingMimirProcesses
	siblingMimirProcesses = func(int, string) ([]int, error) { return pids, nil }
	t.Cleanup(func() { siblingMimirProcesses = old })
}

func writePending(t *testing.T, pending pendingUpdate) {
	t.Helper()
	paths, err := managedInstallationPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := savePendingUpdate(paths, pending); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending.Staged, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizePendingUpdateAppliesStagedSwap(t *testing.T) {
	isolatedInstallation(t, false)
	target := filepath.Join(t.TempDir(), "mimir")
	ownExecutable(t, target, []byte("old-binary"))
	writePending(t, pendingUpdate{
		Schema: 1, Target: target, Staged: filepath.Join(filepath.Dir(target), ".mimir-update-pending-test"),
		PreviousHash: hashBytes([]byte("old-binary")), NewHash: hashBytes([]byte("new-binary")), Version: "9.9.9",
	})

	applied, pendingVersion, err := finalizePendingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !applied || pendingVersion != "9.9.9" {
		t.Fatalf("applied=%v version=%q", applied, pendingVersion)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-binary" {
		t.Fatalf("binary %q", data)
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.CLI.Hash != hashBytes([]byte("new-binary")) || receipt.CLI.Version != "9.9.9" {
		t.Fatalf("receipt %#v", receipt.CLI)
	}
	paths, _ := managedInstallationPaths()
	if _, found, err := loadPendingUpdate(paths); err != nil || found {
		t.Fatalf("pending marker survived finalize: found=%v err=%v", found, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(target), ".mimir-update-pending-test")); !os.IsNotExist(err) {
		t.Fatalf("staged binary survived finalize: %v", err)
	}
}

func TestFinalizePendingUpdateCompletesHelperSwap(t *testing.T) {
	isolatedInstallation(t, false)
	target := filepath.Join(t.TempDir(), "mimir")
	// The detached helper already swapped the binary; the receipt still
	// points at the previous hash and only the marker is left to reconcile.
	if err := os.WriteFile(target, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := syncInstallArtifacts(installReceiptUpdate{Source: "test", Method: "bootstrap-copy", CLI: installReceiptCLI{Path: target, Version: "1.0.0", Hash: hashBytes([]byte("old-binary"))}}); err != nil {
		t.Fatal(err)
	}
	writePending(t, pendingUpdate{
		Schema: 1, Target: target, Staged: filepath.Join(filepath.Dir(target), ".mimir-update-pending-test"),
		PreviousHash: hashBytes([]byte("old-binary")), NewHash: hashBytes([]byte("new-binary")), Version: "9.9.9",
	})

	applied, _, err := finalizePendingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("helper-completed swap was not finalized")
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.CLI.Hash != hashBytes([]byte("new-binary")) || receipt.CLI.Version != "9.9.9" {
		t.Fatalf("receipt %#v", receipt.CLI)
	}
}

func TestFinalizePendingUpdateRejectsExternallyChangedBinary(t *testing.T) {
	isolatedInstallation(t, false)
	target := filepath.Join(t.TempDir(), "mimir")
	ownExecutable(t, target, []byte("external-replacement"))
	writePending(t, pendingUpdate{
		Schema: 1, Target: target, Staged: filepath.Join(filepath.Dir(target), ".mimir-update-pending-test"),
		PreviousHash: hashBytes([]byte("old-binary")), NewHash: hashBytes([]byte("new-binary")), Version: "9.9.9",
	})

	applied, _, err := finalizePendingUpdate()
	if err == nil || applied {
		t.Fatal("externally changed binary was treated as updated")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "external-replacement" {
		t.Fatalf("external binary changed: %q", data)
	}
	paths, _ := managedInstallationPaths()
	if _, found, _ := loadPendingUpdate(paths); !found {
		t.Fatal("diagnostic marker was removed for an externally changed binary")
	}
}

func TestFinalizePendingUpdateRejectsForgedTarget(t *testing.T) {
	isolatedInstallation(t, false)
	owned := filepath.Join(t.TempDir(), "mimir")
	ownExecutable(t, owned, []byte("old-binary"))
	otherDir := t.TempDir()
	target := filepath.Join(otherDir, "mimir")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePending(t, pendingUpdate{
		Schema: 1, Target: target, Staged: filepath.Join(otherDir, ".mimir-update-pending-forged"),
		PreviousHash: hashBytes([]byte("old-binary")), NewHash: hashBytes([]byte("new-binary")), Version: "9.9.9",
	})
	if applied, _, err := finalizePendingUpdate(); err == nil || applied {
		t.Fatalf("forged target applied=%v err=%v", applied, err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "old-binary" {
		t.Fatalf("forged target changed: %q", data)
	}
}

func TestFinalizePendingUpdateRejectsTamperedStagedBinary(t *testing.T) {
	isolatedInstallation(t, false)
	target := filepath.Join(t.TempDir(), "mimir")
	ownExecutable(t, target, []byte("old-binary"))
	paths, err := managedInstallationPaths()
	if err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(filepath.Dir(target), ".mimir-update-pending-tampered")
	pending := pendingUpdate{
		Schema: 1, Target: target, Staged: staged,
		PreviousHash: hashBytes([]byte("old-binary")), NewHash: hashBytes([]byte("new-binary")), Version: "9.9.9",
	}
	if err := savePendingUpdate(paths, pending); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if applied, _, err := finalizePendingUpdate(); err == nil || applied {
		t.Fatalf("tampered staged binary applied=%v err=%v", applied, err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "old-binary" {
		t.Fatalf("target changed after staged tampering: %q", data)
	}
}

func TestFinalizePendingUpdateReportsMalformedMarker(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if err := os.MkdirAll(paths.MimirHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pendingUpdatePath(paths), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if applied, _, err := finalizePendingUpdate(); err == nil || applied {
		t.Fatalf("malformed marker applied=%v err=%v", applied, err)
	}
}

func TestUpdateSurfacesMalformedPendingMarker(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if err := os.MkdirAll(paths.MimirHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pendingUpdatePath(paths), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(context.Background(), UpdateOptions{}); err == nil || !strings.Contains(err.Error(), "finalizing pending update") {
		t.Fatalf("error = %v", err)
	}
}

func TestCleanupStaleSwapArtifacts(t *testing.T) {
	isolatedInstallation(t, false)
	dir := t.TempDir()
	target := filepath.Join(dir, "mimir")
	ownExecutable(t, target, []byte("binary"))
	removed := []string{target + ".old", target + ".rollback"}
	for _, path := range removed {
		if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldStaged := filepath.Join(dir, ".mimir-update-123")
	if err := os.WriteFile(oldStaged, []byte("staged"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldStaged, past, past); err != nil {
		t.Fatal(err)
	}
	freshStaged := filepath.Join(dir, ".mimir-update-fresh")
	if err := os.WriteFile(freshStaged, []byte("staged"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, "mimir.bak")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreignNew := target + ".new"
	if err := os.WriteFile(foreignNew, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanupStaleSwapArtifacts(target)

	for _, path := range append(removed, oldStaged) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale artifact %s survived cleanup", filepath.Base(path))
		}
	}
	for _, path := range []string{freshStaged, foreign, foreignNew} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s was removed: %v", filepath.Base(path), err)
		}
	}
}

func TestCleanupStaleSwapArtifactsRequiresReceiptOwnership(t *testing.T) {
	isolatedInstallation(t, false)
	target := filepath.Join(t.TempDir(), "mimir")
	if err := os.WriteFile(target, []byte("unowned"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := target + ".old"
	if err := os.WriteFile(old, []byte("preserve"), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanupStaleSwapArtifacts(target)
	if data, err := os.ReadFile(old); err != nil || string(data) != "preserve" {
		t.Fatalf("unowned artifact changed: %q %v", data, err)
	}
}
