package mimircli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	doctorpkg "github.com/cloudboy-jh/mimir/internal/doctor"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/ui/selector"
)

var installTerminal = isTerminal
var runHarnessSelector = selector.Run
var harnessSelectorAvailable = selector.Available

var runHarnessDoctor = func(ctx context.Context) doctorpkg.Report {
	service := doctorpkg.New(apiRequester{})
	service.Lifecycle = lifecycleService()
	return service.Run(ctx)
}

type harnessView struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Selected         bool   `json:"selected"`
	Detected         bool   `json:"detected"`
	Installed        bool   `json:"installed"`
	Active           bool   `json:"active"`
	Status           string `json:"status"`
	Detail           string `json:"detail,omitempty"`
	ActivationAction string `json:"activation_action,omitempty"`
}

func cmdHarness(ctx context.Context, args []string, ioctx IO) error {
	jsonOutput := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	configureInstall()
	if len(filtered) == 0 {
		return cmdHarnessOverview(ctx, ioctx, jsonOutput)
	}
	if len(filtered) == 1 && filtered[0] == "list" {
		harnesses, err := installpkg.Harnesses()
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(ioctx.Out).Encode(harnesses)
		}
		for _, harness := range harnesses {
			marker := "○"
			if harness.Selected {
				marker = "●"
			}
			detected := ""
			if harness.Detected {
				detected = " (detected)"
			}
			if _, err := fmt.Fprintf(ioctx.Out, "%s %s%s\n", marker, harness.Name, detected); err != nil {
				return err
			}
		}
		return nil
	}
	if len(filtered) != 2 || (filtered[0] != "enable" && filtered[0] != "disable") {
		return fmt.Errorf("usage: mimir harness [--json] | mimir harness <list|enable|disable> [id] [--json]")
	}
	ids, err := installpkg.NormalizeHarnesses([]string{filtered[1]})
	if err != nil || len(ids) != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("harness enable/disable requires one harness id")
	}
	id, enabled := ids[0], filtered[0] == "enable"
	return setHarnessEnabled(ctx, id, enabled, jsonOutput, false, ioctx.Out)
}

func cmdPrimaryHarness(ctx context.Context, command string, args []string, ioctx IO) error {
	filtered, jsonOutput := stripJSONFlag(args)
	if len(filtered) == 0 {
		return fmt.Errorf("usage: mimir %s <pi|opencode|hermes> [--json]", command)
	}
	id, err := primaryHarnessID(strings.Join(filtered, " "))
	if err != nil {
		return err
	}
	return setHarnessEnabled(ctx, id, command == "enable", jsonOutput, true, ioctx.Out)
}

func primaryHarnessID(value string) (string, error) {
	normalized := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '-', '_':
			return -1
		default:
			return r
		}
	}, strings.ToLower(strings.TrimSpace(value)))
	switch normalized {
	case "pi", "opencode", "hermes":
		return normalized, nil
	default:
		return "", fmt.Errorf("unknown primary harness %q; use mimir harness to manage other integrations", value)
	}
}

func setHarnessEnabled(ctx context.Context, id string, enabled, jsonOutput, friendlyName bool, out io.Writer) error {
	wasEnabled, err := harnessSelected(id)
	if err != nil {
		return err
	}
	hermesService := lifecycleService()
	hermesTeardown := false
	if id == "hermes" && !enabled && wasEnabled {
		paths, pathErr := installpkg.Paths()
		if pathErr != nil {
			return pathErr
		}
		if paths.HermesDetected {
			if err := hermesService.Hermes.Disable(ctx, paths.HermesHome); err != nil {
				return err
			}
			hermesTeardown = true
		}
	}
	report, err := installpkg.SetHarnessEnabled(id, enabled)
	if err != nil {
		if hermesTeardown {
			return harnessRollbackError(err, restoreHermesDisable(ctx))
		}
		return err
	}
	if hermesTeardown {
		state := hermesService.Hermes.Uninstall()
		if state.State == "preserved" {
			rollbackErr := restoreHermesDisable(ctx)
			return harnessRollbackError(fmt.Errorf("removing Hermes route: %s", state.Detail), rollbackErr)
		}
	}
	if enabled {
		if id == "hermes" {
			paths, pathErr := installpkg.Paths()
			if pathErr != nil {
				return pathErr
			}
			if !installpkg.ArtifactsReady(report, paths.HermesHome, "plugins/hermes/", "skills/mimir-use/") {
				rollbackErr := rollbackHarnessEnable(ctx, id, wasEnabled, false)
				return harnessRollbackError(fmt.Errorf("Hermes artifacts are not ready or receipt-owned; conflicting or modified files were preserved"), rollbackErr)
			}
			if paths.HermesDetected {
				if err := lifecycleService().Hermes.Enable(ctx, paths.HermesHome); err != nil {
					return harnessRollbackError(err, rollbackHarnessEnable(ctx, id, wasEnabled, false))
				}
			}
		}
		lifecycle := refreshConnectedLifecycleIntegrations(ctx, "harness-enable")
		if !lifecycle.OK {
			return harnessRollbackError(fmt.Errorf("enabling harness integration: %s", lifecycle.Error), rollbackHarnessEnable(ctx, id, wasEnabled, id == "hermes"))
		}
	}
	result := map[string]any{"harness": id, "enabled": enabled, "artifacts": report}
	if jsonOutput {
		return json.NewEncoder(out).Encode(result)
	}
	action := "disabled"
	if enabled {
		action = "enabled"
	}
	name := id
	if friendlyName {
		name = harnessDisplayName(id)
	}
	_, err = fmt.Fprintf(out, "%s %s\n", name, action)
	return err
}

func harnessSelected(id string) (bool, error) {
	harnesses, err := installpkg.Harnesses()
	if err != nil {
		return false, err
	}
	for _, harness := range harnesses {
		if harness.ID == id {
			return harness.Selected, nil
		}
	}
	return false, fmt.Errorf("unknown harness %q", id)
}

func rollbackHarnessEnable(ctx context.Context, id string, wasEnabled, teardownHermes bool) error {
	if wasEnabled {
		return nil
	}
	if id == "hermes" {
		paths, _ := installpkg.Paths()
		service := lifecycleService()
		var failures []string
		if teardownHermes && paths.HermesDetected {
			if err := service.Hermes.Disable(ctx, paths.HermesHome); err != nil {
				failures = append(failures, err.Error())
			}
			if state := service.Hermes.Uninstall(); state.State == "preserved" {
				failures = append(failures, state.Detail)
			}
		}
		if _, err := installpkg.SetHarnessEnabled(id, false); err != nil {
			failures = append(failures, err.Error())
		}
		if len(failures) > 0 {
			return fmt.Errorf("%s", strings.Join(failures, "; "))
		}
		return nil
	}
	_, err := installpkg.SetHarnessEnabled(id, false)
	return err
}

func restoreHermesDisable(ctx context.Context) error {
	var failures []string
	if _, err := installpkg.SetHarnessEnabled("hermes", true); err != nil {
		failures = append(failures, err.Error())
	}
	paths, err := installpkg.Paths()
	if err != nil {
		failures = append(failures, err.Error())
	} else if paths.HermesDetected {
		if err := lifecycleService().Hermes.Enable(ctx, paths.HermesHome); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func harnessRollbackError(cause, rollbackErr error) error {
	if rollbackErr != nil {
		return fmt.Errorf("%w (restoring harness selection: %v)", cause, rollbackErr)
	}
	return cause
}

func cmdHarnessOverview(ctx context.Context, ioctx IO, jsonOutput bool) error {
	harnesses, err := installpkg.Harnesses()
	if err != nil {
		return err
	}
	artifacts, artifactErr := installpkg.CheckArtifacts()
	paths, pathsErr := installpkg.Paths()
	if pathsErr != nil {
		return pathsErr
	}
	receipt, receiptErr := installpkg.LoadReceipt()
	preserved := map[string]string{}
	if receiptErr == nil {
		preserved = preservedHarnessArtifacts(receipt, paths)
	}
	active := map[string]doctorpkg.State{}
	diagnostics := map[string]doctorpkg.State{}
	if artifactErr == nil && selectedHarnessExists(harnesses) {
		structured := runHarnessDoctor(ctx).Structured()
		active = structured.Active
		diagnostics = structured.Diagnostics
	}
	views := harnessViews(harnesses, artifacts, artifactErr, active, diagnostics, preserved, paths)
	if jsonOutput {
		return json.NewEncoder(ioctx.Out).Encode(views)
	}
	if len(views) == 0 {
		return renderHarnessViews(ioctx.Out, views)
	}
	in, inputTTY := ioctx.In.(*os.File)
	out, outputTTY := ioctx.Out.(*os.File)
	if !inputTTY || !outputTTY || !installTerminal(in) || !installTerminal(out) || !harnessSelectorAvailable(in, out) {
		return renderHarnessViews(ioctx.Out, views)
	}
	items := make([]selector.Item, len(views))
	for i, view := range views {
		items[i] = selector.Item{
			Label:    fmt.Sprintf("%-16s %s", view.Name, harnessStatus(view)),
			Selected: view.Selected,
		}
	}
	result, err := runHarnessSelector(in, out, "Mimir harnesses", items)
	if err != nil {
		return err
	}
	if !result.Accepted {
		return nil
	}
	if len(result.Selected) != len(views) {
		return errors.New("harness selector returned an invalid selection")
	}
	selected := make([]string, 0, len(result.Selected))
	for i, enabled := range result.Selected {
		if enabled {
			selected = append(selected, views[i].ID)
		}
	}
	return applyHarnessSelection(ctx, harnesses, selected, ioctx.Out)
}

func selectedHarnessExists(harnesses []installpkg.Harness) bool {
	for _, harness := range harnesses {
		if harness.Selected {
			return true
		}
	}
	return false
}

func harnessViews(harnesses []installpkg.Harness, artifacts installpkg.ArtifactReport, artifactErr error, active, diagnostics map[string]doctorpkg.State, preserved map[string]string, paths installpkg.InstallationPaths) []harnessView {
	views := make([]harnessView, 0, len(harnesses))
	for _, harness := range harnesses {
		preservedDetail, hasPreserved := preserved[harness.ID]
		if !harness.Selected && !harness.Detected && !hasPreserved {
			continue
		}
		view := harnessView{ID: harness.ID, Name: harness.Name, Selected: harness.Selected, Detected: harness.Detected}
		found, healthy, artifactDetail := harnessArtifactsState(harness.ID, artifacts, paths)
		if !harness.Selected {
			if hasPreserved {
				view.Installed = true
				view.Status, view.Detail = "needs_attention", preservedDetail
			} else if found {
				view.Installed = true
				view.Status, view.Detail = "needs_attention", artifactDetail
			} else {
				view.Status = "detected"
			}
			views = append(views, view)
			continue
		}
		if artifactErr != nil {
			view.Status, view.Detail = "needs_attention", artifactErr.Error()
			views = append(views, view)
			continue
		}
		if !found || !healthy {
			view.Status, view.Detail = "needs_attention", artifactDetail
			views = append(views, view)
			continue
		}
		view.Installed = true
		if detail, failed := harnessDiagnosticFailure(harness.ID, diagnostics); failed {
			view.Status, view.Detail = "needs_attention", detail
			views = append(views, view)
			continue
		}
		state, found := active[harness.ID+".plugin-load"]
		switch {
		case found && state.Status == "ok":
			view.Status, view.Active, view.Detail = "active", true, state.Detail
		case found && state.Status == "failed":
			view.Status, view.Detail = "installed", state.Detail
			view.ActivationAction = state.Repair
		default:
			view.Status = "activation_unknown"
			view.Detail = "installed; activation could not be verified"
			view.ActivationAction = activationAction(harness.ID)
		}
		views = append(views, view)
	}
	return views
}

func harnessDiagnosticFailure(id string, diagnostics map[string]doctorpkg.State) (string, bool) {
	if id != "hermes" {
		return "", false
	}
	names := make([]string, 0, len(diagnostics))
	for name := range diagnostics {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		state := diagnostics[name]
		if strings.HasPrefix(name, "hermes.") && state.Status == "failed" {
			return state.Detail, true
		}
	}
	return "", false
}

func preservedHarnessArtifacts(receipt installpkg.Receipt, paths installpkg.InstallationPaths) map[string]string {
	preserved := map[string]string{}
	selected := map[string]bool{}
	for _, id := range receipt.Harnesses {
		selected[id] = true
	}
	for target, artifact := range receipt.Artifacts {
		id := harnessArtifactOwner(artifact.Source, target, paths)
		if id == "" || selected[id] {
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			preserved[id] = "modified managed files were preserved during removal"
		}
	}
	return preserved
}

func harnessArtifactsState(id string, report installpkg.ArtifactReport, paths installpkg.InstallationPaths) (found, healthy bool, detail string) {
	for _, artifact := range report.Artifacts {
		if harnessArtifactOwner(artifact.Source, artifact.Path, paths) != id {
			continue
		}
		found = true
		switch artifact.Status {
		case installpkg.ArtifactCurrent, installpkg.ArtifactInstalled, installpkg.ArtifactAdopted, installpkg.ArtifactMigrated, installpkg.ArtifactUpdated:
		default:
			return true, false, string(artifact.Status) + " · " + artifact.Path
		}
	}
	if !found {
		return false, false, "managed integration artifacts are missing"
	}
	return true, true, ""
}

func harnessArtifactOwner(source, target string, paths installpkg.InstallationPaths) string {
	source = strings.ReplaceAll(source, "\\", "/")
	for _, harness := range installpkg.CanonicalHarnesses() {
		if strings.HasPrefix(source, "plugins/"+harness.ID+"/") {
			return harness.ID
		}
	}
	if strings.HasPrefix(source, "skills/") {
		if pathWithinHarnessRoot(filepath.Join(paths.HermesHome, "skills"), target) {
			return "hermes"
		}
		if pathWithinHarnessRoot(filepath.Join(paths.OpenCodeHome, "skills"), target) {
			return "opencode"
		}
	}
	return ""
}

func pathWithinHarnessRoot(root, target string) bool {
	if root == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func renderHarnessViews(out io.Writer, views []harnessView) error {
	if _, err := fmt.Fprintln(out, "Mimir harnesses"); err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(out, "\nNo harnesses detected.")
		return err
	}
	for _, view := range views {
		marker := "○"
		if view.Selected {
			marker = "●"
		}
		if _, err := fmt.Fprintf(out, "%s %-16s %s\n", marker, view.Name, harnessStatus(view)); err != nil {
			return err
		}
	}
	return nil
}

func harnessStatus(view harnessView) string {
	status := "Detected"
	switch view.Status {
	case "active":
		status = "Active"
	case "installed":
		status = "Installed"
		if view.ActivationAction != "" {
			status += ", " + view.ActivationAction + " to activate"
		}
	case "activation_unknown":
		status = "Installed, activation unknown"
	case "needs_attention":
		status = "Needs attention"
	}
	return status
}

func applyHarnessSelection(ctx context.Context, current []installpkg.Harness, selected []string, out io.Writer) error {
	desired := map[string]bool{}
	for _, id := range selected {
		desired[id] = true
	}
	for _, harness := range current {
		if !harness.Selected && desired[harness.ID] {
			if err := setHarnessEnabled(ctx, harness.ID, true, false, true, out); err != nil {
				rollbackErr := restoreHarnessSelection(ctx, current, out)
				if rollbackErr != nil {
					return fmt.Errorf("%w (restoring harness selection: %v)", err, rollbackErr)
				}
				return err
			}
		}
	}
	for _, harness := range current {
		if harness.Selected && !desired[harness.ID] {
			if err := setHarnessEnabled(ctx, harness.ID, false, false, true, out); err != nil {
				rollbackErr := restoreHarnessSelection(ctx, current, out)
				if rollbackErr != nil {
					return fmt.Errorf("%w (restoring harness selection: %v)", err, rollbackErr)
				}
				return err
			}
		}
	}
	return nil
}

func restoreHarnessSelection(ctx context.Context, original []installpkg.Harness, out io.Writer) error {
	actual, err := installpkg.Harnesses()
	if err != nil {
		return err
	}
	originalSelected := map[string]bool{}
	actualSelected := map[string]bool{}
	for _, harness := range original {
		originalSelected[harness.ID] = harness.Selected
	}
	for _, harness := range actual {
		actualSelected[harness.ID] = harness.Selected
	}
	var failures []string
	for _, harness := range original {
		if harness.Selected && !actualSelected[harness.ID] {
			if err := setHarnessEnabled(ctx, harness.ID, true, false, true, out); err != nil {
				failures = append(failures, harness.Name+": "+err.Error())
			}
		}
	}
	for _, harness := range actual {
		if harness.Selected && !originalSelected[harness.ID] {
			if err := setHarnessEnabled(ctx, harness.ID, false, false, true, out); err != nil {
				failures = append(failures, harness.Name+": "+err.Error())
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
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

func parseInstallHarnesses(args []string) (remaining, selected []string, explicit bool, err error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--harness":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, nil, false, fmt.Errorf("--harness requires a value")
			}
			explicit = true
			selected = append(selected, args[i+1])
			i++
		case strings.HasPrefix(args[i], "--harness="):
			explicit = true
			selected = append(selected, strings.TrimPrefix(args[i], "--harness="))
		default:
			remaining = append(remaining, args[i])
		}
	}
	if explicit {
		selected, err = installpkg.NormalizeHarnesses(selected)
	}
	return remaining, selected, explicit, err
}

func installHarnessSelection(ioctx IO, jsonOutput, explicit bool, selected []string) ([]string, error) {
	if explicit {
		return selected, nil
	}
	in, inputTTY := ioctx.In.(*os.File)
	out, outputTTY := ioctx.Out.(*os.File)
	if jsonOutput || !inputTTY || !outputTTY || !installTerminal(in) || !installTerminal(out) {
		return nil, fmt.Errorf("noninteractive installs require at least one --harness <id> (or --harness all)")
	}
	defaults, err := installpkg.DetectedHarnesses()
	if err != nil {
		return nil, err
	}
	defaultSet := map[string]bool{}
	for _, id := range defaults {
		defaultSet[id] = true
	}
	if _, err := fmt.Fprintln(ioctx.Out, "Select harnesses (comma-separated IDs; Enter accepts detected defaults):"); err != nil {
		return nil, err
	}
	for _, harness := range installpkg.CanonicalHarnesses() {
		marker := "○"
		if defaultSet[harness.ID] {
			marker = "●"
		}
		prefix := ""
		if harness.ID == "pi" {
			if _, err := fmt.Fprintln(ioctx.Out, "Pi"); err != nil {
				return nil, err
			}
			prefix = "  "
		} else if harness.ID == "oh-my-pi" {
			prefix = "  "
		}
		if _, err := fmt.Fprintf(ioctx.Out, "%s%s %s (%s)\n", prefix, marker, harness.Name, harness.ID); err != nil {
			return nil, err
		}
	}
	line, err := bufio.NewReader(ioctx.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaults, nil
	}
	return installpkg.NormalizeHarnesses(strings.Split(line, ","))
}
