package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	"github.com/cloudboy-jh/mimir/internal/tailnet"
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
		case strings.HasPrefix(name, "managed-file "):
			name = strings.TrimPrefix(name, "managed-file ")
			structured.Installed[name] = append(structured.Installed[name], state)
			continue
		case name == "managed-files":
			structured.Installed[name] = append(structured.Installed[name], state)
			continue
		case name == "worker" || strings.HasPrefix(name, "worker."):
			section = structured.Deployed
		case name == "connection" || strings.HasPrefix(name, "connection.") || name == "tailnet":
			section = structured.Connection
		case strings.HasSuffix(name, ".plugin-load") || name == "harness-loads":
			section = structured.Active
		}
		section[name] = state
	}
	return structured
}

type Service struct {
	API            Requester
	CheckArtifacts func() (install.ArtifactReport, error)
	LoadManifest   func() (install.Manifest, error)
	LoadPointer    func() (mimirapi.Pointer, error)
	ReportTailnet  func(context.Context, tailnet.Requester) (bool, error)
	Lifecycle      lifecycle.Service
}

func New(api Requester) Service {
	return Service{
		API: api, CheckArtifacts: install.CheckArtifacts, LoadManifest: install.LoadManifest,
		LoadPointer: mimirapi.LoadPointer, ReportTailnet: tailnet.Report, Lifecycle: lifecycle.New(),
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
	artifacts, err := s.CheckArtifacts()
	if err != nil {
		add("managed-files", "failed", err.Error(), "mimir install")
	} else {
		addArtifactChecks(artifacts, add)
	}
	manifest, manifestErr := s.LoadManifest()
	if manifestErr != nil {
		add("managed-files", "failed", manifestErr.Error(), "mimir install")
	}
	pointer, err := s.LoadPointer()
	if err != nil {
		add("connection", "failed", err.Error(), "mimir login")
		return report
	}
	data, err := s.API.Request(ctx, http.MethodGet, "/whoami", nil)
	if err != nil {
		add("worker", "failed", err.Error(), "mimir login")
	} else if err := ValidateWorkerIdentity(data); err != nil {
		add("worker", "failed", err.Error(), "mimir deploy")
	} else {
		add("worker", "ok", pointer.URL, "")
		var identity workerIdentity
		_ = json.Unmarshal(data, &identity)
		expected, identityErr := install.EmbeddedWorkerIdentity()
		switch {
		case identityErr != nil:
			add("worker.bundle", "failed", identityErr.Error(), "mimir deploy")
		case identity.BundleSHA256 == "":
			add("worker.bundle", "skipped", "deployed Worker bundle identity is unknown", "mimir deploy")
		case identity.BundleSHA256 != expected.SHA256:
			add("worker.bundle", "failed", fmt.Sprintf("installed %s (%s), deployed %s (%s)", expected.Version, expected.SHA256, identity.BundleVersion, identity.BundleSHA256), "mimir deploy")
		default:
			add("worker.bundle", "ok", fmt.Sprintf("installed and deployed %s · sha256 %s", expected.Version, expected.SHA256), "")
		}
		if manifestErr == nil {
			s.addHarnessLoadChecks(ctx, artifacts, manifest, add)
		}
	}
	switch reported, err := s.ReportTailnet(ctx, s.API); {
	case err != nil:
		add("tailnet", "failed", err.Error(), "check tailscale status, then mimir doctor")
	case reported:
		add("tailnet", "ok", "reported", "")
	default:
		add("tailnet", "skipped", "tailscale not available", "")
	}
	if manifestErr != nil || !contains(manifest.Harnesses, "hermes") {
		return report
	}
	connection, err := s.Lifecycle.Manifest(pointer.URL)
	if err != nil {
		add("connection.manifest", "failed", err.Error(), "mimir login")
		return report
	}
	for _, check := range s.Lifecycle.Hermes.Doctor(ctx, pointer, connection, manifest.InstallationID) {
		add(check.Name, check.Status, check.Detail, check.Repair)
	}
	return report
}

// addArtifactChecks reports each managed file. An MCP configuration Mimir
// could not write is a manual step, not a failure.
func addArtifactChecks(artifacts install.ArtifactReport, add func(string, string, string, string)) {
	for _, artifact := range artifacts.Artifacts {
		detail := string(artifact.Status) + " · " + artifact.Path
		switch {
		case install.ArtifactUsable(artifact.Status):
			add("managed-file "+artifact.Source, "ok", detail, "")
		case artifact.Snippet != "":
			add(artifact.Harness+".mcp", "warning", detail, "add the mimir server to "+artifact.Path+": "+artifact.Snippet)
		case artifact.Status == install.ArtifactMissing:
			add("managed-file "+artifact.Source, "failed", detail, "mimir install")
		default:
			add("managed-file "+artifact.Source, "failed", detail, "review the preserved file; remove or restore it, then run mimir install")
		}
	}
}

type harnessLoad struct {
	Harness        string `json:"harness"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	InstallationID string `json:"installation_id"`
	ReportedAt     string `json:"reported_at"`
}

// loadSources maps each harness to the file whose hash its loads report.
var loadSources = []struct{ harness, source, activate string }{
	{"pi", "plugins/pi/mimir.ts", "restart Pi"},
	{"oh-my-pi", "plugins/oh-my-pi/mimir.ts", "restart Oh My Pi"},
	{"opencode", "plugins/opencode/mimir.ts", "restart OpenCode"},
	{"hermes", "plugins/hermes/__init__.py", "restart Hermes"},
	{"claude-code", "plugins/claude-code/hooks/hooks.json", "run /reload-plugins in Claude Code or restart Claude Code"},
	{"codex", "plugins/codex/hooks/hooks.json", "review and trust the Mimir plugin hooks in /hooks, then restart Codex"},
	{"cursor", "plugins/cursor/hooks.json", "open or continue a Cursor agent session; Cursor reloads hooks.json automatically"},
}

// addHarnessLoadChecks compares the hash each harness last reported loading
// with the hash managed.json recorded for that file.
func (s Service) addHarnessLoadChecks(ctx context.Context, artifacts install.ArtifactReport, manifest install.Manifest, add func(string, string, string, string)) {
	recorded := map[string]string{}
	for _, artifact := range artifacts.Artifacts {
		if hash := manifest.Files[artifact.Path]; hash != "" && install.ArtifactUsable(artifact.Status) {
			recorded[artifact.Source] = hash
		}
	}
	var expected []int
	for i, entry := range loadSources {
		if recorded[entry.source] != "" {
			expected = append(expected, i)
		}
	}
	if len(expected) == 0 {
		return
	}
	data, err := s.API.Request(ctx, http.MethodGet, "/integrations/harness-loads", nil)
	if err != nil {
		var apiErr *mimirapi.Error
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusMethodNotAllowed) {
			add("harness-loads", "skipped", "the deployed Worker does not report harness loads", "mimir deploy")
			return
		}
		add("harness-loads", "failed", err.Error(), "mimir deploy")
		return
	}
	var response struct {
		Loads []harnessLoad `json:"loads"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		add("harness-loads", "failed", "invalid /integrations/harness-loads response: "+err.Error(), "mimir deploy")
		return
	}
	for _, i := range expected {
		entry := loadSources[i]
		hash := recorded[entry.source]
		load, found := latestHarnessLoad(response.Loads, entry.harness, manifest.InstallationID)
		switch {
		case !found || load.ArtifactSHA256 == "":
			add(entry.harness+".plugin-load", "failed", "installed, but no active load was reported", entry.activate)
		case load.ArtifactSHA256 != hash:
			add(entry.harness+".plugin-load", "failed", fmt.Sprintf("installed %s, but active integration is %s", hash, load.ArtifactSHA256), entry.activate)
		default:
			add(entry.harness+".plugin-load", "ok", "installed, active, and current · sha256 "+hash, "")
		}
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
	for _, required := range []string{"query_api", "hermes_authorization", "session_events", "session_lifecycle"} {
		if !contains(identity.Capabilities, required) {
			return fmt.Errorf("deployed Worker lacks required capability %s", required)
		}
	}
	return nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
