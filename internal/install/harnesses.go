package install

import (
	"fmt"
	"os"
	"os/exec"
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
	{ID: "oh-my-pi", Name: "Oh My Pi"},
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
			return nil, fmt.Errorf("unknown harness %q (want opencode, pi, oh-my-pi, hermes, claude-code, codex, cursor, or all)", raw)
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
	paths, err := Paths()
	if err != nil {
		return nil, err
	}
	manifest, err := loadManifest(paths)
	if err != nil {
		return nil, err
	}
	detected := detectedHarnessSet(paths)
	selected := stringSet(manifest.Harnesses)
	result := CanonicalHarnesses()
	for i := range result {
		result[i].Detected = detected[result[i].ID]
		result[i].Selected = selected[result[i].ID]
	}
	return result, nil
}

func DetectedHarnesses() ([]string, error) {
	paths, err := Paths()
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

func detectedHarnessSet(paths InstallationPaths) map[string]bool {
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
	if _, err := exec.LookPath("omp"); err == nil {
		result["oh-my-pi"] = true
	}
	return result
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
