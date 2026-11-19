package mimirassets

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

// Bundle contains the production integrations, skills, Worker sources, and
// compiled dashboard shipped with the CLI. Tests, dashboard build inputs,
// Wrangler state, and dependency directories are deliberately not embedded.
//
//go:embed plugins/pi/mimir.ts plugins/oh-my-pi/mimir.ts plugins/opencode/mimir.ts plugins/hermes/__init__.py plugins/hermes/plugin.yaml
//go:embed plugins/claude-code/.claude-plugin/plugin.json plugins/claude-code/hooks/hooks.json plugins/codex/plugin.json plugins/codex/marketplace.json plugins/codex/hooks/hooks.json plugins/cursor/hooks.json
//go:embed skills/mimir-setup skills/mimir-use
//go:embed worker/src/app.ts worker/src/env.ts worker/src/index.ts worker/src/maintenance.ts
//go:embed worker/src/auth/auth-middleware.ts
//go:embed worker/src/config/config-routes.ts worker/src/config/config-store.ts
//go:embed worker/src/dashboard/dashboard-routes.ts
//go:embed worker/src/days/day-snapshot.ts
//go:embed worker/src/exchanges/archives.ts worker/src/exchanges/capture-pipeline.ts worker/src/exchanges/evidence.ts worker/src/exchanges/exchange-types.ts worker/src/exchanges/redaction.ts worker/src/exchanges/reported-exchange-routes.ts worker/src/exchanges/reported-exchange-schema.ts worker/src/exchanges/response-codec.ts worker/src/exchanges/tool-names.ts worker/src/exchanges/transcript.ts
//go:embed worker/src/gateway/openrouter-routes.ts worker/src/gateway/upstream-proxy.ts
//go:embed worker/src/integrations/integration-routes.ts
//go:embed worker/src/machines/machine-routes.ts
//go:embed worker/src/mcp/mcp-routes.ts
//go:embed worker/src/queries/commits.ts worker/src/queries/days.ts worker/src/queries/devices.ts worker/src/queries/query-context.ts worker/src/queries/query-routes.ts worker/src/queries/search.ts worker/src/queries/session-records.ts worker/src/queries/sessions.ts worker/src/queries/stats.ts worker/src/queries/turns.ts
//go:embed worker/src/search/search-index.ts
//go:embed worker/src/sessions/capture-status.ts worker/src/sessions/events.ts worker/src/sessions/git-artifacts.ts worker/src/sessions/git-repository.ts worker/src/sessions/lifecycle.ts worker/src/sessions/session-routes.ts worker/src/sessions/session-tree.ts worker/src/sessions/summaries.ts worker/src/sessions/titles.ts
//go:embed worker/src/shared/cursors.ts worker/src/shared/date-range.ts worker/src/shared/query-error.ts worker/src/shared/ulid.ts
//go:embed worker/migrations
//go:embed worker/package.json worker/package-lock.json worker/tsconfig.json worker/worker-configuration.d.ts worker/wrangler.jsonc
//go:embed worker/web/dist
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
