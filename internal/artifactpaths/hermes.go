package artifactpaths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// HermesHome resolves the filesystem root where managed Hermes artifacts live.
func HermesHome() (string, bool, error) {
	if configured := strings.TrimSpace(os.Getenv("HERMES_HOME")); configured != "" {
		configured = filepath.Clean(configured)
		if _, err := os.Stat(configured); os.IsNotExist(err) {
			return configured, true, nil
		} else if err != nil {
			return "", false, err
		}
		return ResolveHermesProfile(configured)
	}
	var home string
	if runtime.GOOS == "windows" {
		base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if base == "" {
			userHome, err := os.UserHomeDir()
			if err != nil {
				return "", false, err
			}
			base = filepath.Join(userHome, "AppData", "Local")
		}
		home = filepath.Join(base, "hermes")
	} else {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", false, err
		}
		home = filepath.Join(userHome, ".hermes")
	}
	return ResolveHermesProfile(home)
}

func ResolveHermesProfile(home string) (string, bool, error) {
	info, err := os.Stat(home)
	if os.IsNotExist(err) {
		return home, false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.IsDir() {
		return home, false, nil
	}
	active, err := os.ReadFile(filepath.Join(home, "active_profile"))
	if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	profile := strings.TrimSpace(string(active))
	if profile != "" && profile != "default" {
		if filepath.Base(profile) != profile || profile == "." || strings.ContainsAny(profile, `/\`) {
			return "", false, fmt.Errorf("Hermes active_profile is invalid")
		}
		profileHome := filepath.Join(home, "profiles", profile)
		profileInfo, err := os.Stat(profileHome)
		if err != nil {
			return "", false, fmt.Errorf("Hermes active profile %q is unavailable: %w", profile, err)
		}
		if !profileInfo.IsDir() {
			return "", false, fmt.Errorf("Hermes active profile %q is not a directory", profile)
		}
		home = profileHome
	}
	return home, true, nil
}
