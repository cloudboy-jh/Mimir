package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness"
	hermesintegration "github.com/cloudboy-jh/mimir/internal/harness/hermes"
	openintegration "github.com/cloudboy-jh/mimir/internal/harness/opencode"
	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

type Report struct {
	OK           bool                      `json:"ok"`
	Artifacts    install.ArtifactReport    `json:"artifacts"`
	Integrations harness.IntegrationReport `json:"integrations"`
	Error        string                    `json:"error,omitempty"`
}

type InstallReport struct {
	Binary         install.BinaryReport     `json:"binary"`
	Artifacts      install.ArtifactReport   `json:"artifacts"`
	OpenCode       harness.IntegrationState `json:"opencode"`
	Hermes         harness.IntegrationState `json:"hermes"`
	ClaudeCode     harness.IntegrationState `json:"claude_code"`
	Codex          harness.IntegrationState `json:"codex"`
	Cursor         harness.IntegrationState `json:"cursor"`
	OpenCodeReady  bool                     `json:"open_code_ready"`
	HermesReady    bool                     `json:"hermes_ready"`
	ActionRequired bool                     `json:"action_required"`
}

type UninstallReport struct {
	Operation   string                        `json:"operation"`
	Result      string                        `json:"result"`
	Summary     string                        `json:"summary"`
	ReceiptPath string                        `json:"receipt_path"`
	LogPath     string                        `json:"log_path"`
	Binary      install.UninstallBinaryResult `json:"binary"`
	Hermes      harness.IntegrationState      `json:"hermes"`
	Artifacts   []install.ArtifactResult      `json:"artifacts"`
}

type UpdateReport struct {
	Check        bool                       `json:"check"`
	Binary       install.UpdateBinaryReport `json:"binary"`
	Artifacts    install.ArtifactReport     `json:"artifacts"`
	Integrations harness.IntegrationReport  `json:"integrations,omitempty"`
}

type Service struct {
	RefreshArtifacts         func(string) (install.ArtifactReport, error)
	RefreshPreviouslyManaged func(string) (install.ArtifactReport, error)
	HasManagedReceipt        func() (bool, error)
	Paths                    func() (install.InstallationPaths, error)
	LoadPointer              func() (mimirapi.Pointer, error)
	LoadReceipt              func() (install.Receipt, error)
	InstallFiles             func(string, func() (string, error)) (install.InstallReport, error)
	UninstallFiles           func(bool) (install.UninstallReport, error)
	Hermes                   hermesintegration.Service
	Step                     func(string)
}

func New() Service {
	return Service{
		RefreshArtifacts:         install.RefreshArtifacts,
		RefreshPreviouslyManaged: install.RefreshPreviouslyManagedArtifacts,
		HasManagedReceipt:        install.HasManagedReceipt,
		Paths:                    install.Paths,
		LoadPointer:              mimirapi.LoadPointer,
		LoadReceipt:              install.LoadReceipt,
		InstallFiles:             install.Install,
		UninstallFiles:           install.Uninstall,
		Hermes:                   hermesintegration.New(),
	}
}

func (s Service) Install(ctx context.Context, explicitDir string, executable func() (string, error)) (InstallReport, error) {
	mechanical, err := s.InstallFiles(explicitDir, executable)
	if err != nil {
		return InstallReport{}, err
	}
	s.step("CLI and managed artifacts installed")
	paths, err := s.Paths()
	if err != nil {
		return InstallReport{}, err
	}
	report := InstallReport{Binary: mechanical.Binary, Artifacts: mechanical.Artifacts, HermesReady: true}
	pointer, pointerErr := s.LoadPointer()
	if install.ArtifactsReady(mechanical.Artifacts, paths.OpenCodeHome, openintegration.ArtifactSourcePrefixes()...) {
		report.OpenCode = harness.IntegrationState{State: "staged", Scope: "capture", RestartRequired: true, Detail: "managed OpenCode capture plugin staged; activation is unverified until a load is reported"}
	} else {
		report.OpenCode = harness.IntegrationState{State: "failed", Scope: "capture", Detail: "conflicting or modified OpenCode files were preserved"}
	}
	report.OpenCodeReady = report.OpenCode.State != "failed"
	report.ClaudeCode = hookArtifactState(mechanical.Artifacts, paths.ClaudeCodeHome, "plugins/claude-code/", "Claude Code")
	report.Codex = hookArtifactState(mechanical.Artifacts, paths.CodexHome, "plugins/codex/", "Codex")
	report.Cursor = hookArtifactState(mechanical.Artifacts, paths.CursorHome, "plugins/cursor/", "Cursor")
	s.step("OpenCode integration configured")
	if paths.HermesDetected {
		if !install.ArtifactsReady(mechanical.Artifacts, paths.HermesHome, hermesintegration.ArtifactSourcePrefixes()...) {
			report.HermesReady = false
			report.Hermes = harness.IntegrationState{State: "failed", Scope: "all-providers", Detail: "conflicting or modified Hermes plugin files were preserved"}
		} else {
			if pointerErr != nil {
				if err := s.Hermes.Enable(ctx, paths.HermesHome); err != nil {
					return InstallReport{}, err
				}
				report.Hermes = harness.IntegrationState{State: "staged", Scope: "all-providers", RestartRequired: true, Detail: "Mimir capture plugin staged; activation is unverified until a load is reported; connect Mimir to install the OpenRouter route"}
			} else {
				manifest, err := s.Manifest(pointer.URL)
				if err != nil {
					return InstallReport{}, err
				}
				installed, err := s.Hermes.Configure(ctx, pointer, manifest)
				if err != nil {
					return InstallReport{}, err
				}
				if !installed {
					return InstallReport{}, fmt.Errorf("Hermes disappeared during installation")
				}
				report.Hermes = harness.IntegrationState{State: "staged", Provider: "openrouter", Scope: "all-providers", RestartRequired: true, Detail: "OpenRouter proxy and direct-provider lifecycle capture staged; activation is unverified until a load is reported"}
			}
		}
	} else {
		report.Hermes = harness.IntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	}
	s.step("Hermes integration checked")
	report.ActionRequired = install.ArtifactIssueCount(mechanical.Artifacts) > 0 || !report.OpenCodeReady || !report.HermesReady || integrationStaged(report.OpenCode, report.Hermes, report.ClaudeCode, report.Codex, report.Cursor)
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
	receipt, err := s.LoadReceipt()
	if err != nil {
		return UninstallReport{}, err
	}
	hermesOwned := false
	for _, artifact := range receipt.Artifacts {
		if hermesintegration.OwnsArtifactSource(artifact.Source) {
			hermesOwned = true
			break
		}
	}
	var disableErr error
	if paths.HermesDetected && hermesOwned {
		disableErr = s.Hermes.Disable(ctx, paths.HermesHome)
	}
	mechanical, err := s.UninstallFiles(keepBinary)
	if err != nil {
		return UninstallReport{}, err
	}
	hermesState := s.Hermes.Uninstall()
	if disableErr != nil {
		hermesState.State = "preserved"
		hermesState.Detail = disableErr.Error()
	}
	result := mechanical.Result
	if hermesState.State == "preserved" && result == "success" {
		result = "warning"
	}
	return UninstallReport{
		Operation: mechanical.Operation, Result: result, Summary: mechanical.Summary,
		ReceiptPath: mechanical.ReceiptPath, LogPath: mechanical.LogPath,
		Binary: mechanical.Binary, Hermes: hermesState, Artifacts: mechanical.Artifacts,
	}, nil
}

func (s Service) Update(ctx context.Context, check, force bool) (UpdateReport, error) {
	var lifecycle Report
	mechanical, err := install.Update(ctx, install.UpdateOptions{
		Check: check,
		Force: force,
		Progress: func(message string) {
			s.step(message)
		},
		Refresh: func(ctx context.Context, operation string) (install.ArtifactReport, error) {
			lifecycle = s.Refresh(ctx, operation)
			if !lifecycle.OK {
				return lifecycle.Artifacts, fmt.Errorf("%s", lifecycle.Error)
			}
			return lifecycle.Artifacts, nil
		},
		AfterReplace: func(ctx context.Context, executable string) (install.ArtifactReport, error) {
			command := exec.CommandContext(ctx, executable, "_post-update")
			output, err := command.CombinedOutput()
			if err != nil {
				return install.ArtifactReport{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
			}
			if err := json.Unmarshal(output, &lifecycle); err != nil {
				return install.ArtifactReport{}, fmt.Errorf("reading updated integration report: %w", err)
			}
			if !lifecycle.OK {
				return lifecycle.Artifacts, fmt.Errorf("%s", lifecycle.Error)
			}
			return lifecycle.Artifacts, nil
		},
	})
	if err != nil {
		return UpdateReport{}, err
	}
	return UpdateReport{
		Check: mechanical.Check, Binary: mechanical.Binary, Artifacts: mechanical.Artifacts,
		Integrations: lifecycle.Integrations,
	}, nil
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

func (s Service) Refresh(ctx context.Context, operation string) Report {
	report := Report{OK: true}
	artifacts, err := s.RefreshArtifacts(operation)
	report.Artifacts = artifacts
	if err != nil {
		report.OK = false
		report.Error = fmt.Sprintf("refreshing managed artifacts: %v", err)
	}
	return s.finish(ctx, report)
}

func (s Service) RefreshConnected(ctx context.Context, operation string) Report {
	report := Report{OK: true}
	artifacts, err := s.RefreshPreviouslyManaged(operation)
	report.Artifacts = artifacts
	if err != nil {
		report.OK = false
		report.Error = fmt.Sprintf("refreshing managed artifacts: %v", err)
		return report
	}
	managed, err := s.HasManagedReceipt()
	if err != nil {
		report.OK = false
		report.Error = fmt.Sprintf("reading managed installation: %v", err)
		return report
	}
	if !managed {
		report.Integrations = harness.IntegrationReport{
			OpenCode:   harness.IntegrationState{State: "skipped", Detail: "no managed installation receipt; setup and login do not enroll artifacts"},
			Hermes:     harness.IntegrationState{State: "skipped", Detail: "no managed installation receipt"},
			ClaudeCode: harness.IntegrationState{State: "skipped", Detail: "no managed installation receipt"},
			Codex:      harness.IntegrationState{State: "skipped", Detail: "no managed installation receipt"},
			Cursor:     harness.IntegrationState{State: "skipped", Detail: "no managed installation receipt"},
		}
		return report
	}
	return s.finish(ctx, report)
}

func (s Service) finish(ctx context.Context, report Report) Report {
	pointer, err := s.LoadPointer()
	if err != nil {
		report.Integrations = harness.IntegrationReport{
			OpenCode:   harness.IntegrationState{State: "skipped", Detail: "Mimir is not connected"},
			Hermes:     harness.IntegrationState{State: "skipped", Detail: "Mimir is not connected"},
			ClaudeCode: harness.IntegrationState{State: "skipped", Detail: "Mimir is not connected"},
			Codex:      harness.IntegrationState{State: "skipped", Detail: "Mimir is not connected"},
			Cursor:     harness.IntegrationState{State: "skipped", Detail: "Mimir is not connected"},
		}
		return report
	}
	integrations, err := s.InstallCurrent(ctx, pointer, report.Artifacts)
	report.Integrations = integrations
	if err != nil {
		report.OK = false
		if report.Error != "" {
			report.Error += "; "
		}
		report.Error += fmt.Sprintf("refreshing harness configuration: %v", err)
	}
	return report
}

func (s Service) InstallCurrent(ctx context.Context, pointer mimirapi.Pointer, artifacts install.ArtifactReport) (harness.IntegrationReport, error) {
	report := harness.IntegrationReport{}
	var failures []string
	paths, err := s.Paths()
	if err != nil {
		return report, err
	}
	manifest, err := s.Manifest(pointer.URL)
	if err != nil {
		return report, err
	}
	if install.ArtifactsReady(artifacts, paths.OpenCodeHome, openintegration.ArtifactSourcePrefixes()...) {
		report.OpenCode = harness.IntegrationState{State: "staged", Scope: "capture", RestartRequired: true, Detail: "managed OpenCode capture plugin staged; activation is unverified until a load is reported"}
	} else {
		report.OpenCode = harness.IntegrationState{State: "failed", Scope: "capture", Detail: "conflicting or modified OpenCode files were preserved"}
		failures = append(failures, report.OpenCode.Detail)
	}
	report.ClaudeCode = hookArtifactState(artifacts, paths.ClaudeCodeHome, "plugins/claude-code/", "Claude Code")
	report.Codex = hookArtifactState(artifacts, paths.CodexHome, "plugins/codex/", "Codex")
	report.Cursor = hookArtifactState(artifacts, paths.CursorHome, "plugins/cursor/", "Cursor")
	if _, found, discoverErr := s.Hermes.Discover(); discoverErr != nil {
		failures = append(failures, discoverErr.Error())
	} else if !found {
		report.Hermes = harness.IntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	} else if !install.ArtifactsReady(artifacts, paths.HermesHome, hermesintegration.ArtifactSourcePrefixes()...) {
		report.Hermes = harness.IntegrationState{State: "failed", Scope: "all-providers", Detail: "conflicting or modified Hermes plugin files were preserved"}
		failures = append(failures, report.Hermes.Detail)
	} else if installed, configureErr := s.Hermes.Configure(ctx, pointer, manifest); configureErr != nil {
		report.Hermes = harness.IntegrationState{State: "failed", Provider: "openrouter", Scope: "openrouter", Detail: configureErr.Error()}
		failures = append(failures, configureErr.Error())
	} else if installed {
		report.Hermes = harness.IntegrationState{State: "staged", Provider: "openrouter", Scope: "all-providers", RestartRequired: true, Detail: "OpenRouter proxy and direct-provider lifecycle capture staged; activation is unverified until a load is reported"}
	} else {
		report.Hermes = harness.IntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	}
	if len(failures) > 0 {
		return report, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return report, nil
}

func hookArtifactState(artifacts install.ArtifactReport, root, prefix, label string) harness.IntegrationState {
	if root == "" {
		return harness.IntegrationState{State: "skipped", Detail: label + " hook path is unavailable"}
	}
	if install.ArtifactsReady(artifacts, root, prefix) {
		state := harness.IntegrationState{State: "staged", Scope: "hooks", RestartRequired: true, Detail: "managed " + label + " capture hooks staged; activation is unverified until a load is reported"}
		if label == "Cursor" {
			state.RestartRequired = false
			state.Detail += "; Cursor reloads hooks.json automatically"
		}
		return state
	}
	return harness.IntegrationState{State: "preserved", Scope: "hooks", Detail: "existing " + label + " hook files are user-owned or modified; Mimir hooks were not installed"}
}

func integrationStaged(states ...harness.IntegrationState) bool {
	for _, state := range states {
		if state.State == "staged" {
			return true
		}
	}
	return false
}
