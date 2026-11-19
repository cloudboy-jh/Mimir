package install

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	mimirassets "github.com/cloudboy-jh/mimir"
)

type BundleIdentity struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

func EmbeddedWorkerIdentity() (BundleIdentity, error) {
	metadata, err := mimirassets.BundleMetadata()
	if err != nil {
		return BundleIdentity{}, err
	}
	hash := sha256.New()
	for _, file := range metadata {
		if !strings.HasPrefix(file.Path, "worker/") && !strings.HasPrefix(file.Path, "assets/images/") {
			continue
		}
		_, _ = fmt.Fprintf(hash, "%s\x00%s\n", file.Path, file.SHA256)
	}
	return BundleIdentity{Version: version, SHA256: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}

func WorkerDir(explicit string) (string, error)       { return workerDir(explicit) }
func MaterializeWorker(source string) (string, error) { return materializeWorker(source) }
func EnsureWorkerDependencies(ctx context.Context, dir string) error {
	return ensureWorkerDependencies(ctx, dir)
}
func EnsureDashboardDependencies(ctx context.Context, dir string) error {
	return ensureDashboardDependencies(ctx, dir)
}
func BuildDashboard(ctx context.Context, dir string) error { return buildDashboard(ctx, dir) }
