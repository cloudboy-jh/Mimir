package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	"github.com/cloudboy-jh/mimir/internal/ui/lineoutput"
)

var runLifecycleUpdate = func(ctx context.Context, check, force bool, progress func(string)) (lifecyclepkg.UpdateReport, error) {
	configureInstall()
	service := lifecycleService()
	service.Step = progress
	return service.Update(ctx, check, force)
}

func cmdUpdate(ctx context.Context, args []string, out io.Writer) error {
	return cmdUpdateIO(ctx, args, IO{Out: out})
}

func cmdUpdateIO(ctx context.Context, args []string, ioctx IO) error {
	check, jsonOutput, force := false, false, false
	for _, arg := range args {
		switch arg {
		case "--check":
			check = true
		case "--json":
			jsonOutput = true
		case "--force":
			force = true
		default:
			return fmt.Errorf("usage: mimir update [--check] [--force] [--json]")
		}
	}
	if check && force {
		return fmt.Errorf("--force cannot be combined with --check")
	}
	var guard *interruptGuard
	lines := lineoutput.New(ioctx.Out)
	if !check {
		guard = startInterruptGuard(ctx)
		defer guard.Stop()
		ctx = guard.Context()
		if !jsonOutput {
			_ = lines.Phase("Updating Mimir")
		}
	}
	lastProgress := ""
	progress := func(message string) {
		if guard != nil && message == "Replacing Mimir executable" {
			guard.Commit()
		}
		message = strings.TrimSpace(message)
		if !jsonOutput && message != "" && message != lastProgress {
			_ = lines.Phase(message)
			lastProgress = message
		}
	}
	report, err := runLifecycleUpdate(ctx, check, force, progress)
	if err != nil {
		return err
	}
	if !check {
		if pointer, pointerErr := loadPointer(); pointerErr == nil {
			client := mimirapi.Client{HTTPClient: httpClient, Pointer: pointer}
			if associationErr := associateMachineIdentity(ctx, client); associationErr != nil {
				detail := strings.ReplaceAll(associationErr.Error(), pointer.Token, "[redacted]")
				report.Warning = "machine identity association failed: " + detail
			}
		}
	}
	if jsonOutput {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	return renderUpdate(ioctx.Out, report)
}

func renderUpdate(out io.Writer, report lifecyclepkg.UpdateReport) error {
	message := "Mimir update complete"
	switch report.Binary.Status {
	case "current":
		message = fmt.Sprintf("Mimir is up to date: %s", report.Binary.Current)
	case "available":
		message = fmt.Sprintf("Mimir update available: %s -> %s", report.Binary.Current, report.Binary.Latest)
	case "updated":
		message = fmt.Sprintf("Mimir updated: %s -> %s", report.Binary.Current, report.Binary.Latest)
	case "scheduled":
		message = fmt.Sprintf("Mimir update scheduled: %s -> %s", report.Binary.Current, report.Binary.Latest)
	}
	lines := lineoutput.New(out)
	if err := lines.Success(message); err != nil {
		return err
	}
	if detail := strings.TrimSpace(report.Binary.Detail); detail != "" {
		if err := lines.Detail(detail); err != nil {
			return err
		}
	}
	if warning := strings.TrimSpace(report.Warning); warning != "" {
		if err := lines.Warning(warning); err != nil {
			return err
		}
	}
	integrations := []namedIntegration{
		{name: "Pi", state: report.Integrations.Pi},
		{name: "OpenCode", state: report.Integrations.OpenCode},
		{name: "Hermes", state: report.Integrations.Hermes},
		{name: "Claude Code", state: report.Integrations.ClaudeCode},
		{name: "Codex", state: report.Integrations.Codex},
		{name: "Cursor", state: report.Integrations.Cursor},
	}
	if err := writeAttention(out, report.Artifacts, integrations); err != nil {
		return err
	}
	if restarts := updateRestartNames(report.Artifacts, report.Integrations); len(restarts) > 0 {
		if err := lines.Warning("Restart required: " + strings.Join(restarts, ", ")); err != nil {
			return err
		}
	}
	switch report.Binary.Status {
	case "scheduled":
		return lines.Next("mimir update --force")
	case "updated":
		return lines.Next("mimir deploy")
	}
	return nil
}

func updateRestartNames(artifacts installpkg.ArtifactReport, integrations harness.IntegrationReport) []string {
	changed := map[string]bool{}
	for _, artifact := range artifacts.Artifacts {
		switch artifact.Status {
		case installpkg.ArtifactInstalled, installpkg.ArtifactMigrated, installpkg.ArtifactUpdated, installpkg.ArtifactRemoved:
		default:
			continue
		}
		source := strings.TrimPrefix(strings.ReplaceAll(artifact.Source, "\\", "/"), "./")
		switch {
		case strings.HasPrefix(source, "plugins/pi/"):
			changed["Pi"] = true
		case strings.HasPrefix(source, "plugins/opencode/"):
			changed["OpenCode"] = true
		case strings.HasPrefix(source, "plugins/hermes/"):
			changed["Hermes"] = true
		case strings.HasPrefix(source, "plugins/claude-code/"):
			changed["Claude Code"] = true
		case strings.HasPrefix(source, "plugins/codex/"):
			changed["Codex"] = true
		case strings.HasPrefix(source, "plugins/cursor/"):
			changed["Cursor"] = true
		}
	}
	candidates := []namedIntegration{
		{name: "Pi", state: integrations.Pi},
		{name: "OpenCode", state: integrations.OpenCode},
		{name: "Hermes", state: integrations.Hermes},
		{name: "Claude Code", state: integrations.ClaudeCode},
		{name: "Codex", state: integrations.Codex},
		{name: "Cursor", state: integrations.Cursor},
	}
	names := make([]string, 0, len(candidates))
	for _, integration := range candidates {
		if changed[integration.name] && integration.state.RestartRequired {
			names = append(names, integration.name)
		}
	}
	return names
}
