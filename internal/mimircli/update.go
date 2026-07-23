package mimircli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const updateRepo = "cloudboy-jh/mimir"

// githubAPIBase is a variable so tests can point at a stub server.
var githubAPIBase = "https://api.github.com"

// downloadClient allows large release assets over slow connections; the
// shared httpClient stays tuned for quick API calls.
var downloadClient = &http.Client{Timeout: 5 * time.Minute}

// executablePath is a variable so tests can point updates at a temp binary.
var executablePath = os.Executable

var runUpdatedInstaller = func(ctx context.Context, executable string) (lifecycleIntegrationReport, error) {
	command := exec.CommandContext(ctx, executable, "_post-update")
	output, err := command.CombinedOutput()
	if err != nil {
		return lifecycleIntegrationReport{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	var report lifecycleIntegrationReport
	if err := json.Unmarshal(output, &report); err != nil {
		return lifecycleIntegrationReport{}, fmt.Errorf("reading updated integration report: %w", err)
	}
	if !report.OK {
		return report, fmt.Errorf("%s", report.Error)
	}
	return report, nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (r githubRelease) asset(name string) (string, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset.URL, true
		}
	}
	return "", false
}

func cmdUpdate(ctx context.Context, args []string, out io.Writer) error {
	check, jsonOutput := false, false
	for _, arg := range args {
		switch arg {
		case "--check":
			check = true
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("usage: mimir update [--check] [--json]")
		}
	}
	artifacts, err := checkManagedArtifacts()
	if err != nil {
		return err
	}
	release, err := fetchLatestRelease(ctx)
	if err != nil {
		return err
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(version, "v")
	binaryStatus := "available"
	if latest == current || semverCompare(current, latest) > 0 {
		binaryStatus = "current"
		var lifecycle lifecycleIntegrationReport
		if !check {
			lifecycle = refreshLifecycleIntegrations(ctx, "update")
			artifacts = lifecycle.Artifacts
			if !lifecycle.OK {
				return fmt.Errorf("mimir is current, but %s", lifecycle.Error)
			}
		}
		report := newUpdateReport(check, binaryStatus, current, latest, artifacts, lifecycle.Integrations)
		if jsonOutput {
			return json.NewEncoder(out).Encode(report)
		}
		message := fmt.Sprintf("mimir %s is up to date", current)
		message += "\n" + artifactSummary(artifacts)
		if summary := integrationSummary(lifecycle.Integrations); summary != "" {
			message += "\n" + summary
		}
		_, err := fmt.Fprintln(out, message)
		return err
	}
	if check {
		report := newUpdateReport(true, binaryStatus, current, latest, artifacts, harnessIntegrationReport{})
		if jsonOutput {
			return json.NewEncoder(out).Encode(report)
		}
		_, err := fmt.Fprintf(out, "mimir %s available (current %s)\n%s\n", latest, current, artifactSummary(artifacts))
		return err
	}
	assetName := releaseAssetName(latest, runtime.GOOS, runtime.GOARCH)
	assetURL, ok := release.asset(assetName)
	if !ok {
		return fmt.Errorf("release %s has no asset %s", release.TagName, assetName)
	}
	checksumsURL, ok := release.asset("checksums.txt")
	if !ok {
		return fmt.Errorf("release %s has no checksums.txt", release.TagName)
	}
	checksums, err := download(ctx, checksumsURL)
	if err != nil {
		return err
	}
	want, ok := parseChecksum(string(checksums), assetName)
	if !ok {
		return fmt.Errorf("checksums.txt has no entry for %s", assetName)
	}
	archive, err := download(ctx, assetURL)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(archive); !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return fmt.Errorf("checksum mismatch for %s; aborting update", assetName)
	}
	binary, err := extractBinary(archive, runtime.GOOS)
	if err != nil {
		return err
	}
	target, err := executablePath()
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to update symlinked executable path %s", target)
	}
	if managedByPackageManager(target) {
		return fmt.Errorf("mimir at %s is managed by a package manager; update through it instead", target)
	}
	if err := installBinary(target, binary); err != nil {
		return fmt.Errorf("installing update: %w", err)
	}
	lifecycle, err := runUpdatedInstaller(ctx, target)
	if err != nil {
		return fmt.Errorf("mimir updated, but refreshing managed artifacts failed: %w", err)
	}
	artifacts = lifecycle.Artifacts
	report := newUpdateReport(false, "updated", current, latest, artifacts, lifecycle.Integrations)
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	message := fmt.Sprintf("updated mimir %s → %s", current, latest)
	message += "\n" + artifactSummary(artifacts)
	if summary := integrationSummary(lifecycle.Integrations); summary != "" {
		message += "\n" + summary
	}
	_, err = fmt.Fprintln(out, message)
	return err
}

type updateReport struct {
	Check        bool                     `json:"check"`
	Binary       updateBinaryReport       `json:"binary"`
	Artifacts    managedArtifactReport    `json:"artifacts"`
	Integrations harnessIntegrationReport `json:"integrations,omitempty"`
}

type updateBinaryReport struct {
	Status  string `json:"status"`
	Current string `json:"current_version"`
	Latest  string `json:"latest_version"`
}

func newUpdateReport(check bool, status, current, latest string, artifacts managedArtifactReport, integrations harnessIntegrationReport) updateReport {
	return updateReport{Check: check, Binary: updateBinaryReport{Status: status, Current: current, Latest: latest}, Artifacts: artifacts, Integrations: integrations}
}

func semverCompare(left, right string) int {
	var lmajor, lminor, lpatch, rmajor, rminor, rpatch int
	if _, err := fmt.Sscanf(strings.TrimPrefix(left, "v"), "%d.%d.%d", &lmajor, &lminor, &lpatch); err != nil {
		return 0
	}
	if _, err := fmt.Sscanf(strings.TrimPrefix(right, "v"), "%d.%d.%d", &rmajor, &rminor, &rpatch); err != nil {
		return 0
	}
	for _, pair := range [][2]int{{lmajor, rmajor}, {lminor, rminor}, {lpatch, rpatch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}

func releaseAssetName(version, goos, goarch string) string {
	format := "tar.gz"
	if goos == "windows" {
		format = "zip"
	}
	return fmt.Sprintf("mimir_%s_%s_%s.%s", version, goos, goarch, format)
}

func fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", githubAPIBase+"/repos/"+updateRepo+"/releases/latest", nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("user-agent", "mimir-cli")
	data, err := do(httpClient, req)
	if err != nil {
		return githubRelease{}, fmt.Errorf("checking for updates: %w", err)
	}
	var release githubRelease
	if err := json.Unmarshal(data, &release); err != nil || release.TagName == "" {
		return githubRelease{}, fmt.Errorf("checking for updates: invalid GitHub response")
	}
	return release, nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("user-agent", "mimir-cli")
	return do(downloadClient, req)
}

func do(client *http.Client, req *http.Request) ([]byte, error) {
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("GET %s: %s", req.URL, res.Status)
	}
	return data, nil
}

func parseChecksum(checksums, assetName string) (string, bool) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			return fields[0], true
		}
	}
	return "", false
}

func extractBinary(archive []byte, goos string) ([]byte, error) {
	if goos == "windows" {
		return extractZip(archive)
	}
	return extractTarGz(archive)
}

func extractTarGz(archive []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("reading release archive: %w", err)
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading release archive: %w", err)
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == "mimir" {
			return io.ReadAll(tarReader)
		}
	}
	return nil, fmt.Errorf("release archive does not contain the mimir binary")
}

func extractZip(archive []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("reading release archive: %w", err)
	}
	for _, file := range reader.File {
		if filepath.Base(file.Name) != "mimir.exe" {
			continue
		}
		contents, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer contents.Close()
		return io.ReadAll(contents)
	}
	return nil, fmt.Errorf("release archive does not contain mimir.exe")
}

// managedByPackageManager detects installs owned by brew, scoop, chocolatey,
// or nix; replacing those binaries directly would corrupt the manager's
// bookkeeping.
func managedByPackageManager(path string) bool {
	lower := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	for _, marker := range []string{"/homebrew/", "/cellar/", "/linuxbrew/", "/scoop/", "/chocolatey/", "/nix/store/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// installBinary atomically swaps the running binary. On Windows a running
// executable can be renamed but not replaced, so the current binary is moved
// aside first; the leftover is removed on the next update.
func installBinary(target string, binary []byte) error {
	dir := filepath.Dir(target)
	if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to update symlinked executable path %s", target)
	}
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to update non-regular executable path %s", target)
	}
	current, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	expectedHash := hashBytes(current)
	staged, err := os.CreateTemp(dir, ".mimir-update-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if _, err := staged.Write(binary); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to replace symlinked executable path %s", target)
	}
	if err := validateExecutableReplacement(target, expectedHash); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := target + ".old"
		_ = os.Remove(old)
		if err := os.Rename(target, old); err != nil {
			return err
		}
		if err := os.Rename(stagedPath, target); err != nil {
			_ = os.Rename(old, target)
			return err
		}
		_ = os.Remove(old)
		return nil
	}
	return os.Rename(stagedPath, target)
}
