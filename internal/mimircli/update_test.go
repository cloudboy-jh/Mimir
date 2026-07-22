package mimircli

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

func TestCmdUpdateInstallsVerifiedBinary(t *testing.T) {
	server := stubReleaseServer(t, "9.9.9", []byte("new-binary"), false)
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
	oldInstaller := runUpdatedInstaller
	runUpdatedInstaller = func(context.Context, string) (harnessIntegrationReport, error) {
		return harnessIntegrationReport{}, nil
	}
	t.Cleanup(func() { runUpdatedInstaller = oldInstaller })

	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })

	var out strings.Builder
	if err := cmdUpdate(context.Background(), nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "updated mimir 1.0.0 → 9.9.9") {
		t.Fatalf("output %q", out.String())
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new-binary" {
		t.Fatalf("binary %q", contents)
	}
}

func TestCmdUpdateRejectsChecksumMismatch(t *testing.T) {
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

	var out strings.Builder
	if err := cmdUpdate(context.Background(), nil, &out); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error %v", err)
	}
	contents, _ := os.ReadFile(target)
	if string(contents) != "old-binary" {
		t.Fatalf("binary replaced despite mismatch: %q", contents)
	}
}

func TestCmdUpdateCheckAndCurrent(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	server := stubReleaseServer(t, "1.0.0", []byte("binary"), false)
	oldBase := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })

	var out strings.Builder
	if err := cmdUpdate(context.Background(), nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Fatalf("output %q", out.String())
	}

	version = "0.9.0"
	out.Reset()
	if err := cmdUpdate(context.Background(), []string{"--check"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1.0.0 available (current 0.9.0)") {
		t.Fatalf("check output %q", out.String())
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
