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
	Pi             harness.IntegrationState `json:"pi"`
	OhMyPi         harness.IntegrationState `json:"oh_my_pi"`
	OpenCode       harness.IntegrationState `json:"opencode"`
	Hermes         harness.IntegrationState `json:"hermes"`
	ClaudeCode     harness.IntegrationState `json:"claude_code"`
	Codex          harness.IntegrationState `json:"codex"`
	Cursor         harness.IntegrationState `json:"cursor"`
	PiReady        bool                     `json:"pi_ready"`
	OhMyPiReady    bool                     `json:"oh_my_pi_ready"`
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
	Warning      string                     `json:"warning,omitempty"`
}

type Service struct {
	RefreshArtifacts         func(string) (install.ArtifactReport, error)
	RefreshPreviouslyManaged func(string) (install.ArtifactReport, error)
	HasManagedReceipt        func() (bool, error)
	Paths                    func() (install.InstallationPaths, error)
	LoadPointer              func() (mimirapi.Pointer, error)
	LoadReceipt              func() (install.Receipt, error)
	InstallFiles             func(string, func() (string, error)) (install.InstallReport, error)
	InstallHarnessFiles      func(string, []string, func() (string, error)) (install.InstallReport, error)
	RefreshSelected          func(string, []string) (install.ArtifactReport, error)
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
		InstallHarnessFiles:      install.InstallHarnesses,
		RefreshSelected:          install.RefreshSelectedArtifacts,
		UninstallFiles:           install.Uninstall,
		Hermes:                   hermesintegration.New(),
	}
}

func (s Service) Install(ctx context.Context, explicitDir string, executable func() (string, error)) (InstallReport, error) {
	return s.InstallSelected(ctx, explicitDir, nil, executable)
}

func (s Service) InstallSelected(ctx context.Context, explicitDir string, selected []string, executable func() (string, error)) (InstallReport, error) {
	if err := ctx.Err(); err != nil {
		return InstallReport{}, err
	}
	var prior []string
	hermesDeselected := false
	if selected != nil {
		receipt, err := s.LoadReceipt()
		if err != nil {
			return InstallReport{}, err
		}
		prior = append([]string(nil), receipt.Harnesses...)
		hermesDeselected = selectedHarnessSet(prior)["hermes"] && !selectedHarnessSet(selected)["hermes"]
		if hermesDeselected {
			paths, err := s.Paths()
			if err != nil {
				return InstallReport{}, err
			}
			if paths.HermesDetected {
				if err := s.Hermes.Disable(ctx, paths.HermesHome); err != nil {
					return InstallReport{}, err
				}
				if state := s.Hermes.Uninstall(); state.State == "preserved" {
					if enableErr := s.Hermes.Enable(ctx, paths.HermesHome); enableErr != nil {
						return InstallReport{}, fmt.Errorf("removing Hermes route: %s (restoring Hermes plugin: %v)", state.Detail, enableErr)
					}
					return InstallReport{}, fmt.Errorf("removing Hermes route: %s", state.Detail)
				}
			}
		}
	}
	var mechanical install.InstallReport
	var err error
	if selected == nil {
		mechanical, err = s.InstallFiles(explicitDir, executable)
	} else {
		mechanical, err = s.InstallHarnessFiles(explicitDir, selected, executable)
	}
	if err != nil {
		if hermesDeselected {
			paths, pathErr := s.Paths()
			if pathErr == nil && paths.HermesDetected {
				if restoreErr := s.restoreHermes(ctx, paths); restoreErr != nil {
					return InstallReport{}, fmt.Errorf("%w (restoring Hermes after install failure: %v)", err, restoreErr)
				}
			}
		}
		return InstallReport{}, err
	}
	s.step("CLI and managed artifacts installed")
	ctx = context.WithoutCancel(ctx)
	receipt, receiptErr := s.LoadReceipt()
	paths, err := s.Paths()
	if err != nil {
		return InstallReport{}, err
	}
	selectedSet := selectedHarnessSet(selected)
	if selected == nil {
		selectedSet = allHarnessSet()
		if receiptErr == nil && receipt.Harnesses != nil {
			selectedSet = selectedHarnessSet(receipt.Harnesses)
		}
	}
	report := InstallReport{Binary: mechanical.Binary, Artifacts: mechanical.Artifacts, HermesReady: true}
	pointer, pointerErr := s.LoadPointer()
	if !selectedSet["pi"] {
		report.Pi = unselectedState()
	} else if paths.PiHome == "" {
		report.Pi = harness.IntegrationState{State: "skipped", Detail: "Pi extension path is unavailable"}
	} else if install.ArtifactsReady(mechanical.Artifacts, paths.PiHome, "plugins/pi/") {
		report.Pi = harness.IntegrationState{State: "staged", Provider: "openrouter", Scope: "all-providers", RestartRequired: true, Detail: "managed Pi capture extension staged; restart Pi to route OpenRouter and capture direct-provider turns"}
	} else {
		report.Pi = harness.IntegrationState{State: "failed", Scope: "capture", Detail: "conflicting or modified Pi extension files were preserved"}
	}
	report.PiReady = report.Pi.State != "failed"
	if !selectedSet["oh-my-pi"] {
		report.OhMyPi = unselectedState()
	} else if paths.OhMyPiHome == "" {
		report.OhMyPi = harness.IntegrationState{State: "skipped", Detail: "Oh My Pi extension path is unavailable"}
	} else if install.ArtifactsReady(mechanical.Artifacts, paths.OhMyPiHome, "plugins/oh-my-pi/") {
		report.OhMyPi = harness.IntegrationState{State: "staged", Provider: "openrouter", Scope: "all-providers", RestartRequired: true, Detail: "managed Oh My Pi capture extension staged; restart Oh My Pi to activate it"}
	} else {
		report.OhMyPi = harness.IntegrationState{State: "failed", Scope: "capture", Detail: "conflicting or modified Oh My Pi extension files were preserved"}
	}
	report.OhMyPiReady = report.OhMyPi.State != "failed"
	if !selectedSet["opencode"] {
		report.OpenCode = unselectedState()
	} else if install.ArtifactsReady(mechanical.Artifacts, paths.OpenCodeHome, openintegration.ArtifactSourcePrefixes()...) {
		report.OpenCode = harness.IntegrationState{State: "staged", Scope: "capture", RestartRequired: true, Detail: "managed OpenCode capture plugin staged; activation is unverified until a load is reported"}
	} else {
		report.OpenCode = harness.IntegrationState{State: "failed", Scope: "capture", Detail: "conflicting or modified OpenCode files were preserved"}
	}
	report.OpenCodeReady = report.OpenCode.State != "failed"
	report.ClaudeCode = selectedHookState(selectedSet["claude-code"], mechanical.Artifacts, paths.ClaudeCodeHome, "plugins/claude-code/", "Claude Code")
	report.Codex = selectedHookState(selectedSet["codex"], mechanical.Artifacts, paths.CodexHome, "plugins/codex/", "Codex")
	report.Cursor = selectedHookState(selectedSet["cursor"], mechanical.Artifacts, paths.CursorHome, "plugins/cursor/", "Cursor")
	s.step("OpenCode integration configured")
	if !selectedSet["hermes"] {
		report.Hermes = unselectedState()
	} else if paths.HermesDetected {
		if !install.ArtifactsReady(mechanical.Artifacts, paths.HermesHome, hermesintegration.ArtifactSourcePrefixes()...) {
			report.HermesReady = false
			report.Hermes = harness.IntegrationState{State: "failed", Scope: "all-providers", Detail: "conflicting or modified Hermes plugin files were preserved"}
			if err := s.rollbackHermesSelection(ctx, paths, prior, selected, false, fmt.Errorf("%s", report.Hermes.Detail)); err != nil && strings.Contains(err.Error(), "rollback incomplete") {
				return InstallReport{}, err
			}
		} else {
			if pointerErr != nil {
				if err := s.Hermes.Enable(ctx, paths.HermesHome); err != nil {
					return InstallReport{}, s.rollbackHermesSelection(ctx, paths, prior, selected, true, err)
				}
				report.Hermes = harness.IntegrationState{State: "staged", Scope: "all-providers", RestartRequired: true, Detail: "Mimir capture plugin staged; activation is unverified until a load is reported; connect Mimir to install the OpenRouter route"}
			} else {
				if receiptErr != nil {
					return InstallReport{}, s.rollbackHermesSelection(ctx, paths, prior, selected, true, receiptErr)
				}
				if receipt.InstallationID == "" {
					return InstallReport{}, s.rollbackHermesSelection(ctx, paths, prior, selected, true, fmt.Errorf("managed installation receipt has no installation ID"))
				}
				manifest, err := s.Manifest(pointer.URL)
				if err != nil {
					return InstallReport{}, s.rollbackHermesSelection(ctx, paths, prior, selected, true, err)
				}
				installed, err := s.Hermes.Configure(ctx, pointer, manifest, receipt.InstallationID)
				if err != nil {
					return InstallReport{}, s.rollbackHermesSelection(ctx, paths, prior, selected, true, err)
				}
				if !installed {
					return InstallReport{}, s.rollbackHermesSelection(ctx, paths, prior, selected, true, fmt.Errorf("Hermes disappeared during installation"))
				}
				report.Hermes = harness.IntegrationState{State: "staged", Provider: "openrouter", Scope: "all-providers", RestartRequired: true, Detail: "OpenRouter proxy and direct-provider lifecycle capture staged; activation is unverified until a load is reported"}
			}
		}
	} else {
		report.Hermes = harness.IntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	}
	s.step("Hermes integration checked")
	report.ActionRequired = install.ArtifactIssueCount(mechanical.Artifacts) > 0 || !report.PiReady || !report.OhMyPiReady || !report.OpenCodeReady || !report.HermesReady || integrationStaged(report.Pi, report.OhMyPi, report.OpenCode, report.Hermes, report.ClaudeCode, report.Codex, report.Cursor)
	return report, nil
}

func (s Service) rollbackHermesSelection(ctx context.Context, paths install.InstallationPaths, prior, selected []string, teardown bool, cause error) error {
	if selected == nil || selectedHarnessSet(prior)["hermes"] || !selectedHarnessSet(selected)["hermes"] {
		return cause
	}
	var rollback []string
	if teardown && paths.HermesDetected {
		if err := s.Hermes.Disable(ctx, paths.HermesHome); err != nil {
			rollback = append(rollback, err.Error())
		}
		if state := s.Hermes.Uninstall(); state.State == "preserved" {
			rollback = append(rollback, state.Detail)
		}
	}
	if _, err := s.RefreshSelected("install-rollback", prior); err != nil {
		rollback = append(rollback, err.Error())
	}
	if len(rollback) > 0 {
		return fmt.Errorf("%w (Hermes rollback incomplete: %s)", cause, strings.Join(rollback, "; "))
	}
	return cause
}

func (s Service) restoreHermes(ctx context.Context, paths install.InstallationPaths) error {
	pointer, err := s.LoadPointer()
	if err != nil {
		return s.Hermes.Enable(ctx, paths.HermesHome)
	}
	manifest, err := s.Manifest(pointer.URL)
	if err != nil {
		return err
	}
	receipt, err := s.LoadReceipt()
	if err != nil {
		return err
	}
	installed, err := s.Hermes.Configure(ctx, pointer, manifest, receipt.InstallationID)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("Hermes disappeared while restoring its integration")
	}
	return nil
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
			Pi:         harness.IntegrationState{State: "skipped", Detail: "no managed installation receipt; setup and login do not enroll artifacts"},
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
			Pi:         harness.IntegrationState{State: "skipped", Detail: "Mimir is not connected"},
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
	selected := allHarnessSet()
	receipt, receiptErr := s.LoadReceipt()
	if receiptErr == nil && receipt.Harnesses != nil {
		selected = selectedHarnessSet(receipt.Harnesses)
	}
	if !selected["pi"] {
		report.Pi = unselectedState()
	} else if paths.PiHome == "" {
		report.Pi = harness.IntegrationState{State: "skipped", Detail: "Pi extension path is unavailable"}
	} else if install.ArtifactsReady(artifacts, paths.PiHome, "plugins/pi/") {
		report.Pi = harness.IntegrationState{State: "staged", Provider: "openrouter", Scope: "all-providers", RestartRequired: true, Detail: "managed Pi capture extension staged; activation is unverified until a load is reported"}
	} else {
		report.Pi = harness.IntegrationState{State: "failed", Scope: "capture", Detail: "conflicting or modified Pi extension files were preserved"}
		failures = append(failures, report.Pi.Detail)
	}
	if !selected["opencode"] {
		report.OpenCode = unselectedState()
	} else if install.ArtifactsReady(artifacts, paths.OpenCodeHome, openintegration.ArtifactSourcePrefixes()...) {
		report.OpenCode = harness.IntegrationState{State: "staged", Scope: "capture", RestartRequired: true, Detail: "managed OpenCode capture plugin staged; activation is unverified until a load is reported"}
	} else {
		report.OpenCode = harness.IntegrationState{State: "failed", Scope: "capture", Detail: "conflicting or modified OpenCode files were preserved"}
		failures = append(failures, report.OpenCode.Detail)
	}
	if !selected["oh-my-pi"] {
		report.OhMyPi = unselectedState()
	} else if install.ArtifactsReady(artifacts, paths.OhMyPiHome, "plugins/oh-my-pi/") {
		report.OhMyPi = harness.IntegrationState{State: "staged", Provider: "openrouter", Scope: "all-providers", RestartRequired: true, Detail: "managed Oh My Pi capture extension staged; activation is unverified until a load is reported"}
	} else {
		report.OhMyPi = harness.IntegrationState{State: "failed", Scope: "capture", Detail: "conflicting or modified Oh My Pi extension files were preserved"}
		failures = append(failures, report.OhMyPi.Detail)
	}
	report.ClaudeCode = selectedHookState(selected["claude-code"], artifacts, paths.ClaudeCodeHome, "plugins/claude-code/", "Claude Code")
	report.Codex = selectedHookState(selected["codex"], artifacts, paths.CodexHome, "plugins/codex/", "Codex")
	report.Cursor = selectedHookState(selected["cursor"], artifacts, paths.CursorHome, "plugins/cursor/", "Cursor")
	if !selected["hermes"] {
		report.Hermes = unselectedState()
	} else if _, found, discoverErr := s.Hermes.Discover(); discoverErr != nil {
		failures = append(failures, discoverErr.Error())
	} else if !found {
		report.Hermes = harness.IntegrationState{State: "skipped", Detail: "Hermes is not installed"}
	} else if !install.ArtifactsReady(artifacts, paths.HermesHome, hermesintegration.ArtifactSourcePrefixes()...) {
		report.Hermes = harness.IntegrationState{State: "failed", Scope: "all-providers", Detail: "conflicting or modified Hermes plugin files were preserved"}
		failures = append(failures, report.Hermes.Detail)
	} else if receiptErr != nil {
		report.Hermes = harness.IntegrationState{State: "failed", Provider: "openrouter", Scope: "openrouter", Detail: receiptErr.Error()}
		failures = append(failures, receiptErr.Error())
	} else if receipt.InstallationID == "" {
		report.Hermes = harness.IntegrationState{State: "failed", Provider: "openrouter", Scope: "openrouter", Detail: "managed installation receipt has no installation ID"}
		failures = append(failures, report.Hermes.Detail)
	} else if installed, configureErr := s.Hermes.Configure(ctx, pointer, manifest, receipt.InstallationID); configureErr != nil {
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

func selectedHarnessSet(selected []string) map[string]bool {
	result := map[string]bool{}
	for _, id := range selected {
		result[id] = true
	}
	return result
}

func allHarnessSet() map[string]bool {
	return map[string]bool{"opencode": true, "pi": true, "oh-my-pi": true, "hermes": true, "claude-code": true, "codex": true, "cursor": true}
}

func unselectedState() harness.IntegrationState {
	return harness.IntegrationState{State: "skipped", Detail: "harness is not selected"}
}

func selectedHookState(selected bool, artifacts install.ArtifactReport, root, prefix, label string) harness.IntegrationState {
	if !selected {
		return unselectedState()
	}
	return hookArtifactState(artifacts, root, prefix, label)
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
