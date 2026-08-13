package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Harness struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Selected bool   `json:"selected"`
	Detected bool   `json:"detected"`
}

var canonicalHarnesses = []Harness{
	{ID: "opencode", Name: "OpenCode"},
	{ID: "pi", Name: "Pi"},
	{ID: "hermes", Name: "Hermes"},
	{ID: "claude-code", Name: "Claude Code"},
	{ID: "codex", Name: "Codex"},
	{ID: "cursor", Name: "Cursor"},
}

func CanonicalHarnesses() []Harness {
	return append([]Harness(nil), canonicalHarnesses...)
}

func NormalizeHarnesses(values []string) ([]string, error) {
	requested := map[string]bool{}
	for _, raw := range values {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == "all" {
			for _, harness := range canonicalHarnesses {
				requested[harness.ID] = true
			}
			continue
		}
		valid := false
		for _, harness := range canonicalHarnesses {
			if id == harness.ID {
				valid = true
				requested[id] = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("unknown harness %q (want opencode, pi, hermes, claude-code, codex, cursor, or all)", raw)
		}
	}
	result := make([]string, 0, len(requested))
	for _, harness := range canonicalHarnesses {
		if requested[harness.ID] {
			result = append(result, harness.ID)
		}
	}
	return result, nil
}

func Harnesses() ([]Harness, error) {
	paths, err := managedInstallationPaths()
	if err != nil {
		return nil, err
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		return nil, err
	}
	detected := detectedHarnessSet(paths)
	selected := stringSet(receipt.Harnesses)
	result := CanonicalHarnesses()
	for i := range result {
		result[i].Detected = detected[result[i].ID]
		result[i].Selected = selected[result[i].ID]
	}
	return result, nil
}

func DetectedHarnesses() ([]string, error) {
	paths, err := managedInstallationPaths()
	if err != nil {
		return nil, err
	}
	detected := detectedHarnessSet(paths)
	var result []string
	for _, harness := range canonicalHarnesses {
		if detected[harness.ID] {
			result = append(result, harness.ID)
		}
	}
	return result, nil
}

func detectedHarnessSet(paths installationPaths) map[string]bool {
	result := map[string]bool{"hermes": paths.HermesDetected}
	checks := []struct{ id, home, executable string }{
		{"opencode", paths.OpenCodeHome, "opencode"}, {"pi", paths.PiHome, "pi"},
		{"claude-code", paths.ClaudeCodeHome, "claude"}, {"codex", paths.CodexHome, "codex"},
		{"cursor", paths.CursorHome, "cursor"},
	}
	for _, check := range checks {
		if info, err := os.Stat(check.home); err == nil && info.IsDir() {
			result[check.id] = true
		} else if _, err := exec.LookPath(check.executable); err == nil {
			result[check.id] = true
		}
	}
	return result
}

func legacyHarnesses(paths installationPaths, artifacts map[string]installReceiptArtifact) []string {
	selected := map[string]bool{"opencode": true, "pi": true, "claude-code": true, "codex": true, "cursor": true}
	if paths.HermesDetected {
		selected["hermes"] = true
	}
	for target, artifact := range artifacts {
		if harness := artifactHarness(paths, target, artifact.Source); harness != "" {
			selected[harness] = true
		}
	}
	var result []string
	for _, harness := range canonicalHarnesses {
		if selected[harness.ID] {
			result = append(result, harness.ID)
		}
	}
	return result
}

func artifactHarness(paths installationPaths, target, source string) string {
	source = filepath.ToSlash(filepath.Clean(source))
	switch {
	case strings.HasPrefix(source, "plugins/opencode/"):
		return "opencode"
	case strings.HasPrefix(source, "plugins/pi/"):
		return "pi"
	case strings.HasPrefix(source, "plugins/hermes/"):
		return "hermes"
	case strings.HasPrefix(source, "plugins/claude-code/"):
		return "claude-code"
	case strings.HasPrefix(source, "plugins/codex/"):
		return "codex"
	case strings.HasPrefix(source, "plugins/cursor/"):
		return "cursor"
	case strings.HasPrefix(source, "skills/"):
		if paths.HermesHome != "" && pathWithin(filepath.Join(paths.HermesHome, "skills"), target) {
			return "hermes"
		}
		if pathWithin(filepath.Join(paths.OpenCodeHome, "skills"), target) {
			return "opencode"
		}
	}
	return ""
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func sortedArtifactTargets(artifacts map[string]installReceiptArtifact) []string {
	targets := make([]string, 0, len(artifacts))
	for target := range artifacts {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}
