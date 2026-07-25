package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/jsonconfig"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

var (
	version = "0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

func SetBuildInfo(info BuildInfo) {
	version, commit, date = info.Version, info.Commit, info.Date
}

func pointerPath() (string, error) { return mimirapi.ConfigPath() }

func sameFilePath(left, right string) bool {
	left, _ = filepath.Abs(left)
	right, _ = filepath.Abs(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runCommand(ctx context.Context, dir string, stdin io.Reader, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Stdin = dir, stdin
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}

func updateWranglerVars(path string, vars map[string]string) error {
	return jsonconfig.UpdateVars(path, vars)
}
