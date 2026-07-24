package mimircli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const (
	hermesManagedStart = "# >>> mimir managed openrouter route"
	hermesManagedEnd   = "# <<< mimir managed openrouter route"
)

var dotenvExpansion = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func installCurrentHermesIntegration(ctx context.Context) (bool, error) {
	pointer, err := loadPointer()
	if err != nil {
		return false, err
	}
	manifest, err := currentConnectionManifest(pointer.URL)
	if err != nil {
		return false, err
	}
	home, found, err := discoverHermesHome()
	if err != nil || !found {
		return false, err
	}
	if err := enableHermesPlugin(ctx, home); err != nil {
		return false, err
	}
	hermesKey, err := hermesOpenRouterKey(home)
	if err != nil {
		return false, err
	}
	if err := authorizeHermesCredential(ctx, pointer, hermesKey); err != nil {
		return false, fmt.Errorf("authorizing Hermes OpenRouter credential: %w", err)
	}
	if err := installHermesIntegration(home, manifest); err != nil {
		return false, err
	}
	return true, nil
}

var runHermesPluginCommand = func(ctx context.Context, home string, args ...string) error {
	command := exec.CommandContext(ctx, "hermes", append([]string{"plugins"}, args...)...)
	command.Env = append(os.Environ(), "HERMES_HOME="+home)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("hermes plugins %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

var listHermesPlugins = func(ctx context.Context, home string) (string, error) {
	command := exec.CommandContext(ctx, "hermes", "plugins", "list", "--plain", "--no-bundled")
	command.Env = append(os.Environ(), "HERMES_HOME="+home)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("hermes plugins list: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func hermesPluginEnabled(ctx context.Context, home string) (bool, error) {
	output, err := listHermesPlugins(ctx, home)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "enabled" && fields[len(fields)-1] == "mimir" {
			return true, nil
		}
	}
	return false, nil
}

func enableHermesPlugin(ctx context.Context, home string) error {
	if err := runHermesPluginCommand(ctx, home, "enable", "mimir"); err != nil {
		return fmt.Errorf("enabling Hermes Mimir plugin: %w", err)
	}
	return nil
}

func disableHermesPlugin(ctx context.Context, home string) error {
	if err := runHermesPluginCommand(ctx, home, "disable", "mimir"); err != nil {
		return fmt.Errorf("disabling Hermes Mimir plugin: %w", err)
	}
	return nil
}

func authorizeHermesCredential(ctx context.Context, pointer Pointer, token string) error {
	hash := sha256.Sum256([]byte(token))
	_, err := remoteRequestWithPointer(ctx, pointer, "POST", "/integrations/hermes/authorize", map[string]string{"token_hash": hex.EncodeToString(hash[:])})
	var apiErr *apiError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == 404 || apiErr.StatusCode == 405) {
		return fmt.Errorf("deployed Worker is older than this CLI and lacks Hermes authorization; run mimir deploy")
	}
	return err
}

func discoverHermesHome() (string, bool, error) {
	if configured := strings.TrimSpace(os.Getenv("HERMES_HOME")); configured != "" {
		configured = filepath.Clean(configured)
		if _, err := os.Stat(configured); os.IsNotExist(err) {
			return configured, true, nil
		} else if err != nil {
			return "", false, err
		}
		return resolveHermesProfileHome(configured)
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
	return resolveHermesProfileHome(home)
}

func resolveHermesProfileHome(home string) (string, bool, error) {
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
		if filepath.Base(profile) != profile || profile == "." || strings.ContainsAny(profile, `/\\`) {
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

func installHermesIntegration(home string, manifest connectionManifest) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	path := filepath.Join(home, ".env")
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlinked Hermes .env")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	updated, err := upsertHermesEnv(current, manifest.OpenAIBaseURL+"/hermes")
	if err != nil {
		return err
	}
	if string(current) == string(updated) {
		return os.Chmod(path, 0o600)
	}
	return writeHermesEnv(path, updated)
}

func uninstallHermesIntegration() harnessIntegrationState {
	home, found, err := discoverHermesHome()
	if err != nil {
		return harnessIntegrationState{State: "preserved", Provider: "openrouter", Scope: "openrouter", Detail: err.Error()}
	}
	if !found {
		return harnessIntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	}
	path := filepath.Join(home, ".env")
	if info, err := os.Lstat(path); os.IsNotExist(err) {
		return harnessIntegrationState{State: "absent", Provider: "openrouter", Scope: "openrouter", Detail: "Mimir managed OpenRouter route is absent"}
	} else if err != nil {
		return harnessIntegrationState{State: "preserved", Provider: "openrouter", Scope: "openrouter", Detail: err.Error()}
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return harnessIntegrationState{State: "preserved", Provider: "openrouter", Scope: "openrouter", Detail: "Hermes .env is symlinked or non-regular"}
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return harnessIntegrationState{State: "preserved", Provider: "openrouter", Scope: "openrouter", Detail: err.Error()}
	}
	updated, status := removeHermesManagedEnv(current)
	if status != "removed" {
		detail := "Mimir managed OpenRouter route is absent"
		if status == "preserved" {
			detail = "Hermes .env managed block is malformed or modified; preserving it"
		}
		return harnessIntegrationState{State: status, Provider: "openrouter", Scope: "openrouter", Detail: detail}
	}
	if err := writeHermesEnv(path, updated); err != nil {
		return harnessIntegrationState{State: "preserved", Provider: "openrouter", Scope: "openrouter", Detail: err.Error()}
	}
	return harnessIntegrationState{State: "removed", Provider: "openrouter", Scope: "openrouter", RestartRequired: true, Detail: "Mimir managed OpenRouter route removed; restart Hermes"}
}

func removeHermesManagedEnv(current []byte) ([]byte, string) {
	newline := "\n"
	if strings.Contains(string(current), "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(current), "\r\n", "\n"), "\n")
	start, end := -1, -1
	for i, line := range lines {
		switch line {
		case hermesManagedStart:
			if start != -1 {
				return current, "preserved"
			}
			start = i
		case hermesManagedEnd:
			if end != -1 {
				return current, "preserved"
			}
			end = i
		}
	}
	if start == -1 && end == -1 {
		return current, "absent"
	}
	if start == -1 || end != start+2 || end >= len(lines) {
		return current, "preserved"
	}
	const prefix = "OPENROUTER_BASE_URL="
	assignment := lines[start+1]
	if !strings.HasPrefix(assignment, prefix) {
		return current, "preserved"
	}
	value, err := strconv.Unquote(strings.TrimPrefix(assignment, prefix))
	if err != nil || assignment != prefix+strconv.Quote(value) || value != strings.TrimRight(value, "/") || !strings.HasSuffix(value, "/v1/hermes") {
		return current, "preserved"
	}
	removeStart := start
	if removeStart > 0 && lines[removeStart-1] == "" {
		removeStart--
	}
	lines = append(lines[:removeStart], lines[end+1:]...)
	return []byte(strings.ReplaceAll(strings.Join(lines, "\n"), "\n", newline)), "removed"
}

func upsertHermesEnv(current []byte, baseURL string) ([]byte, error) {
	newline := "\n"
	if strings.Contains(string(current), "\r\n") {
		newline = "\r\n"
	}
	normalized := strings.ReplaceAll(string(current), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	start, end := -1, -1
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case hermesManagedStart:
			if start != -1 {
				return nil, fmt.Errorf("Hermes .env contains duplicate Mimir managed blocks")
			}
			start = i
		case hermesManagedEnd:
			if end != -1 {
				return nil, fmt.Errorf("Hermes .env contains duplicate Mimir managed blocks")
			}
			end = i
		}
	}
	if (start == -1) != (end == -1) || (start != -1 && end < start) {
		return nil, fmt.Errorf("Hermes .env contains a malformed Mimir managed block")
	}
	if start != -1 {
		lines = append(lines[:start], lines[end+1:]...)
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines,
		hermesManagedStart,
		"OPENROUTER_BASE_URL="+strconv.Quote(strings.TrimRight(baseURL, "/")),
		hermesManagedEnd,
		"",
	)
	return []byte(strings.ReplaceAll(strings.Join(lines, "\n"), "\n", newline)), nil
}

func writeHermesEnv(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".mimir-hermes-env-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func hermesIntegrationMatches(home string, manifest connectionManifest) (bool, string) {
	data, err := os.ReadFile(filepath.Join(home, ".env"))
	if err != nil {
		return false, err.Error()
	}
	want, err := upsertHermesEnv(data, manifest.OpenAIBaseURL+"/hermes")
	if err != nil {
		return false, err.Error()
	}
	if string(data) != string(want) {
		return false, "OpenRouter route or machine credential does not match Mimir"
	}
	return true, manifest.OpenAIBaseURL + "/hermes"
}

func hermesOpenRouterKey(home string) (string, error) {
	data, err := os.ReadFile(filepath.Join(home, ".env"))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	values, err := parseHermesDotenv(data)
	if err != nil {
		return "", err
	}
	value := values["OPENROUTER_API_KEY"]
	if strings.TrimSpace(value) == "" {
		value = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("Hermes OPENROUTER_API_KEY is missing")
	}
	return value, nil
}

func parseHermesDotenv(data []byte) (map[string]string, error) {
	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, raw, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		raw = strings.TrimSpace(raw)
		value := ""
		if strings.HasPrefix(raw, "'") {
			if len(raw) < 2 || !strings.HasSuffix(raw, "'") {
				return nil, fmt.Errorf("Hermes .env has an unterminated single-quoted value for %s", key)
			}
			value = strings.ReplaceAll(strings.ReplaceAll(raw[1:len(raw)-1], `\'`, `'`), `\\`, `\`)
		} else if strings.HasPrefix(raw, `"`) {
			unquoted, err := strconv.Unquote(raw)
			if err != nil {
				return nil, fmt.Errorf("Hermes .env has an invalid quoted value for %s", key)
			}
			value = unquoted
		} else {
			value = stripDotenvComment(raw)
		}
		value = dotenvExpansion.ReplaceAllStringFunc(value, func(match string) string {
			name := dotenvExpansion.FindStringSubmatch(match)[1]
			if prior, ok := values[name]; ok {
				return prior
			}
			return os.Getenv(name)
		})
		values[key] = value
	}
	return values, nil
}

func stripDotenvComment(value string) string {
	for index, character := range value {
		if character == '#' && index > 0 {
			previous := value[index-1]
			if previous == ' ' || previous == '\t' {
				return strings.TrimSpace(value[:index])
			}
		}
	}
	return strings.TrimSpace(value)
}
