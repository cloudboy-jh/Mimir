package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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

type workerIdentity struct {
	Service      string   `json:"service"`
	APIVersion   int      `json:"api_version"`
	Capabilities []string `json:"capabilities"`
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
	artifacts, artifactErr := checkManagedArtifacts()
	if artifactErr != nil {
		add("managed-artifacts", "failed", artifactErr.Error(), "mimir install or mimir update")
	} else {
		for _, artifact := range artifacts.Artifacts {
			status := "ok"
			repair := ""
			if artifact.Status != artifactCurrent {
				status = "failed"
				switch artifact.Status {
				case artifactOutdated, artifactMissing:
					repair = "mimir install"
				case artifactConflict, artifactModified:
					repair = "review the preserved Mimir file; remove or restore it, then run mimir install"
				default:
					repair = "mimir install or mimir update"
				}
			}
			add("managed-artifact "+artifact.Source, status, string(artifact.Status)+" · "+artifact.Path, repair)
		}
	}
	pointer, err := loadPointer()
	if err != nil {
		add("connection", "failed", err.Error(), "mimir login")
		return report
	}
	if data, err := remoteRequestWithPointer(ctx, pointer, "GET", "/whoami", nil); err != nil {
		add("worker", "failed", err.Error(), "mimir login")
	} else if err := validateWorkerIdentity(data); err != nil {
		add("worker", "failed", err.Error(), "mimir deploy")
	} else {
		add("worker", "ok", pointer.URL, "")
	}
	manifest, err := currentConnectionManifest(pointer.URL)
	if err != nil {
		add("connection.manifest", "failed", err.Error(), "mimir login")
		return report
	}
	checkOpenCodeMCP(add, manifest)
	checkHermesIntegration(ctx, add, pointer, manifest)
	return report
}

func validateWorkerIdentity(data []byte) error {
	var identity workerIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return fmt.Errorf("invalid /whoami response: %w", err)
	}
	if identity.Service != "mimir" || identity.APIVersion < 1 {
		return fmt.Errorf("deployed Worker predates the versioned machine API")
	}
	capabilities := make(map[string]bool, len(identity.Capabilities))
	for _, capability := range identity.Capabilities {
		capabilities[capability] = true
	}
	for _, required := range []string{"hermes_authorization", "session_events", "session_lifecycle"} {
		if !capabilities[required] {
			return fmt.Errorf("deployed Worker lacks required capability %s", required)
		}
	}
	return nil
}

func checkOpenCodeMCP(add func(string, string, string, string), manifest connectionManifest) {
	if _, err := findOpenCode(); err != nil {
		add("opencode.mcp", "skipped", "OpenCode is not installed", "")
		return
	}
	command := manifest.MCPCommand
	if len(command) != 2 || command[1] != "serve" {
		add("opencode.mcp", "failed", "Mimir connection manifest has a malformed MCP command", "mimir login")
		return
	}
	if info, err := os.Lstat(command[0]); err != nil || !info.Mode().IsRegular() {
		add("opencode.mcp", "failed", "Mimir MCP executable does not exist: "+command[0], "mimir install")
		return
	}
	add("opencode.mcp", "ok", "managed plugin injects "+strings.Join(command, " ")+" at startup", "")
}

func checkHermesIntegration(ctx context.Context, add func(string, string, string, string), pointer Pointer, manifest connectionManifest) {
	hermesHome, hermesFound, err := discoverHermesHome()
	if err != nil {
		add("hermes.home", "failed", err.Error(), "")
		return
	}
	if !hermesFound {
		add("hermes", "skipped", "Hermes is not installed", "")
		return
	}
	if enabled, err := hermesPluginEnabled(ctx, hermesHome); err != nil {
		add("hermes.plugin", "failed", err.Error(), "hermes plugins enable mimir")
	} else if !enabled {
		add("hermes.plugin", "failed", "Mimir plugin is disabled", "hermes plugins enable mimir")
	} else {
		add("hermes.plugin", "ok", "Mimir plugin is enabled", "")
	}
	if matches, detail := hermesIntegrationMatches(hermesHome, manifest); !matches {
		add("hermes.openrouter", "failed", detail, "mimir update")
	} else {
		add("hermes.openrouter", "ok", detail, "")
	}
	hermesKey, err := hermesOpenRouterKey(hermesHome)
	if err != nil {
		add("hermes.credential", "failed", err.Error(), "configure Hermes OpenRouter authentication")
		return
	}
	hermesPointer := Pointer{URL: pointer.URL, Token: hermesKey}
	for _, endpoint := range []string{"models", "key", "credits"} {
		path := "/v1/hermes/" + endpoint
		if _, err := remoteRequestWithPointer(ctx, hermesPointer, "GET", path, nil); err != nil {
			add("hermes."+endpoint, "failed", err.Error(), "mimir deploy")
		} else {
			add("hermes."+endpoint, "ok", path, "")
		}
	}
}
