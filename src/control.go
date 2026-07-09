package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	envMimirHome = "MIMIR_HOME"
	configFile   = "config.toml"
	defaultLog   = "mimir.log"
	sessionsDir  = "sessions"
)

// ControlConfig is the human-readable ~/.mimir/config.toml schema.
type ControlConfig struct {
	Machine  string
	Sessions struct {
		Enabled        bool
		Repo           string
		Path           string
		DefaultHarness string
	}
	Code struct {
		PreferMCP        bool
		AutoIndexIfStale bool
	}
	Log struct {
		Path  string
		Level string
	}
}

func defaultControlConfig(machine string) ControlConfig {
	var c ControlConfig
	c.Machine = machine
	c.Sessions.Enabled = false
	c.Sessions.Path = filepath.ToSlash(filepath.Join("~", ".mimir", "sessions"))
	c.Code.PreferMCP = true
	c.Code.AutoIndexIfStale = true
	c.Log.Path = filepath.ToSlash(filepath.Join("~", ".mimir", defaultLog))
	c.Log.Level = "info"
	return c
}

func mimirHome() (string, error) {
	if v := strings.TrimSpace(os.Getenv(envMimirHome)); v != "" {
		return filepath.Clean(v), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mimir"), nil
}

func controlPaths() (home, cfgPath, logPath string, err error) {
	home, err = mimirHome()
	if err != nil {
		return "", "", "", err
	}
	return home, filepath.Join(home, configFile), filepath.Join(home, defaultLog), nil
}

func expandHomePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, p[2:]), nil
	}
	// Also expand %USERPROFILE% style if present
	if strings.HasPrefix(p, "%USERPROFILE%") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(p, "%USERPROFILE%")), nil
	}
	return p, nil
}

// deriveMachine picks a short machine id without asking.
func deriveMachine() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return fallbackMachine()
	}
	host = strings.ToLower(strings.TrimSpace(host))
	// strip domain
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	// known alias substrings
	aliases := []string{"therig", "thedeck"}
	for _, a := range aliases {
		if host == a || strings.Contains(host, a) {
			return a
		}
	}
	// clean to simple id
	var b strings.Builder
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return fallbackMachine()
	}
	return b.String()
}

func fallbackMachine() string {
	switch runtime.GOOS {
	case "windows":
		return "win"
	case "darwin":
		return "mac"
	default:
		return "box"
	}
}

func loadControlConfig() (ControlConfig, error) {
	home, cfgPath, _, err := controlPaths()
	if err != nil {
		return ControlConfig{}, err
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := defaultControlConfig(deriveMachine())
			cfg.Sessions.Path = displaySessionsPath(home)
			cfg.Log.Path = displayLogPath(home)
			return cfg, nil
		}
		return ControlConfig{}, err
	}
	cfg, err := parseControlTOML(string(data))
	if err != nil {
		return ControlConfig{}, err
	}
	if cfg.Machine == "" {
		cfg.Machine = deriveMachine()
	}
	if cfg.Sessions.Path == "" {
		cfg.Sessions.Path = displaySessionsPath(home)
	}
	if cfg.Log.Path == "" {
		cfg.Log.Path = displayLogPath(home)
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	return cfg, nil
}

func displaySessionsPath(home string) string {
	return filepath.ToSlash(filepath.Join("~", ".mimir", sessionsDir))
}

func displayLogPath(home string) string {
	_ = home
	return filepath.ToSlash(filepath.Join("~", ".mimir", defaultLog))
}

func sessionsAbsPath(cfg ControlConfig) (string, error) {
	if cfg.Sessions.Path != "" {
		return expandMimirPath(cfg.Sessions.Path)
	}
	home, err := mimirHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, sessionsDir), nil
}

func logAbsPath(cfg ControlConfig) (string, error) {
	if cfg.Log.Path != "" {
		return expandMimirPath(cfg.Log.Path)
	}
	_, _, lp, err := controlPaths()
	return lp, err
}

// expandMimirPath resolves paths. Portable "~/.mimir/..." prefixes honor MIMIR_HOME.
func expandMimirPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	home, err := mimirHome()
	if err != nil {
		return "", err
	}
	slash := filepath.ToSlash(p)
	const prefix = "~/.mimir"
	if slash == prefix {
		return home, nil
	}
	if strings.HasPrefix(slash, prefix+"/") {
		return filepath.Join(home, filepath.FromSlash(slash[len(prefix)+1:])), nil
	}
	return expandHomePath(p)
}

func saveControlConfig(cfg ControlConfig) error {
	home, cfgPath, _, err := controlPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	if cfg.Machine == "" {
		cfg.Machine = deriveMachine()
	}
	if cfg.Sessions.Path == "" {
		cfg.Sessions.Path = displaySessionsPath(home)
	}
	if cfg.Log.Path == "" {
		cfg.Log.Path = displayLogPath(home)
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	body := encodeControlTOML(cfg)
	tmp := cfgPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, cfgPath)
}

func controlInit(machine string) (ControlConfig, error) {
	home, cfgPath, defaultLogPath, err := controlPaths()
	if err != nil {
		return ControlConfig{}, err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return ControlConfig{}, err
	}
	existing, err := loadControlConfig()
	if err != nil {
		return ControlConfig{}, err
	}
	// if config already exists on disk, preserve machine unless override
	if pathExists(cfgPath) {
		if machine != "" && machine != existing.Machine {
			existing.Machine = machine
			if err := saveControlConfig(existing); err != nil {
				return ControlConfig{}, err
			}
		}
	} else {
		if machine == "" {
			machine = deriveMachine()
		}
		existing = defaultControlConfig(machine)
		existing.Sessions.Path = displaySessionsPath(home)
		existing.Log.Path = displayLogPath(home)
		if err := saveControlConfig(existing); err != nil {
			return ControlConfig{}, err
		}
	}

	logPath, err := logAbsPath(existing)
	if err != nil {
		logPath = defaultLogPath
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return ControlConfig{}, err
	}
	// ensure log file exists
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return ControlConfig{}, err
	}
	_ = f.Close()

	_ = appendLog(existing, "control.init", fmt.Sprintf("machine=%s path=%s", existing.Machine, home), "ok")
	return existing, nil
}

// Minimal TOML encode/decode for the locked control schema (no external deps).

func encodeControlTOML(cfg ControlConfig) string {
	var b strings.Builder
	b.WriteString("# ~/.mimir/config.toml — agent owns most writes\n")
	fmt.Fprintf(&b, "machine = %q\n\n", cfg.Machine)
	b.WriteString("[sessions]\n")
	fmt.Fprintf(&b, "enabled = %t\n", cfg.Sessions.Enabled)
	if cfg.Sessions.Repo != "" {
		fmt.Fprintf(&b, "repo = %q\n", cfg.Sessions.Repo)
	}
	fmt.Fprintf(&b, "path = %q\n", cfg.Sessions.Path)
	if cfg.Sessions.DefaultHarness != "" {
		fmt.Fprintf(&b, "default_harness = %q\n", cfg.Sessions.DefaultHarness)
	}
	b.WriteString("\n[code]\n")
	fmt.Fprintf(&b, "prefer_mcp = %t\n", cfg.Code.PreferMCP)
	fmt.Fprintf(&b, "auto_index_if_stale = %t\n", cfg.Code.AutoIndexIfStale)
	b.WriteString("\n[log]\n")
	fmt.Fprintf(&b, "path = %q\n", cfg.Log.Path)
	fmt.Fprintf(&b, "level = %q\n", cfg.Log.Level)
	return b.String()
}

func parseControlTOML(src string) (ControlConfig, error) {
	cfg := defaultControlConfig("")
	section := ""
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, val, ok := splitKV(line)
		if !ok {
			continue
		}
		switch section {
		case "":
			if key == "machine" {
				cfg.Machine = unquote(val)
			}
		case "sessions":
			switch key {
			case "enabled":
				cfg.Sessions.Enabled = parseBool(val)
			case "repo":
				cfg.Sessions.Repo = unquote(val)
			case "path":
				cfg.Sessions.Path = unquote(val)
			case "default_harness":
				cfg.Sessions.DefaultHarness = unquote(val)
			}
		case "code":
			switch key {
			case "prefer_mcp":
				cfg.Code.PreferMCP = parseBool(val)
			case "auto_index_if_stale":
				cfg.Code.AutoIndexIfStale = parseBool(val)
			}
		case "log":
			switch key {
			case "path":
				cfg.Log.Path = unquote(val)
			case "level":
				cfg.Log.Level = unquote(val)
			}
		}
	}
	return cfg, nil
}

func splitKV(line string) (string, string, bool) {
	i := strings.IndexByte(line, '=')
	if i < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:i])
	val := strings.TrimSpace(line[i+1:])
	if key == "" {
		return "", "", false
	}
	return key, val, true
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func parseBool(v string) bool {
	switch strings.ToLower(unquote(v)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}
