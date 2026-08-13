package hermes

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
	"strconv"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/artifactpaths"
	"github.com/cloudboy-jh/mimir/internal/harness"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

const (
	pluginSourcePrefix = "plugins/hermes/"
	managedStart       = "# >>> mimir managed openrouter route"
	managedEnd         = "# <<< mimir managed openrouter route"
)

func ArtifactSourcePrefixes() []string { return []string{pluginSourcePrefix} }

func OwnsArtifactSource(source string) bool {
	return strings.HasPrefix(filepath.ToSlash(source), pluginSourcePrefix)
}

var dotenvExpansion = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

type RemovalResult struct {
	State           string
	RestartRequired bool
	Detail          string
}

type Service struct {
	RunPluginCommand func(context.Context, string, ...string) error
	ListPlugins      func(context.Context, string) (string, error)
	Request          func(context.Context, mimirapi.Pointer, string, string, any) ([]byte, error)
	Discover         func() (string, bool, error)
}

func New() Service {
	return Service{
		RunPluginCommand: runPluginCommand,
		ListPlugins:      listPlugins,
		Request: func(ctx context.Context, pointer mimirapi.Pointer, method, path string, body any) ([]byte, error) {
			return mimirapi.New(pointer).Request(ctx, method, path, body)
		},
		Discover: DiscoverHome,
	}
}

func runPluginCommand(ctx context.Context, home string, args ...string) error {
	command := exec.CommandContext(ctx, "hermes", append([]string{"plugins"}, args...)...)
	command.Env = append(os.Environ(), "HERMES_HOME="+home)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("hermes plugins %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func listPlugins(ctx context.Context, home string) (string, error) {
	command := exec.CommandContext(ctx, "hermes", "plugins", "list", "--plain", "--no-bundled")
	command.Env = append(os.Environ(), "HERMES_HOME="+home)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("hermes plugins list: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func (s Service) Enable(ctx context.Context, home string) error {
	if err := s.RunPluginCommand(ctx, home, "enable", "mimir"); err != nil {
		return fmt.Errorf("enabling Hermes Mimir plugin: %w", err)
	}
	if err := cleanupLegacyMimir(home); err != nil {
		return fmt.Errorf("cleaning legacy Hermes Mimir integration: %w", err)
	}
	return nil
}

func (s Service) Disable(ctx context.Context, home string) error {
	if err := s.RunPluginCommand(ctx, home, "disable", "mimir"); err != nil {
		return fmt.Errorf("disabling Hermes Mimir plugin: %w", err)
	}
	return nil
}

func (s Service) PluginEnabled(ctx context.Context, home string) (bool, error) {
	output, err := s.ListPlugins(ctx, home)
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

func (s Service) Authorize(ctx context.Context, pointer mimirapi.Pointer, token string) error {
	hash := sha256.Sum256([]byte(token))
	_, err := s.Request(ctx, pointer, "POST", "/integrations/hermes/authorize", map[string]string{"token_hash": hex.EncodeToString(hash[:])})
	var apiErr *mimirapi.Error
	if errors.As(err, &apiErr) && (apiErr.StatusCode == 404 || apiErr.StatusCode == 405) {
		return fmt.Errorf("deployed Worker is older than this CLI and lacks Hermes authorization; run mimir deploy")
	}
	return err
}

func (s Service) Configure(ctx context.Context, pointer mimirapi.Pointer, manifest harness.ConnectionManifest, installationID string) (bool, error) {
	home, found, err := s.Discover()
	if err != nil || !found {
		return false, err
	}
	if err := s.Enable(ctx, home); err != nil {
		return false, err
	}
	key, err := OpenRouterKey(home)
	if err != nil {
		return false, err
	}
	if err := s.Authorize(ctx, pointer, key); err != nil {
		return false, fmt.Errorf("authorizing Hermes OpenRouter credential: %w", err)
	}
	if err := Install(home, manifest.OpenAIBaseURL, installationID); err != nil {
		return false, err
	}
	return true, nil
}

func (s Service) Uninstall() harness.IntegrationState {
	home, found, err := s.Discover()
	if err != nil {
		return harness.IntegrationState{State: "preserved", Provider: "openrouter", Scope: "openrouter", Detail: err.Error()}
	}
	if !found {
		return harness.IntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	}
	result := Remove(home)
	return harness.IntegrationState{State: result.State, Provider: "openrouter", Scope: "openrouter", RestartRequired: result.RestartRequired, Detail: result.Detail}
}

func (s Service) Doctor(ctx context.Context, pointer mimirapi.Pointer, manifest harness.ConnectionManifest, installationID string) []harness.Diagnostic {
	home, found, err := s.Discover()
	if err != nil {
		return []harness.Diagnostic{{Name: "hermes.home", Status: "failed", Detail: err.Error()}}
	}
	if !found {
		return []harness.Diagnostic{{Name: "hermes", Status: "skipped", Detail: "Hermes is not installed"}}
	}
	checks := make([]harness.Diagnostic, 0, 6)
	if enabled, err := s.PluginEnabled(ctx, home); err != nil {
		checks = append(checks, harness.Diagnostic{Name: "hermes.plugin", Status: "failed", Detail: err.Error(), Repair: "hermes plugins enable mimir"})
	} else if !enabled {
		checks = append(checks, harness.Diagnostic{Name: "hermes.plugin", Status: "failed", Detail: "Mimir plugin is disabled", Repair: "hermes plugins enable mimir"})
	} else {
		checks = append(checks, harness.Diagnostic{Name: "hermes.plugin", Status: "ok", Detail: "Mimir plugin is enabled"})
	}
	if matches, detail := IntegrationMatches(home, manifest.OpenAIBaseURL, installationID); !matches {
		checks = append(checks, harness.Diagnostic{Name: "hermes.openrouter", Status: "failed", Detail: detail, Repair: "mimir update"})
	} else {
		checks = append(checks, harness.Diagnostic{Name: "hermes.openrouter", Status: "ok", Detail: detail})
	}
	key, err := OpenRouterKey(home)
	if err != nil {
		return append(checks, harness.Diagnostic{Name: "hermes.credential", Status: "failed", Detail: err.Error(), Repair: "configure Hermes OpenRouter authentication"})
	}
	providerPointer := mimirapi.Pointer{URL: pointer.URL, Token: key}
	for _, endpoint := range []string{"models", "key", "credits"} {
		path := "/v1/hermes/" + installationID + "/" + endpoint
		if _, err := s.Request(ctx, providerPointer, "GET", path, nil); err != nil {
			checks = append(checks, harness.Diagnostic{Name: "hermes." + endpoint, Status: "failed", Detail: err.Error(), Repair: "mimir deploy"})
		} else {
			checks = append(checks, harness.Diagnostic{Name: "hermes." + endpoint, Status: "ok", Detail: path})
		}
	}
	return checks
}

func DiscoverHome() (string, bool, error) {
	return artifactpaths.HermesHome()
}

func ResolveProfileHome(home string) (string, bool, error) {
	return artifactpaths.ResolveHermesProfile(home)
}

func Install(home, openAIBaseURL, installationID string) error {
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(installationID) {
		return fmt.Errorf("invalid Mimir installation ID")
	}
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
	updated, err := UpsertEnv(current, openAIBaseURL+"/hermes/"+installationID)
	if err != nil {
		return err
	}
	if string(current) == string(updated) {
		return os.Chmod(path, 0o600)
	}
	return writeEnv(path, updated)
}

func Remove(home string) RemovalResult {
	path := filepath.Join(home, ".env")
	if info, err := os.Lstat(path); os.IsNotExist(err) {
		return RemovalResult{State: "absent", Detail: "Mimir managed OpenRouter route is absent"}
	} else if err != nil {
		return RemovalResult{State: "preserved", Detail: err.Error()}
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return RemovalResult{State: "preserved", Detail: "Hermes .env is symlinked or non-regular"}
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return RemovalResult{State: "preserved", Detail: err.Error()}
	}
	updated, status := RemoveManagedEnv(current)
	if status != "removed" {
		detail := "Mimir managed OpenRouter route is absent"
		if status == "preserved" {
			detail = "Hermes .env managed block is malformed or modified; preserving it"
		}
		return RemovalResult{State: status, Detail: detail}
	}
	if err := writeEnv(path, updated); err != nil {
		return RemovalResult{State: "preserved", Detail: err.Error()}
	}
	return RemovalResult{State: "removed", RestartRequired: true, Detail: "Mimir managed OpenRouter route removed; restart Hermes"}
}

func IntegrationMatches(home, openAIBaseURL, installationID string) (bool, string) {
	data, err := os.ReadFile(filepath.Join(home, ".env"))
	if err != nil {
		return false, err.Error()
	}
	want, err := UpsertEnv(data, openAIBaseURL+"/hermes/"+installationID)
	if err != nil {
		return false, err.Error()
	}
	if string(data) != string(want) {
		return false, "OpenRouter route or machine credential does not match Mimir"
	}
	return true, openAIBaseURL + "/hermes/" + installationID
}

func OpenRouterKey(home string) (string, error) {
	data, err := os.ReadFile(filepath.Join(home, ".env"))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	values, err := ParseDotenv(data)
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

func RemoveManagedEnv(current []byte) ([]byte, string) {
	newline := "\n"
	if strings.Contains(string(current), "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(current), "\r\n", "\n"), "\n")
	start, end := -1, -1
	for i, line := range lines {
		switch line {
		case managedStart:
			if start != -1 {
				return current, "preserved"
			}
			start = i
		case managedEnd:
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
	value, err := strconv.Unquote(strings.TrimPrefix(assignment, prefix))
	if !strings.HasPrefix(assignment, prefix) || err != nil || assignment != prefix+strconv.Quote(value) || value != strings.TrimRight(value, "/") || !regexp.MustCompile(`/v1/hermes(?:/[a-f0-9]{32})?$`).MatchString(value) {
		return current, "preserved"
	}
	removeStart := start
	if removeStart > 0 && lines[removeStart-1] == "" {
		removeStart--
	}
	lines = append(lines[:removeStart], lines[end+1:]...)
	return []byte(strings.ReplaceAll(strings.Join(lines, "\n"), "\n", newline)), "removed"
}

func UpsertEnv(current []byte, baseURL string) ([]byte, error) {
	newline := "\n"
	if strings.Contains(string(current), "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(current), "\r\n", "\n"), "\n")
	start, end := -1, -1
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case managedStart:
			if start != -1 {
				return nil, fmt.Errorf("Hermes .env contains duplicate Mimir managed blocks")
			}
			start = i
		case managedEnd:
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
	lines = append(lines, managedStart, "OPENROUTER_BASE_URL="+strconv.Quote(strings.TrimRight(baseURL, "/")), managedEnd, "")
	return []byte(strings.ReplaceAll(strings.Join(lines, "\n"), "\n", newline)), nil
}

func writeEnv(path string, data []byte) error {
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

func ParseDotenv(data []byte) (map[string]string, error) {
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
		if character == '#' && index > 0 && (value[index-1] == ' ' || value[index-1] == '\t') {
			return strings.TrimSpace(value[:index])
		}
	}
	return strings.TrimSpace(value)
}
