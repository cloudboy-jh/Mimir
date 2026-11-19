package lifecycle

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness"
	hermesintegration "github.com/cloudboy-jh/mimir/internal/harness/hermes"
	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

const notInstalled = "no managed installation; run mimir install"

type Report struct {
	OK           bool                      `json:"ok"`
	Artifacts    install.ArtifactReport    `json:"artifacts"`
	Integrations harness.IntegrationReport `json:"integrations"`
	Error        string                    `json:"error,omitempty"`
}

type InstallReport struct {
	Binary         install.BinaryReport      `json:"binary"`
	Artifacts      install.ArtifactReport    `json:"artifacts"`
	Integrations   harness.IntegrationReport `json:"integrations"`
	Warnings       []string                  `json:"warnings,omitempty"`
	ActionRequired bool                      `json:"action_required"`
}

type UninstallReport struct {
	ManifestPath string                        `json:"manifest_path"`
	Binary       install.UninstallBinaryResult `json:"binary"`
	Hermes       harness.IntegrationState      `json:"hermes"`
	Artifacts    []install.ArtifactResult      `json:"artifacts"`
}

type Service struct {
	Paths          func() (install.InstallationPaths, error)
	LoadPointer    func() (mimirapi.Pointer, error)
	LoadManifest   func() (install.Manifest, error)
	InstallFiles   func(string, []string, func() (string, error)) (install.InstallReport, error)
	SelectFiles    func(string, []string) (install.ArtifactReport, error)
	RefreshFiles   func(string) (install.ArtifactReport, error)
	UninstallFiles func(bool) (install.UninstallReport, error)
	SaveExecutable func(string) error
	Hermes         hermesintegration.Service
	Step           func(string)
}

func New() Service {
	return Service{
		Paths:          install.Paths,
		LoadPointer:    mimirapi.LoadPointer,
		LoadManifest:   install.LoadManifest,
		InstallFiles:   install.Install,
		SelectFiles:    install.SelectArtifacts,
		RefreshFiles:   install.RefreshArtifacts,
		UninstallFiles: install.Uninstall,
		SaveExecutable: mimirapi.SaveExecutable,
		Hermes:         hermesintegration.New(),
	}
}

// Install installs the binary and the selected harnesses (nil keeps the
// recorded selection), then configures harness integrations.
func (s Service) Install(ctx context.Context, explicitDir string, selected []string, executable func() (string, error)) (InstallReport, error) {
	if err := ctx.Err(); err != nil {
		return InstallReport{}, err
	}
	prior, err := s.LoadManifest()
	if err != nil {
		return InstallReport{}, err
	}
	paths, err := s.Paths()
	if err != nil {
		return InstallReport{}, err
	}
	wasHermes := contains(prior.Harnesses, "hermes")
	if selected != nil && wasHermes && !contains(selected, "hermes") && paths.HermesDetected {
		if err := s.Hermes.Disable(ctx, paths.HermesHome); err != nil {
			return InstallReport{}, err
		}
		if state := s.Hermes.Uninstall(); state.State == "preserved" {
			return InstallReport{}, fmt.Errorf("removing Hermes route: %s", state.Detail)
		}
	}
	mechanical, err := s.InstallFiles(explicitDir, selected, executable)
	if err != nil {
		return InstallReport{}, err
	}
	s.step("CLI and managed files installed")
	ctx = context.WithoutCancel(ctx)
	report := InstallReport{Binary: mechanical.Binary, Artifacts: mechanical.Artifacts}
	if err := s.SaveExecutable(mechanical.Binary.Path); err != nil {
		report.Warnings = append(report.Warnings, "recording executable path: "+err.Error())
	}
	manifest, err := s.LoadManifest()
	if err != nil {
		return InstallReport{}, err
	}
	integrations, configureErr := s.configure(ctx, paths, manifest, mechanical.Artifacts)
	if configureErr != nil && !wasHermes && contains(manifest.Harnesses, "hermes") {
		// Hermes was newly selected but could not be configured: roll back to the prior selection.
		var rollback []string
		if paths.HermesDetected {
			if err := s.Hermes.Disable(ctx, paths.HermesHome); err != nil {
				rollback = append(rollback, err.Error())
			}
			if state := s.Hermes.Uninstall(); state.State == "preserved" {
				rollback = append(rollback, state.Detail)
			}
		}
		if _, err := s.SelectFiles("install", prior.Harnesses); err != nil {
			rollback = append(rollback, err.Error())
		}
		if len(rollback) > 0 {
			return InstallReport{}, fmt.Errorf("%w (Hermes rollback incomplete: %s)", configureErr, strings.Join(rollback, "; "))
		}
		return InstallReport{}, configureErr
	}
	if configureErr != nil {
		report.Warnings = append(report.Warnings, configureErr.Error())
	}
	s.step("Harness integrations configured")
	report.Integrations = integrations
	report.ActionRequired = install.ArtifactIssueCount(mechanical.Artifacts) > 0 || configureErr != nil || needsAction(integrations)
	return report, nil
}

func (s Service) step(message string) {
	if s.Step != nil {
		s.Step(message)
	}
}

func (s Service) Uninstall(ctx context.Context, keepBinary bool) (UninstallReport, error) {
	paths, err := s.Paths()
	if err != nil {
		return UninstallReport{}, err
	}
	manifest, err := s.LoadManifest()
	if err != nil {
		return UninstallReport{}, err
	}
	var disableErr error
	if paths.HermesDetected && ownsFileUnder(manifest, filepath.Join(paths.HermesHome, "plugins", "mimir")) {
		disableErr = s.Hermes.Disable(ctx, paths.HermesHome)
	}
	mechanical, err := s.UninstallFiles(keepBinary)
	if err != nil {
		return UninstallReport{}, err
	}
	hermesState := s.Hermes.Uninstall()
	if disableErr != nil {
		hermesState.State, hermesState.Detail = "preserved", disableErr.Error()
	}
	return UninstallReport{ManifestPath: mechanical.ManifestPath, Binary: mechanical.Binary, Hermes: hermesState, Artifacts: mechanical.Artifacts}, nil
}

func (s Service) Manifest(url string) (harness.ConnectionManifest, error) {
	credential, err := mimirapi.TokenPath()
	if err != nil {
		return harness.ConnectionManifest{}, err
	}
	base := strings.TrimRight(url, "/")
	return harness.ConnectionManifest{
		OpenAIBaseURL:     base + "/v1",
		AnthropicBaseURL:  base,
		CredentialFile:    credential,
		CredentialCommand: []string{"cat", credential},
		OptionalHeaders:   []string{"x-mimir-session", "x-mimir-repo", "x-mimir-harness", "x-mimir-git-ref", "x-mimir-request-kind"},
	}, nil
}

// RefreshConnected rewrites the recorded harness files and reconfigures
// integrations after setup or login. It never enrolls new harnesses.
func (s Service) RefreshConnected(ctx context.Context, operation string) Report {
	report := Report{OK: true}
	fail := func(format string, err error) Report {
		report.OK = false
		report.Error = fmt.Sprintf(format, err)
		return report
	}
	manifest, err := s.LoadManifest()
	if err != nil {
		return fail("reading managed installation: %v", err)
	}
	if len(manifest.Harnesses) == 0 {
		skipped := harness.IntegrationState{State: "skipped", Detail: notInstalled}
		report.Integrations = harness.IntegrationReport{Pi: skipped, OhMyPi: skipped, OpenCode: skipped, Hermes: skipped, ClaudeCode: skipped, Codex: skipped, Cursor: skipped}
		return report
	}
	if report.Artifacts, err = s.RefreshFiles(operation); err != nil {
		return fail("refreshing managed files: %v", err)
	}
	paths, err := s.Paths()
	if err != nil {
		return fail("resolving install paths: %v", err)
	}
	if report.Integrations, err = s.configure(ctx, paths, manifest, report.Artifacts); err != nil {
		return fail("refreshing harness configuration: %v", err)
	}
	return report
}

// configure derives each harness state from its managed files and wires the
// Hermes OpenRouter route when Mimir is connected.
func (s Service) configure(ctx context.Context, paths install.InstallationPaths, manifest install.Manifest, artifacts install.ArtifactReport) (harness.IntegrationReport, error) {
	selected := map[string]bool{}
	for _, id := range manifest.Harnesses {
		selected[id] = true
	}
	files := func(id, label, scope, detail string, restart bool) harness.IntegrationState {
		switch {
		case !selected[id]:
			return harness.IntegrationState{State: "skipped", Detail: "harness is not selected"}
		case !install.ArtifactsReady(artifacts, id):
			return harness.IntegrationState{State: "failed", Scope: scope, Detail: "conflicting or modified " + label + " files were preserved"}
		}
		return harness.IntegrationState{State: "staged", Scope: scope, RestartRequired: restart, Detail: detail}
	}
	report := harness.IntegrationReport{
		Pi:         files("pi", "Pi", "all-providers", "managed Pi capture extension staged; restart Pi to activate it", true),
		OhMyPi:     files("oh-my-pi", "Oh My Pi", "all-providers", "managed Oh My Pi capture extension staged; restart Oh My Pi to activate it", true),
		OpenCode:   files("opencode", "OpenCode", "capture", "managed OpenCode capture plugin staged; restart OpenCode to activate it", true),
		ClaudeCode: files("claude-code", "Claude Code", "hooks", "managed Claude Code hooks and MCP server staged; run /reload-plugins or restart Claude Code", true),
		Codex:      files("codex", "Codex", "hooks", "managed Codex plugin staged; trust its hooks in /hooks, then restart Codex", true),
		Cursor:     files("cursor", "Cursor", "hooks", "managed Cursor hooks staged; Cursor reloads hooks.json automatically", false),
	}
	switch {
	case !selected["hermes"]:
		report.Hermes = harness.IntegrationState{State: "skipped", Detail: "harness is not selected"}
	case !paths.HermesDetected:
		report.Hermes = harness.IntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	default:
		report.Hermes = files("hermes", "Hermes", "all-providers", "", true)
		if report.Hermes.State != "staged" {
			break
		}
		pointer, err := s.LoadPointer()
		if err != nil {
			if err := s.Hermes.Enable(ctx, paths.HermesHome); err != nil {
				report.Hermes = harness.IntegrationState{State: "failed", Scope: "all-providers", Detail: err.Error()}
				return report, err
			}
			report.Hermes.Detail = "Mimir capture plugin staged; connect Mimir to install the OpenRouter route"
			break
		}
		connection, err := s.Manifest(pointer.URL)
		if err == nil && manifest.InstallationID == "" {
			err = fmt.Errorf("managed installation has no installation ID")
		}
		if err == nil {
			var installed bool
			if installed, err = s.Hermes.Configure(ctx, pointer, connection, manifest.InstallationID); err == nil && !installed {
				err = fmt.Errorf("Hermes disappeared during configuration")
			}
		}
		if err != nil {
			report.Hermes = harness.IntegrationState{State: "failed", Provider: "openrouter", Scope: "openrouter", Detail: err.Error()}
			return report, err
		}
		report.Hermes.Provider = "openrouter"
		report.Hermes.Detail = "OpenRouter proxy and direct-provider capture staged; restart Hermes to activate it"
	}
	return report, nil
}

func needsAction(report harness.IntegrationReport) bool {
	for _, state := range []harness.IntegrationState{report.Pi, report.OhMyPi, report.OpenCode, report.Hermes, report.ClaudeCode, report.Codex, report.Cursor} {
		if state.State == "staged" || state.State == "failed" {
			return true
		}
	}
	return false
}

func ownsFileUnder(manifest install.Manifest, dir string) bool {
	for path := range manifest.Files {
		if rel, err := filepath.Rel(dir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
