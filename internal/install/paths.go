package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/artifactpaths"
)

// InstallationPaths lists the directories Mimir installs into.
type InstallationPaths struct {
	MimirHome      string `json:"mimir_home"`
	Manifest       string `json:"manifest"`
	PiHome         string `json:"pi_home"`
	OhMyPiHome     string `json:"oh_my_pi_home"`
	OpenCodeHome   string `json:"opencode_home"`
	ClaudeCodeHome string `json:"claude_code_home"`
	CodexHome      string `json:"codex_home"`
	AgentPlugins   string `json:"agent_plugins"`
	CursorHome     string `json:"cursor_home"`
	HermesHome     string `json:"hermes_home,omitempty"`
	HermesDetected bool   `json:"hermes_detected"`
	Worker         string `json:"worker"`
	SharedAssets   string `json:"shared_assets"`
}

func Paths() (InstallationPaths, error) {
	pointer, err := pointerPath()
	if err != nil {
		return InstallationPaths{}, err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return InstallationPaths{}, err
	}
	mimirHome := filepath.Dir(pointer)
	piHome, err := configuredHome("PI_CODING_AGENT_DIR", filepath.Join(userHome, ".pi", "agent"))
	if err != nil {
		return InstallationPaths{}, err
	}
	ohMyPiHome, err := configuredOhMyPiHome(userHome)
	if err != nil {
		return InstallationPaths{}, err
	}
	hermesHome, found, err := artifactpaths.HermesHome()
	if err != nil {
		return InstallationPaths{}, err
	}
	claudeHome, err := configuredHome("CLAUDE_CONFIG_DIR", filepath.Join(userHome, ".claude"))
	if err != nil {
		return InstallationPaths{}, err
	}
	codexHome, err := configuredHome("CODEX_HOME", filepath.Join(userHome, ".codex"))
	if err != nil {
		return InstallationPaths{}, err
	}
	return InstallationPaths{
		MimirHome:      mimirHome,
		Manifest:       filepath.Join(mimirHome, "managed.json"),
		PiHome:         piHome,
		OhMyPiHome:     ohMyPiHome,
		OpenCodeHome:   filepath.Join(userHome, ".config", "opencode"),
		ClaudeCodeHome: claudeHome,
		CodexHome:      codexHome,
		AgentPlugins:   filepath.Join(userHome, ".agents", "plugins"),
		CursorHome:     filepath.Join(userHome, ".cursor"),
		HermesHome:     hermesHome,
		HermesDetected: found,
		Worker:         filepath.Join(mimirHome, "worker"),
		SharedAssets:   filepath.Join(mimirHome, "assets", "images"),
	}, nil
}

func configuredOhMyPiHome(userHome string) (string, error) {
	configDir := strings.TrimSpace(os.Getenv("PI_CONFIG_DIR"))
	if configDir == "" {
		configDir = ".omp"
	}
	base := configDir
	if !filepath.IsAbs(base) {
		base = filepath.Join(userHome, base)
	}
	profile := strings.TrimSpace(os.Getenv("OMP_PROFILE"))
	if profile == "" {
		profile = strings.TrimSpace(os.Getenv("PI_PROFILE"))
	}
	if profile != "" && profile != "default" {
		if strings.ContainsAny(profile, `/\\`) || profile == "." || profile == ".." {
			return "", fmt.Errorf("invalid OMP_PROFILE")
		}
		return filepath.Join(base, "profiles", profile, "agent"), nil
	}
	if override := strings.TrimSpace(os.Getenv("OMP_CODING_AGENT_DIR")); override != "" {
		return configuredHome("OMP_CODING_AGENT_DIR", filepath.Join(base, "agent"))
	}
	return configuredHome("PI_CODING_AGENT_DIR", filepath.Join(base, "agent"))
}

func configuredHome(env, fallback string) (string, error) {
	value := strings.TrimSpace(os.Getenv(env))
	if value == "" {
		return fallback, nil
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", env, err)
	}
	return path, nil
}
