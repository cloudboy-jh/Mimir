package mimirassets

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

// LogoPNG is the Mimir wordmark used by interactive setup.
//
//go:embed assets/images/mimir-cliimage.png
var LogoPNG []byte

// Bundle contains the production integrations, skills, Worker sources, and
// dashboard inputs shipped with the CLI. Tests, generated output, Wrangler
// state, and dependency directories are deliberately not embedded.
//
//go:embed plugins/opencode/mimir.ts plugins/hermes/__init__.py plugins/hermes/plugin.yaml
//go:embed skills/mimir-setup skills/mimir-use
//go:embed worker/src/app.ts worker/src/auth.ts worker/src/capture.ts worker/src/config.ts worker/src/index.ts worker/src/proxy.ts worker/src/session-events.ts worker/src/session-object.ts worker/src/sessions.ts worker/src/storage.ts worker/src/types.ts worker/src/routes/dashboard.ts worker/src/routes/machine.ts
//go:embed worker/migrations
//go:embed worker/package.json worker/package-lock.json worker/tsconfig.json worker/worker-configuration.d.ts worker/wrangler.jsonc
//go:embed worker/web/src worker/web/public worker/web/bun.lock worker/web/index.html worker/web/package.json worker/web/tsconfig.json worker/web/vite.config.ts
//go:embed assets/images/mimir-readme.png assets/images/mimir-favicon-32.png assets/images/mimir-favicon-180.png
var Bundle embed.FS

// BundleFile describes an embedded file. SHA256 is calculated from the bytes
// in the compiled bundle rather than from release metadata.
type BundleFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// BundleMetadata returns stable, path-sorted metadata for every bundled file.
func BundleMetadata() ([]BundleFile, error) {
	files := make([]BundleFile, 0)
	err := fs.WalkDir(Bundle, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := Bundle.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		files = append(files, BundleFile{Path: path, SHA256: fmt.Sprintf("%x", sum), Size: int64(len(data))})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
