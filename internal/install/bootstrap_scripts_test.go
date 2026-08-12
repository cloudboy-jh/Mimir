package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseBootstrapScripts(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	version := "9.8.7"
	osName, arch := releasePlatform(t)
	name := "mimir"
	archiveName := fmt.Sprintf("mimir_%s_%s_%s.tar.gz", version, osName, arch)
	if runtime.GOOS == "windows" {
		name += ".exe"
		archiveName = fmt.Sprintf("mimir_%s_windows_%s.zip", version, arch)
	}
	binary := filepath.Join(t.TempDir(), name)
	command := exec.Command("go", "build", "-o", binary, "./cmd/mimir")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("building fixture Mimir: %v\n%s", err, output)
	}
	archive := releaseArchive(t, binary, archiveName, version, osName, arch)
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%x  %s\n", sum, archiveName)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/api/repos/cloudboy-jh/mimir/releases/latest":
			response.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(response, `{"tag_name":"v%s","prerelease":false,"draft":false}`, version)
		case request.URL.Path == "/releases/latest":
			http.Redirect(response, request, "/releases/tag/v"+version, http.StatusFound)
		case request.URL.Path == "/releases/tag/v"+version:
			response.WriteHeader(http.StatusOK)
		case strings.HasSuffix(request.URL.Path, "/"+archiveName):
			response.Header().Set("Content-Type", "application/octet-stream")
			_, _ = response.Write(archive)
		case strings.HasSuffix(request.URL.Path, "/checksums.txt"):
			if strings.Contains(request.URL.Path, "/bad/") {
				fmt.Fprintf(response, "%064d  %s\n", 0, archiveName)
				return
			}
			fmt.Fprint(response, checksums)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	for _, test := range []struct {
		name        string
		pinned      bool
		badChecksum bool
		wantError   bool
	}{
		{name: "latest stable"},
		{name: "pinned version", pinned: true},
		{name: "checksum mismatch", pinned: true, badChecksum: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := isolatedInstallation(t, false)
			installDir := filepath.Join(t.TempDir(), "bin")
			t.Setenv("MIMIR_INSTALL_DIR", installDir)
			t.Setenv("MIMIR_GITHUB_API_URL", server.URL+"/api")
			releases := server.URL + "/releases"
			if test.badChecksum {
				releases = server.URL + "/bad/releases"
			}
			t.Setenv("MIMIR_RELEASES_URL", releases)
			if test.pinned {
				t.Setenv("MIMIR_VERSION", version)
			} else {
				t.Setenv("MIMIR_VERSION", "")
			}

			output, runErr := runBootstrapScript(t, root)
			if test.wantError {
				if runErr == nil || !strings.Contains(string(output), "checksum mismatch") {
					t.Fatalf("error=%v output=%s", runErr, output)
				}
				if _, err := os.Stat(filepath.Join(installDir, name)); !os.IsNotExist(err) {
					t.Fatalf("binary exists after rejected checksum: %v", err)
				}
				return
			}
			if runErr != nil {
				t.Fatalf("bootstrap failed: %v\n%s", runErr, output)
			}
			installed := filepath.Join(installDir, name)
			if info, err := os.Stat(installed); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("installed binary: %v, %v", info, err)
			}
			receiptData, err := os.ReadFile(paths.Receipt)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(receiptData, []byte(`"source": "release"`)) {
				t.Fatalf("release source missing from receipt:\n%s", receiptData)
			}
			if test.name == "latest stable" {
				testInstalledLifecycleTranscripts(t, installed, installDir)
				testInstalledDeployTranscripts(t, installed, paths)
			}
		})
	}
}

func testInstalledLifecycleTranscripts(t *testing.T, installed, installDir string) {
	t.Helper()
	humanInstall := runInstalledCommand(t, installed, "install", "--bin-dir", installDir)
	if humanInstall.exitCode != 0 || humanInstall.stderr != "" || !strings.Contains(humanInstall.stdout, "==> Installing Mimir") || !strings.Contains(humanInstall.stdout, "already installed") {
		t.Fatalf("human install = %#v", humanInstall)
	}
	jsonInstall := runInstalledCommand(t, installed, "install", "--bin-dir", installDir, "--json")
	assertSingleJSONTranscript(t, "json install", jsonInstall, false)

	humanUpdate := runInstalledCommand(t, installed, "update", "--check")
	if humanUpdate.exitCode != 0 || humanUpdate.stderr != "" || !strings.Contains(humanUpdate.stdout, "Mimir update available") {
		t.Fatalf("human update = %#v", humanUpdate)
	}
	jsonUpdate := runInstalledCommand(t, installed, "update", "--check", "--json")
	assertSingleJSONTranscript(t, "json update", jsonUpdate, false)

	humanDoctor := runInstalledCommand(t, installed, "doctor")
	if humanDoctor.exitCode != 0 || humanDoctor.stderr != "" || !strings.Contains(humanDoctor.stdout, "Mimir doctor") {
		t.Fatalf("human doctor = %#v", humanDoctor)
	}
	jsonDoctor := runInstalledCommand(t, installed, "doctor", "--json")
	assertSingleJSONTranscript(t, "json doctor", jsonDoctor, false)
}

func assertSingleJSONTranscript(t *testing.T, label string, transcript commandTranscript, fromStderr bool) {
	t.Helper()
	if transcript.exitCode != 0 {
		t.Fatalf("%s exit = %#v", label, transcript)
	}
	content := transcript.stdout
	other := transcript.stderr
	if fromStderr {
		content, other = transcript.stderr, transcript.stdout
	}
	if other != "" {
		t.Fatalf("%s contaminated secondary stream: %#v", label, transcript)
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	var document map[string]any
	if err := decoder.Decode(&document); err != nil || len(document) == 0 {
		t.Fatalf("%s document=%#v error=%v", label, document, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("%s contains extra output: %v", label, err)
	}
}

type commandTranscript struct {
	stdout, stderr string
	exitCode       int
}

func testInstalledDeployTranscripts(t *testing.T, installed string, paths installationPaths) {
	t.Helper()
	worker, err := WorkerDir("")
	if err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(worker, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(t.TempDir(), "deploy-count")
	t.Setenv("MIMIR_FAKE_DEPLOY_COUNT", counter)
	writeFakeTranscriptWrangler(t, binDir)
	hash, err := workerDependencyHash(worker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worker, ".mimir-dependencies"), []byte(hash+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	failure := runInstalledCommand(t, installed, "deploy")
	if failure.exitCode != 4 || !strings.Contains(failure.stdout, "==> Deployment resources selected") || !strings.Contains(failure.stderr, "fixture wrangler: deploy failed") {
		t.Fatalf("human failure = %#v", failure)
	}
	if _, err := os.Stat(filepath.Join(paths.MimirHome, "cloudflare-deployment.json")); !os.IsNotExist(err) {
		t.Fatalf("failed deploy saved state: %v", err)
	}
	success := runInstalledCommand(t, installed, "deploy")
	if success.exitCode != 0 || success.stderr != "" || !strings.Contains(success.stdout, "OK  Deployment complete: https://mimir.example.workers.dev") {
		t.Fatalf("human success = %#v", success)
	}
	state, err := os.ReadFile(filepath.Join(paths.MimirHome, "cloudflare-deployment.json"))
	if err != nil || !bytes.Contains(state, []byte(`"account_id":"account-1"`)) || !bytes.Contains(state, []byte(`"database_id":"123e4567-e89b-12d3-a456-426614174000"`)) {
		t.Fatalf("deployment state error=%v data=%s", err, state)
	}

	if err := os.Remove(counter); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	jsonFailure := runInstalledCommand(t, installed, "deploy", "--json")
	if jsonFailure.exitCode != 4 || jsonFailure.stdout != "" {
		t.Fatalf("json failure = %#v", jsonFailure)
	}
	var failureDocument struct {
		Error    string `json:"error"`
		ExitCode int    `json:"exit_code"`
	}
	decoder := json.NewDecoder(strings.NewReader(jsonFailure.stderr))
	if err := decoder.Decode(&failureDocument); err != nil || failureDocument.ExitCode != 4 || !strings.Contains(failureDocument.Error, "fixture wrangler: deploy failed") {
		t.Fatalf("json failure document=%#v error=%v", failureDocument, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("json failure contains extra output: %v", err)
	}
	jsonSuccess := runInstalledCommand(t, installed, "deploy", "--json")
	if jsonSuccess.exitCode != 0 || jsonSuccess.stderr != "" {
		t.Fatalf("json success = %#v", jsonSuccess)
	}
	var successDocument map[string]any
	decoder = json.NewDecoder(strings.NewReader(jsonSuccess.stdout))
	if err := decoder.Decode(&successDocument); err != nil || successDocument["state"] != "deployed" || successDocument["url"] != "https://mimir.example.workers.dev" {
		t.Fatalf("json success document=%#v error=%v", successDocument, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("json success contains extra output: %v", err)
	}
}

func runInstalledCommand(t *testing.T, installed string, args ...string) commandTranscript {
	t.Helper()
	command := exec.Command(installed, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		exitCode = exit.ExitCode()
	}
	return commandTranscript{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

func writeFakeTranscriptWrangler(t *testing.T, binDir string) {
	t.Helper()
	path := filepath.Join(binDir, "wrangler")
	script := `#!/bin/sh
if [ "$1" = "whoami" ] && [ "$2" = "--json" ]; then printf '%s' '{"loggedIn":true,"accounts":[{"id":"account-1","name":"Fixture"}]}'; exit 0; fi
if [ "$1" = "whoami" ]; then exit 0; fi
if [ "$1" = "d1" ] && [ "$2" = "list" ]; then printf '%s' '[{"uuid":"123e4567-e89b-12d3-a456-426614174000","name":"mimir"}]'; exit 0; fi
if [ "$1" = "d1" ]; then exit 0; fi
if [ "$1" = "deploy" ]; then
  count=0; [ -f "$MIMIR_FAKE_DEPLOY_COUNT" ] && count=$(cat "$MIMIR_FAKE_DEPLOY_COUNT")
  count=$((count + 1)); printf '%s' "$count" > "$MIMIR_FAKE_DEPLOY_COUNT"
  if [ "$count" = "1" ]; then printf '%s' 'fixture wrangler: deploy failed' >&2; exit 17; fi
  printf '%s' 'Deployed to https://mimir.example.workers.dev'; exit 0
fi
exit 99
`
	if runtime.GOOS == "windows" {
		path += ".cmd"
		script = "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0wrangler.ps1\" %*\r\nexit /b %errorlevel%\r\n"
		powerShell := `param([Parameter(ValueFromRemainingArguments=$true)][string[]]$Rest)
if ($Rest[0] -eq 'whoami' -and $Rest[1] -eq '--json') { [Console]::Out.Write('{"loggedIn":true,"accounts":[{"id":"account-1","name":"Fixture"}]}'); exit 0 }
if ($Rest[0] -eq 'whoami') { exit 0 }
if ($Rest[0] -eq 'd1' -and $Rest[1] -eq 'list') { [Console]::Out.Write('[{"uuid":"123e4567-e89b-12d3-a456-426614174000","name":"mimir"}]'); exit 0 }
if ($Rest[0] -eq 'd1') { exit 0 }
if ($Rest[0] -eq 'deploy') {
  if (-not [IO.File]::Exists($env:MIMIR_FAKE_DEPLOY_COUNT)) {
    [IO.File]::WriteAllText($env:MIMIR_FAKE_DEPLOY_COUNT, 'failed')
    [Console]::Error.Write('fixture wrangler: deploy failed'); exit 17
  }
  [Console]::Out.Write('Deployed to https://mimir.example.workers.dev'); exit 0
}
exit 99
`
		if err := os.WriteFile(filepath.Join(binDir, "wrangler.ps1"), []byte(powerShell), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func releasePlatform(t *testing.T) (string, string) {
	t.Helper()
	osName := runtime.GOOS
	if osName != "darwin" && osName != "linux" && osName != "windows" {
		t.Skipf("bootstrap scripts do not support %s", osName)
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		t.Skipf("bootstrap scripts do not support %s", arch)
	}
	return osName, arch
}

func releaseArchive(t *testing.T, binaryPath, archiveName, version, osName, arch string) []byte {
	t.Helper()
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := fmt.Sprintf("mimir_%s_%s_%s", version, osName, arch)
	var buffer bytes.Buffer
	if strings.HasSuffix(archiveName, ".zip") {
		writer := zip.NewWriter(&buffer)
		header := &zip.FileHeader{Name: wrapped + "/mimir.exe", Method: zip.Deflate}
		header.SetMode(0o755)
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(binary); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: wrapped + "/mimir", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func runBootstrapScript(t *testing.T, root string) ([]byte, error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		shell, err := exec.LookPath("pwsh")
		if err != nil {
			shell, err = exec.LookPath("powershell")
		}
		if err != nil {
			t.Skip("PowerShell is not available")
		}
		return exec.Command(shell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(root, "install.ps1")).CombinedOutput()
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not available")
	}
	return exec.Command(shell, filepath.Join(root, "install.sh")).CombinedOutput()
}
