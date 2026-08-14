package mimircli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

var installTerminal = isTerminal

func cmdHarness(ctx context.Context, args []string, ioctx IO) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mimir harness <list|enable|disable> [id] [--json]")
	}
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
		return fmt.Errorf("usage: mimir harness <list|enable|disable> [id] [--json]")
	}
	ids, err := installpkg.NormalizeHarnesses([]string{filtered[1]})
	if err != nil || len(ids) != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("harness enable/disable requires one harness id")
	}
	id, enabled := ids[0], filtered[0] == "enable"
	if id == "hermes" && !enabled {
		paths, pathErr := installpkg.Paths()
		if pathErr != nil {
			return pathErr
		}
		if paths.HermesDetected {
			service := lifecycleService()
			if err := service.Hermes.Disable(ctx, paths.HermesHome); err != nil {
				return err
			}
			state := service.Hermes.Uninstall()
			if state.State == "preserved" {
				return fmt.Errorf("removing Hermes route: %s", state.Detail)
			}
		}
	}
	report, err := installpkg.SetHarnessEnabled(id, enabled)
	if err != nil {
		return err
	}
	if enabled {
		if id == "hermes" {
			paths, pathErr := installpkg.Paths()
			if pathErr != nil {
				return pathErr
			}
			if !installpkg.ArtifactsReady(report, paths.HermesHome, "plugins/hermes/", "skills/mimir-use/") {
				_, rollbackErr := installpkg.SetHarnessEnabled(id, false)
				if rollbackErr != nil {
					return fmt.Errorf("Hermes artifacts are not ready or receipt-owned (rolling back Hermes selection: %v)", rollbackErr)
				}
				return fmt.Errorf("Hermes artifacts are not ready or receipt-owned; conflicting or modified files were preserved")
			}
			if paths.HermesDetected {
				if err := lifecycleService().Hermes.Enable(ctx, paths.HermesHome); err != nil {
					_, rollbackErr := installpkg.SetHarnessEnabled(id, false)
					if rollbackErr != nil {
						return fmt.Errorf("%w (rolling back Hermes selection: %v)", err, rollbackErr)
					}
					return err
				}
			}
		}
		lifecycle := refreshConnectedLifecycleIntegrations(ctx, "harness-enable")
		if !lifecycle.OK {
			if id == "hermes" {
				paths, _ := installpkg.Paths()
				service := lifecycleService()
				_ = service.Hermes.Disable(ctx, paths.HermesHome)
				_ = service.Hermes.Uninstall()
				_, _ = installpkg.SetHarnessEnabled(id, false)
			}
			return fmt.Errorf("enabling harness integration: %s", lifecycle.Error)
		}
	}
	result := map[string]any{"harness": id, "enabled": enabled, "artifacts": report}
	if jsonOutput {
		return json.NewEncoder(ioctx.Out).Encode(result)
	}
	action := "disabled"
	if enabled {
		action = "enabled"
	}
	_, err = fmt.Fprintf(ioctx.Out, "%s %s\n", id, action)
	return err
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
