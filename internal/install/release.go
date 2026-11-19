package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// InstallScriptURL and InstallPowerShellURL are the canonical installers.
	InstallScriptURL     = "https://raw.githubusercontent.com/cloudboy-jh/mimir/master/install.sh"
	InstallPowerShellURL = "https://raw.githubusercontent.com/cloudboy-jh/mimir/master/install.ps1"
	releaseRepository    = "cloudboy-jh/mimir"
)

var releaseClient = &http.Client{Timeout: 30 * time.Second}

// LatestRelease returns the latest stable release version without a leading v.
// MIMIR_GITHUB_API_URL overrides the GitHub API base, as in the installers.
func LatestRelease(ctx context.Context) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("MIMIR_GITHUB_API_URL")), "/")
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/repos/"+releaseRepository+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("user-agent", "mimir-cli")
	res, err := releaseClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("checking for updates: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("checking for updates: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return "", fmt.Errorf("checking for updates: GET %s: %s", req.URL, res.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &release); err != nil || release.TagName == "" {
		return "", fmt.Errorf("checking for updates: invalid GitHub response")
	}
	return strings.TrimPrefix(release.TagName, "v"), nil
}

// CompareVersions compares two x.y.z versions; unparseable versions compare equal.
func CompareVersions(left, right string) int {
	var l, r [3]int
	if _, err := fmt.Sscanf(strings.TrimPrefix(left, "v"), "%d.%d.%d", &l[0], &l[1], &l[2]); err != nil {
		return 0
	}
	if _, err := fmt.Sscanf(strings.TrimPrefix(right, "v"), "%d.%d.%d", &r[0], &r[1], &r[2]); err != nil {
		return 0
	}
	for i := range l {
		if l[i] != r[i] {
			if l[i] < r[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
