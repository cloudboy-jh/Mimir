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
	cliui "github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
	operationui "github.com/cloudboy-jh/mimir/internal/ui/operations"
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
	operationCtx, cancelOperation := context.WithCancel(ctx)
	defer cancelOperation()
	var operation *operationui.Progress
	if !check && !jsonOutput {
		operation = operationui.Start(operationCtx, ioctx.In, ioctx.Out, "Mimir update", nil, cancelOperation)
	}
	progress := func(message string) {
		if operation != nil {
			if message == "Replacing Mimir executable" {
				operation.Commit()
			}
			operation.Status(message)
		}
	}
	report, err := runLifecycleUpdate(operationCtx, check, force, progress)
	if err != nil {
		if operation != nil {
			operation.Fail()
			operation.Stop()
		}
		return err
	}
	if operation != nil {
		label := "Update complete"
		if report.Binary.Status == "scheduled" {
			label = "Update scheduled"
		}
		operation.Complete(label)
		operation.Stop()
	}
	if jsonOutput {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	render := cliui.New(ioctx.Out)
	message := ""
	tone := bentotui.ToneNeutral
	switch report.Binary.Status {
	case "current":
		message = fmt.Sprintf("mimir %s is up to date", report.Binary.Current)
		tone = bentotui.ToneSuccess
	case "available":
		message = fmt.Sprintf("mimir %s available (current %s)", report.Binary.Latest, report.Binary.Current)
		tone = bentotui.ToneWarn
	case "updated":
		message = fmt.Sprintf("updated mimir %s → %s", report.Binary.Current, report.Binary.Latest)
		tone = bentotui.ToneSuccess
	case "scheduled":
		message = fmt.Sprintf("update to mimir %s scheduled (current %s)", report.Binary.Latest, report.Binary.Current)
		tone = bentotui.ToneWarn
	}
	blocks := []string{render.Heading("Mimir update"), render.Callout(tone, message, report.Binary.Detail)}
	if report.Binary.Status == "scheduled" {
		blocks = append(blocks, render.ActionHint("mimir update --force", "Stop running Mimir processes and apply the update now."))
	}
	artifactFields := []bentotui.Field{{Label: "Summary", Value: installpkg.ArtifactSummary(report.Artifacts)}}
	if report.Artifacts.ReceiptPath != "" {
		artifactFields = append(artifactFields, bentotui.Field{Label: "Receipt", Value: report.Artifacts.ReceiptPath})
	}
	artifactBlock := render.KeyValues("Managed artifacts", artifactFields...)
	items := make([]cliui.StatusItem, 0, len(report.Artifacts.Artifacts))
	for _, artifact := range report.Artifacts.Artifacts {
		status := string(artifact.Status)
		detail := strings.TrimSpace(strings.Join([]string{artifact.Source, artifact.Detail}, " · "))
		detail = strings.Trim(detail, " ·")
		items = append(items, cliui.StatusItem{Title: artifact.Path, Detail: detail, Stat: status, Tone: cliui.ToneForStatus(status)})
	}
	if len(items) > 0 {
		artifactBlock = bentotui.Join("\n\n", artifactBlock, render.StatusItems(items))
	}
	blocks = append(blocks, artifactBlock)
	if summary := integrationSummary(report.Integrations); strings.TrimSpace(summary) != "" {
		blocks = append(blocks, render.Section("Harness integrations", summary))
	}
	if activation := activationRequired(report.Artifacts, report.Integrations); activation != "" {
		blocks = append(blocks, render.Callout(bentotui.ToneWarn, "Activation required", activation))
	}
	if report.Binary.Status == "updated" {
		blocks = append(blocks, render.Callout(bentotui.ToneInfo, "Deployment", "Worker bundle may be behind this CLI version."), render.ActionHint("mimir deploy", "Deploy the bundled Worker and dashboard."))
	}
	_, err = fmt.Fprintln(ioctx.Out, bentotui.Stack(blocks...))
	return err
}

func activationRequired(artifacts installpkg.ArtifactReport, integrations harness.IntegrationReport) string {
	changed := map[string]bool{}
	for _, artifact := range artifacts.Artifacts {
		if artifact.Status != installpkg.ArtifactInstalled && artifact.Status != installpkg.ArtifactMigrated && artifact.Status != installpkg.ArtifactUpdated {
			continue
		}
		switch {
		case artifact.Source == "plugins/opencode/mimir.ts":
			changed["OpenCode"] = true
		case strings.HasPrefix(artifact.Source, "plugins/hermes/"):
			changed["Hermes"] = true
		}
	}
	var lines []string
	if changed["OpenCode"] && integrations.OpenCode.RestartRequired {
		lines = append(lines, "  OpenCode · restart OpenCode to load the updated managed plugin")
	}
	if changed["Hermes"] && integrations.Hermes.RestartRequired {
		lines = append(lines, "  Hermes · restart Hermes to load the updated managed plugin")
	}
	return strings.Join(lines, "\n")
}
