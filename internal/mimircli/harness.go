package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	doctorpkg "github.com/cloudboy-jh/mimir/internal/doctor"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

var runHarnessDoctor = func(ctx context.Context) doctorpkg.Report {
	service := doctorpkg.New(apiRequester{})
	service.Lifecycle = lifecycleService()
	return service.Run(ctx)
}

type harnessView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Selected   bool   `json:"selected"`
	Detected   bool   `json:"detected"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
	Action     string `json:"action,omitempty"`
	MCPPath    string `json:"mcp_path,omitempty"`
	MCPSnippet string `json:"mcp_snippet,omitempty"`
}

func cmdHarness(ctx context.Context, args []string, ioctx IO) error {
	filtered, jsonOutput := stripJSONFlag(args)
	configureInstall()
	if len(filtered) == 0 {
		return cmdHarnessOverview(ctx, ioctx.Out, jsonOutput)
	}
	if len(filtered) != 2 || (filtered[0] != "enable" && filtered[0] != "disable") {
		return fmt.Errorf("usage: mimir harness [--json] | mimir harness <enable|disable> <id> [--json]")
	}
	ids, err := installpkg.NormalizeHarnesses([]string{filtered[1]})
	if err != nil {
		return err
	}
	if len(ids) != 1 {
		return fmt.Errorf("harness enable/disable requires one harness id")
	}
	return setHarnessEnabled(ctx, ids[0], filtered[0] == "enable", jsonOutput, ioctx.Out)
}

func cmdPrimaryHarness(ctx context.Context, command string, args []string, ioctx IO) error {
	filtered, jsonOutput := stripJSONFlag(args)
	if len(filtered) == 0 {
		return fmt.Errorf("usage: mimir %s <pi|opencode|hermes> [--json]", command)
	}
	normalized := strings.NewReplacer(" ", "", "\t", "", "-", "", "_", "").Replace(strings.ToLower(strings.Join(filtered, "")))
	switch normalized {
	case "pi", "opencode", "hermes":
	default:
		return fmt.Errorf("unknown primary harness %q; use mimir harness to manage other integrations", strings.Join(filtered, " "))
	}
	configureInstall()
	return setHarnessEnabled(ctx, normalized, command == "enable", jsonOutput, ioctx.Out)
}

func setHarnessEnabled(ctx context.Context, id string, enabled, jsonOutput bool, out io.Writer) error {
	manifest, err := installpkg.LoadManifest()
	if err != nil {
		return err
	}
	wasEnabled := false
	for _, selected := range manifest.Harnesses {
		wasEnabled = wasEnabled || selected == id
	}
	hermes := lifecycleService().Hermes
	paths, err := installpkg.Paths()
	if err != nil {
		return err
	}
	if id == "hermes" && !enabled && wasEnabled && paths.HermesDetected {
		if err := hermes.Disable(ctx, paths.HermesHome); err != nil {
			return err
		}
	}
	report, err := installpkg.SetHarnessEnabled(id, enabled)
	if err != nil {
		return err
	}
	if id == "hermes" && !enabled && wasEnabled {
		if state := hermes.Uninstall(); state.State == "preserved" {
			return fmt.Errorf("removing Hermes route: %s", state.Detail)
		}
	}
	if enabled {
		if lifecycle := refreshConnectedLifecycleIntegrations(ctx, "harness-enable"); !lifecycle.OK {
			cause := fmt.Errorf("enabling %s: %s", harnessDisplayName(id), lifecycle.Error)
			if wasEnabled {
				return cause
			}
			if _, err := installpkg.SetHarnessEnabled(id, false); err != nil {
				return fmt.Errorf("%w (restoring harness selection: %v)", cause, err)
			}
			return cause
		}
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(map[string]any{"harness": id, "enabled": enabled, "artifacts": report})
	}
	action := "disabled"
	if enabled {
		action = "enabled"
	}
	lines := []string{harnessDisplayName(id) + " " + action}
	lines = append(lines, manualMCPLines(report)...)
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func cmdHarnessOverview(ctx context.Context, out io.Writer, jsonOutput bool) error {
	harnesses, err := installpkg.Harnesses()
	if err != nil {
		return err
	}
	artifacts, artifactErr := installpkg.CheckArtifacts()
	active := map[string]doctorpkg.State{}
	for _, harness := range harnesses {
		if harness.Selected && artifactErr == nil {
			active = runHarnessDoctor(ctx).Structured().Active
			break
		}
	}
	views := harnessViews(harnesses, artifacts, artifactErr, active)
	if jsonOutput {
		return json.NewEncoder(out).Encode(views)
	}
	lines := []string{"Mimir harnesses"}
	if len(views) == 0 {
		lines = append(lines, "No harnesses detected. Run mimir install --harness <id>.")
	}
	for _, view := range views {
		line := statusLine(view.Status, view.Name, view.Detail)
		if view.Action != "" {
			line += " (" + view.Action + ")"
		}
		lines = append(lines, line)
	}
	lines = append(lines, manualMCPLines(artifacts)...)
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

// harnessViews lists selected or detected harnesses with one status each:
// active, installed (activation pending), needs_attention or detected.
func harnessViews(harnesses []installpkg.Harness, artifacts installpkg.ArtifactReport, artifactErr error, active map[string]doctorpkg.State) []harnessView {
	views := []harnessView{}
	for _, harness := range harnesses {
		if !harness.Selected && !harness.Detected {
			continue
		}
		view := harnessView{ID: harness.ID, Name: harness.Name, Selected: harness.Selected, Detected: harness.Detected, Status: "detected"}
		for _, artifact := range artifacts.Artifacts {
			if artifact.Harness == harness.ID && artifact.Snippet != "" && !installpkg.ArtifactUsable(artifact.Status) {
				view.MCPPath, view.MCPSnippet = artifact.Path, artifact.Snippet
			}
		}
		switch {
		case !harness.Selected:
		case artifactErr != nil:
			view.Status, view.Detail = "needs_attention", artifactErr.Error()
		case !installpkg.ArtifactsReady(artifacts, harness.ID):
			view.Status, view.Detail = "needs_attention", harnessArtifactIssue(artifacts, harness.ID)
			view.Action = "mimir install"
		default:
			state, found := active[harness.ID+".plugin-load"]
			switch {
			case found && state.Status == "ok":
				view.Status = "active"
			case found:
				view.Status, view.Detail, view.Action = "installed", state.Detail, state.Repair
			default:
				view.Status, view.Detail, view.Action = "installed", "activation could not be verified", activationAction(harness.ID)
			}
		}
		views = append(views, view)
	}
	return views
}

func harnessArtifactIssue(report installpkg.ArtifactReport, id string) string {
	for _, artifact := range report.Artifacts {
		if artifact.Harness == id && artifact.Snippet == "" && !installpkg.ArtifactUsable(artifact.Status) {
			return string(artifact.Status) + " · " + artifact.Path
		}
	}
	return "managed integration files are missing"
}

func harnessDisplayName(id string) string {
	for _, harness := range installpkg.CanonicalHarnesses() {
		if harness.ID == id {
			return harness.Name
		}
	}
	return id
}

func activationAction(id string) string {
	switch id {
	case "claude-code":
		return "run /reload-plugins or restart Claude Code"
	case "cursor":
		return "open or continue a Cursor agent session"
	default:
		return "restart " + harnessDisplayName(id)
	}
}
