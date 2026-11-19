package mimircli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

var executablePath = os.Executable

var installTerminal = isTerminal

func configureInstall() {
	installpkg.SetBuildInfo(installpkg.BuildInfo{Version: version, Commit: commit, Date: date})
}

// runLifecycleInstall installs the binary and harness files; a nil selection
// keeps the recorded selection.
var runLifecycleInstall = func(ctx context.Context, binDir string, harnesses []string) (lifecyclepkg.InstallReport, error) {
	configureInstall()
	return lifecycleService().Install(ctx, binDir, harnesses, executablePath)
}

func cmdVersion(args []string, out io.Writer) error {
	jsonOutput := onlyJSONFlag(args)
	if len(args) != 0 && !jsonOutput {
		return fmt.Errorf("usage: mimir version [--json]")
	}
	configureInstall()
	manifest, err := installpkg.LoadManifest()
	if err != nil {
		return err
	}
	paths, err := installpkg.Paths()
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(map[string]any{
			"version": version, "commit": commit, "date": date,
			"bundle_version": manifest.BundleVersion, "manifest_path": paths.Manifest,
		})
	}
	if _, err := fmt.Fprintf(out, "mimir %s (%s, %s)\n", version, commit, date); err != nil {
		return err
	}
	if manifest.BundleVersion != "" {
		_, err = fmt.Fprintf(out, "managed files: bundle %s, %d file(s) in %s\n", manifest.BundleVersion, len(manifest.Files), paths.Manifest)
	}
	return err
}

func cmdInstallIO(ctx context.Context, args []string, ioctx IO) error {
	const usage = "usage: mimir install [--harness <id|all>]... [--bin-dir <dir>] [--json]"
	jsonOutput, binDir, explicit := false, "", false
	var selected []string
	value := func(i int, flag string) (string, error) {
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") || strings.TrimSpace(args[i+1]) == "" {
			return "", fmt.Errorf("%s requires a value", flag)
		}
		return args[i+1], nil
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			jsonOutput = true
		case arg == "--harness" || arg == "--bin-dir":
			v, err := value(i, arg)
			if err != nil {
				return err
			}
			i++
			if arg == "--bin-dir" {
				binDir = v
			} else {
				explicit, selected = true, append(selected, v)
			}
		case strings.HasPrefix(arg, "--harness="):
			explicit, selected = true, append(selected, strings.TrimPrefix(arg, "--harness="))
		case strings.HasPrefix(arg, "--bin-dir="):
			if binDir = strings.TrimPrefix(arg, "--bin-dir="); strings.TrimSpace(binDir) == "" {
				return fmt.Errorf("--bin-dir requires a value")
			}
		default:
			return fmt.Errorf(usage)
		}
	}
	configureInstall()
	if explicit {
		var err error
		if selected, err = installpkg.NormalizeHarnesses(selected); err != nil {
			return err
		}
	} else {
		manifest, err := installpkg.LoadManifest()
		if err != nil {
			return err
		}
		if len(manifest.Harnesses) == 0 {
			if selected, err = promptHarnessSelection(ioctx, jsonOutput); err != nil {
				return err
			}
		}
	}
	report, err := runLifecycleInstall(ctx, binDir, selected)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	return renderInstall(ioctx.Out, report)
}

// promptHarnessSelection asks for comma-separated harness ids on a terminal.
func promptHarnessSelection(ioctx IO, jsonOutput bool) ([]string, error) {
	in, inputTTY := ioctx.In.(*os.File)
	out, outputTTY := ioctx.Out.(*os.File)
	if jsonOutput || !inputTTY || !outputTTY || !installTerminal(in) || !installTerminal(out) {
		return nil, fmt.Errorf("noninteractive installs require at least one --harness <id> (or --harness all)")
	}
	defaults, err := installpkg.DetectedHarnesses()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(installpkg.CanonicalHarnesses()))
	for _, harness := range installpkg.CanonicalHarnesses() {
		ids = append(ids, harness.ID)
	}
	if _, err := fmt.Fprintf(ioctx.Out, "Harnesses: %s\nDetected: %s\nSelect harnesses (comma-separated; Enter accepts detected): ", strings.Join(ids, ", "), strings.Join(defaults, ", ")); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(ioctx.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	if line = strings.TrimSpace(line); line == "" {
		if len(defaults) == 0 {
			return nil, fmt.Errorf("no harness detected; pass --harness <id>")
		}
		return defaults, nil
	}
	return installpkg.NormalizeHarnesses(strings.Split(line, ","))
}

func cmdUninstall(ctx context.Context, args []string, out io.Writer) error {
	keepBinary, jsonOutput := false, false
	for _, arg := range args {
		switch arg {
		case "--keep-binary":
			keepBinary = true
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("usage: mimir uninstall [--keep-binary] [--json]")
		}
	}
	configureInstall()
	report, err := lifecycleService().Uninstall(ctx, keepBinary)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	lines := []string{"Mimir uninstalled"}
	for _, artifact := range report.Artifacts {
		lines = append(lines, statusLine(string(artifact.Status), artifact.Path, artifact.Detail))
	}
	lines = append(lines,
		statusLine(report.Binary.Status, "binary "+report.Binary.Path, report.Binary.Detail),
		statusLine(report.Hermes.State, "Hermes route", report.Hermes.Detail),
		"Connection, local Worker files and the Cloudflare deployment were kept.",
	)
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func statusLine(status, subject, detail string) string {
	line := fmt.Sprintf("  %-9s %s", status, subject)
	if detail = strings.TrimSpace(detail); detail != "" {
		line += ": " + detail
	}
	return line
}
