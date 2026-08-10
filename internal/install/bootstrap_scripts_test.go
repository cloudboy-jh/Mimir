package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
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
		})
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
