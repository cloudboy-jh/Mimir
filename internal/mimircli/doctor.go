package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Repair string `json:"repair,omitempty"`
}

type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

func doctor(ctx context.Context, args []string, out io.Writer) error {
	jsonOutput := false
	for _, arg := range args {
		if arg != "--json" {
			return fmt.Errorf("usage: mimir doctor [--json]")
		}
		jsonOutput = true
	}
	report := runDoctor(ctx)
	if jsonOutput {
		data, err := json.Marshal(report)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	}
	for _, check := range report.Checks {
		fmt.Fprintf(out, "%s  %s", check.Status, check.Name)
		if check.Detail != "" {
			fmt.Fprintf(out, " · %s", check.Detail)
		}
		if check.Repair != "" {
			fmt.Fprintf(out, " · repair: %s", check.Repair)
		}
		fmt.Fprintln(out)
	}
	return nil
}

func runDoctor(ctx context.Context) doctorReport {
	report := doctorReport{OK: true}
	add := func(name, status, detail, repair string) {
		report.Checks = append(report.Checks, doctorCheck{Name: name, Status: status, Detail: detail, Repair: repair})
		if status == "failed" {
			report.OK = false
		}
	}
	pointer, err := loadPointer()
	if err != nil {
		add("connection", "failed", err.Error(), "mimir login")
		return report
	}
	if _, err := remoteRequestWithPointer(ctx, pointer, "GET", "/whoami", nil); err != nil {
		add("worker", "failed", err.Error(), "mimir login")
	} else {
		add("worker", "ok", pointer.URL, "")
	}
	manifest, err := currentConnectionManifest(pointer.URL)
	if err != nil {
		add("connection.manifest", "failed", err.Error(), "mimir login")
		return report
	}
	home, err := os.UserHomeDir()
	if err != nil {
		add("opencode.home", "failed", err.Error(), "")
		return report
	}
	configDir := filepath.Join(home, ".config", "opencode")
	plugin, err := os.ReadFile(filepath.Join(configDir, "plugins", "mimir.ts"))
	marker := fmt.Sprintf("mimir-opencode-adapter-version: %d", openCodeAdapterVersion)
	if err != nil || !strings.Contains(string(plugin), marker) {
		add("opencode.adapter", "failed", "missing or stale adapter", "mimir update")
	} else {
		add("opencode.adapter", "ok", fmt.Sprintf("version %d", openCodeAdapterVersion), "")
	}
	data, err := os.ReadFile(filepath.Join(configDir, "opencode.json"))
	if err != nil {
		add("opencode.config", "failed", err.Error(), "mimir login")
		return report
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		add("opencode.config", "failed", err.Error(), "repair opencode.json")
		return report
	}
	providers, _ := config["provider"].(map[string]any)
	openrouter, _ := providers["openrouter"].(map[string]any)
	options, _ := openrouter["options"].(map[string]any)
	wantCredential := "{file:" + filepath.ToSlash(manifest.CredentialFile) + "}"
	if options["baseURL"] != manifest.OpenAIBaseURL || options["apiKey"] != wantCredential {
		add("opencode.provider", "failed", "provider route or credential reference does not match Mimir", "mimir update")
	} else {
		add("opencode.provider", "ok", manifest.OpenAIBaseURL, "")
	}
	mcp, _ := config["mcp"].(map[string]any)
	mimir, _ := mcp["mimir"].(map[string]any)
	command, _ := mimir["command"].([]any)
	commandMatches := len(command) == len(manifest.MCPCommand)
	for i := 0; commandMatches && i < len(command); i++ {
		if value, ok := command[i].(string); !ok || value != manifest.MCPCommand[i] {
			commandMatches = false
		}
	}
	if !commandMatches || mimir["type"] != "local" || mimir["enabled"] != true {
		add("opencode.mcp", "failed", "MCP command is missing or stale", "mimir update")
	} else {
		add("opencode.mcp", "ok", manifest.MCPCommand[0], "")
	}
	return report
}
