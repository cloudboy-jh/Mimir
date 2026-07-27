package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	openintegration "github.com/cloudboy-jh/mimir/internal/harness/opencode"
	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

type Requester interface {
	Request(context.Context, string, string, any) ([]byte, error)
}

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Repair string `json:"repair,omitempty"`
}

type Report struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

type State struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Repair string `json:"repair,omitempty"`
}

type StructuredReport struct {
	OK          bool               `json:"ok"`
	Installed   map[string][]State `json:"installed"`
	Active      map[string]State   `json:"active"`
	Deployed    map[string]State   `json:"deployed"`
	Connection  map[string]State   `json:"connection"`
	Diagnostics map[string]State   `json:"diagnostics"`
}

func (r Report) Structured() StructuredReport {
	structured := StructuredReport{
		OK: r.OK, Installed: map[string][]State{}, Active: map[string]State{},
		Deployed: map[string]State{}, Connection: map[string]State{}, Diagnostics: map[string]State{},
	}
	for _, check := range r.Checks {
		name := check.Name
		state := State{Status: check.Status, Detail: check.Detail, Repair: check.Repair}
		section := structured.Diagnostics
		switch {
		case strings.HasPrefix(name, "managed-artifact "):
			name = strings.TrimPrefix(name, "managed-artifact ")
			structured.Installed[name] = append(structured.Installed[name], state)
			continue
		case name == "managed-artifacts":
			structured.Installed[name] = append(structured.Installed[name], state)
			continue
		case name == "worker" || strings.HasPrefix(name, "worker."):
			section = structured.Deployed
		case name == "connection" || strings.HasPrefix(name, "connection."):
			section = structured.Connection
		case strings.HasPrefix(name, "opencode.") || strings.HasPrefix(name, "hermes.") || name == "harness-loads":
			section = structured.Active
		}
		section[name] = state
	}
	return structured
}

type Service struct {
	API            Requester
	CheckArtifacts func() (install.ArtifactReport, error)
	LoadReceipt    func() (install.Receipt, error)
	LoadPointer    func() (mimirapi.Pointer, error)
	Lifecycle      lifecycle.Service
	FindOpenCode   func() (string, error)
}

func New(api Requester) Service {
	return Service{
		API: api, CheckArtifacts: install.CheckArtifacts, LoadReceipt: install.LoadReceipt, LoadPointer: mimirapi.LoadPointer,
		Lifecycle: lifecycle.New(), FindOpenCode: func() (string, error) { return exec.LookPath("opencode") },
	}
}

func (s Service) Run(ctx context.Context) Report {
	report := Report{OK: true}
	add := func(name, status, detail, repair string) {
		report.Checks = append(report.Checks, Check{Name: name, Status: status, Detail: detail, Repair: repair})
		if status == "failed" {
			report.OK = false
		}
	}
	artifacts, artifactErr := s.CheckArtifacts()
	if artifactErr != nil {
		add("managed-artifacts", "failed", artifactErr.Error(), "mimir install or mimir update")
	} else {
		for _, artifact := range artifacts.Artifacts {
			status, repair := "ok", ""
			if artifact.Status != install.ArtifactCurrent {
				status = "failed"
				switch artifact.Status {
				case install.ArtifactOutdated, install.ArtifactMissing:
					repair = "mimir install"
				case install.ArtifactConflict, install.ArtifactModified:
					repair = "review the preserved Mimir file; remove or restore it, then run mimir install"
				default:
					repair = "mimir install or mimir update"
				}
			}
			add("managed-artifact "+artifact.Source, status, string(artifact.Status)+" · "+artifact.Path, repair)
		}
	}
	pointer, err := s.LoadPointer()
	if err != nil {
		add("connection", "failed", err.Error(), "mimir login")
		return report
	}
	workerReady := false
	data, err := s.API.Request(ctx, "GET", "/whoami", nil)
	if err != nil {
		add("worker", "failed", err.Error(), "mimir login")
	} else if err := ValidateWorkerIdentity(data); err != nil {
		add("worker", "failed", err.Error(), "mimir deploy")
	} else {
		add("worker", "ok", pointer.URL, "")
		workerReady = true
		var identity workerIdentity
		_ = json.Unmarshal(data, &identity)
		expected, identityErr := install.EmbeddedWorkerIdentity()
		if identityErr != nil {
			add("worker.bundle", "failed", identityErr.Error(), "mimir deploy")
		} else if identity.BundleSHA256 == "" {
			add("worker.bundle", "skipped", "deployed Worker bundle identity is unknown", "mimir deploy")
		} else if identity.BundleSHA256 != expected.SHA256 {
			add("worker.bundle", "failed", fmt.Sprintf("installed %s (%s), deployed %s (%s)", expected.Version, expected.SHA256, identity.BundleVersion, identity.BundleSHA256), "mimir deploy")
		} else {
			add("worker.bundle", "ok", fmt.Sprintf("installed and deployed %s · sha256 %s", expected.Version, expected.SHA256), "")
		}
	}
	if workerReady {
		s.addHarnessLoadChecks(ctx, artifacts, add)
	}
	manifest, err := s.Lifecycle.Manifest(pointer.URL)
	if err != nil {
		add("connection.manifest", "failed", err.Error(), "mimir login")
		return report
	}
	if _, err := s.FindOpenCode(); err != nil {
		add("opencode.mcp", "skipped", "OpenCode is not installed", "")
	} else if err := openintegration.ValidateCommand(manifest.MCPCommand); err != nil {
		add("opencode.mcp", "failed", err.Error(), "mimir install")
	} else {
		add("opencode.mcp", "ok", "managed plugin injects "+strings.Join(manifest.MCPCommand, " ")+" at startup", "")
	}
	for _, check := range s.Lifecycle.Hermes.Doctor(ctx, pointer, manifest) {
		add(check.Name, check.Status, check.Detail, check.Repair)
	}
	return report
}

type harnessLoad struct {
	Harness        string `json:"harness"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	BundleVersion  string `json:"bundle_version"`
	CLIVersion     string `json:"cli_version"`
	CLICommit      string `json:"cli_commit"`
	InstallationID string `json:"installation_id"`
	ClientLoadedAt string `json:"client_loaded_at"`
	ReportedAt     string `json:"reported_at"`
}

type harnessLoadsResponse struct {
	Loads []harnessLoad `json:"loads"`
}

func (s Service) addHarnessLoadChecks(ctx context.Context, artifacts install.ArtifactReport, add func(string, string, string, string)) {
	plugins := make(map[string]install.ArtifactResult)
	for _, artifact := range artifacts.Artifacts {
		if !artifactUsable(artifact.Status) || artifact.BundleHash == "" {
			continue
		}
		switch artifact.Source {
		case "plugins/opencode/mimir.ts":
			plugins["opencode"] = artifact
		case "plugins/hermes/__init__.py":
			plugins["hermes"] = artifact
		}
	}
	if len(plugins) == 0 {
		return
	}
	receipt, err := s.LoadReceipt()
	if err != nil {
		add("harness-loads", "failed", err.Error(), "mimir install or mimir update")
		return
	}
	data, err := s.API.Request(ctx, http.MethodGet, "/integrations/harness-loads", nil)
	if err != nil {
		var apiErr *mimirapi.Error
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusMethodNotAllowed) {
			for _, harnessName := range []string{"opencode", "hermes"} {
				if _, ok := plugins[harnessName]; ok {
					add(harnessName+".plugin-load", "skipped", "installed plugin is current; active version unknown because the deployed Worker does not report harness loads", "")
				}
			}
			return
		}
		add("harness-loads", "failed", err.Error(), "mimir deploy")
		return
	}
	var response harnessLoadsResponse
	if err := json.Unmarshal(data, &response); err != nil {
		add("harness-loads", "failed", "invalid /integrations/harness-loads response: "+err.Error(), "mimir deploy")
		return
	}
	for _, harnessName := range []string{"opencode", "hermes"} {
		artifact, ok := plugins[harnessName]
		if !ok {
			continue
		}
		load, found := latestHarnessLoad(response.Loads, harnessName, receipt.InstallationID)
		label := "OpenCode"
		if harnessName == "hermes" {
			label = "Hermes"
		}
		if !found || load.ArtifactSHA256 == "" {
			add(harnessName+".plugin-load", "failed", "installed plugin is current, but no active load was reported; restart required", "restart "+label)
		} else if load.ArtifactSHA256 != artifact.BundleHash {
			add(harnessName+".plugin-load", "failed", fmt.Sprintf("installed plugin is current (%s), but active plugin is %s; restart required", artifact.BundleHash, load.ArtifactSHA256), "restart "+label)
		} else {
			add(harnessName+".plugin-load", "ok", "plugin is installed, active, and current · sha256 "+artifact.BundleHash, "")
		}
	}
}

func artifactUsable(status install.ArtifactStatus) bool {
	switch status {
	case install.ArtifactCurrent, install.ArtifactInstalled, install.ArtifactAdopted, install.ArtifactMigrated, install.ArtifactUpdated:
		return true
	default:
		return false
	}
}

func latestHarnessLoad(loads []harnessLoad, harnessName, installationID string) (harnessLoad, bool) {
	var latest harnessLoad
	found := false
	for _, load := range loads {
		if load.Harness != harnessName || (installationID != "" && load.InstallationID != installationID) {
			continue
		}
		if !found || load.ReportedAt > latest.ReportedAt {
			latest, found = load, true
		}
	}
	return latest, found
}

type workerIdentity struct {
	Service       string   `json:"service"`
	APIVersion    int      `json:"api_version"`
	Capabilities  []string `json:"capabilities"`
	BundleVersion string   `json:"bundle_version"`
	BundleSHA256  string   `json:"bundle_sha256"`
}

func ValidateWorkerIdentity(data []byte) error {
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
