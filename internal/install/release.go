package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

func semverCompareRelease(left, right string) int {
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

func releaseAssetNameForPlatform(version, goos, goarch string) string {
	format := "tar.gz"
	if goos == "windows" {
		format = "zip"
	}
	return fmt.Sprintf("mimir_%s_%s_%s.%s", version, goos, goarch, format)
}

func parseReleaseChecksum(checksums, assetName string) (string, bool) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			return fields[0], true
		}
	}
	return "", false
}

func extractReleaseBinary(archive []byte, goos string) ([]byte, error) {
	if goos == "windows" {
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

func managedByKnownPackageManager(path string) bool {
	lower := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	for _, marker := range []string{"/homebrew/", "/cellar/", "/linuxbrew/", "/scoop/", "/chocolatey/", "/nix/store/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
