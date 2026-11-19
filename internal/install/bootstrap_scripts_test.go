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
			isolatedInstallation(t, false)
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
			manifest, err := LoadManifest()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(manifest.Harnesses, ",") != "opencode" || manifest.CLI.Path != installed {
				t.Fatalf("explicit bootstrap selection missing from managed.json: %#v", manifest)
			}
			if test.name == "latest stable" {
				testInstalledLifecycleTranscripts(t, installed, installDir)
			}
		})
	}
}

func TestBootstrapScriptsForwardHarnessSelectionWithoutDefaultingToAll(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"install.sh", "install.ps1"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "--harness all") {
			t.Fatalf("%s unconditionally selects all harnesses", name)
		}
		if !strings.Contains(strings.ToLower(text), "harness") || !strings.Contains(text, "--harness") {
			t.Fatalf("%s does not forward explicit harness choices", name)
		}
	}
}

func testInstalledLifecycleTranscripts(t *testing.T, installed, installDir string) {
	t.Helper()
	humanInstall := runInstalledCommand(t, installed, "install", "--harness", "all", "--bin-dir", installDir)
	if humanInstall.exitCode != 0 || humanInstall.stderr != "" || !strings.Contains(humanInstall.stdout, "already installed") {
		t.Fatalf("human install = %#v", humanInstall)
	}
	jsonInstall := runInstalledCommand(t, installed, "install", "--harness=all", "--bin-dir", installDir, "--json")
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
		return exec.Command(shell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(root, "install.ps1"), "-Harness", "opencode").CombinedOutput()
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not available")
	}
	return exec.Command(shell, filepath.Join(root, "install.sh"), "--harness", "opencode").CombinedOutput()
}
