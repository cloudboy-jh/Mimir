package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
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
	LookPath       func(string) (string, error)
	LookupEnv      func(string) (string, bool)
	Lifecycle      lifecycle.Service
}

func New(api Requester) Service {
	return Service{
		API: api, CheckArtifacts: install.CheckArtifacts, LoadReceipt: install.LoadReceipt, LoadPointer: mimirapi.LoadPointer,
		LookPath: exec.LookPath, LookupEnv: os.LookupEnv,
		Lifecycle: lifecycle.New(),
	}
}

// providerCredentialEnvVars is the supported Pi provider credential list.
var providerCredentialEnvVars = [...]string{
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"OPENROUTER_API_KEY",
	"GOOGLE_API_KEY",
	"GEMINI_API_KEY",
	"GROQ_API_KEY",
	"MISTRAL_API_KEY",
	"XAI_API_KEY",
}

// RunTUI runs the ordinary doctor checks and adds readiness checks for the TUI.
func (s Service) RunTUI(ctx context.Context) Report {
	report := s.Run(ctx)
	readiness := s.TUIReadiness()
	report.Checks = append(report.Checks, readiness.Checks...)
	if !readiness.OK {
		report.OK = false
	}
	return report
}

// TUIReadiness checks only local Pi and provider prerequisites.
func (s Service) TUIReadiness() Report {
	report := Report{OK: true}
	add := func(name, status, detail, repair string) {
		report.Checks = append(report.Checks, Check{Name: name, Status: status, Detail: detail, Repair: repair})
		if status == "failed" {
			report.OK = false
		}
	}

	lookPath := s.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if path, err := lookPath("pi"); err != nil {
		add("pi", "failed", "pi was not found on PATH", "install Pi and ensure pi is available on PATH")
	} else {
		add("pi", "ok", path, "")
	}

	lookupEnv := s.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	credential := ""
	for _, name := range providerCredentialEnvVars {
		if value, ok := lookupEnv(name); ok && strings.TrimSpace(value) != "" {
			credential = name
			break
		}
	}
	if credential == "" {
		names := strings.Join(providerCredentialEnvVars[:], ", ")
		add("provider-credential", "warning", "no provider API-key environment variable detected; Pi may use stored OAuth or provider credentials; checked: "+names, "if Pi is not already authenticated, configure a provider in Pi")
	} else {
		add("provider-credential", "ok", credential+" is set", "")
	}

	return report
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
	s.addBinaryJunkCheck(add)
	for _, check := range s.Lifecycle.Hermes.Doctor(ctx, pointer, manifest) {
		add(check.Name, check.Status, check.Detail, check.Repair)
	}
	return report
}

// addBinaryJunkCheck reports stale files sitting next to the receipt-owned
// executable: Mimir swap leftovers (.old, .rollback, orphaned staged temps)
// and foreign junk (.bak, linker ~ files). Mimir-owned leftovers are removed
// automatically once no Mimir process holds them; the rest is the user's call.
func (s Service) addBinaryJunkCheck(add func(string, string, string, string)) {
	receipt, err := s.LoadReceipt()
	if err != nil || receipt.CLI.Path == "" {
		return
	}
	entries, err := os.ReadDir(filepath.Dir(receipt.CLI.Path))
	if err != nil {
		return
	}
	base := filepath.Base(receipt.CLI.Path)
	stale := []string{}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case name == base+".old", name == base+".rollback", name == base+".new", name == base+".bak", name == base+"~":
			stale = append(stale, name)
		case strings.HasPrefix(name, ".mimir-update-"), strings.HasPrefix(name, ".mimir-install-"):
			stale = append(stale, name)
		}
	}
	if len(stale) == 0 {
		return
	}
	sort.Strings(stale)
	add("binary-junk", "warning",
		"stale files next to "+receipt.CLI.Path+": "+strings.Join(stale, ", "),
		"Mimir-owned swap leftovers are removed by a later CLI run once no Mimir process holds them; delete foreign files manually")
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
