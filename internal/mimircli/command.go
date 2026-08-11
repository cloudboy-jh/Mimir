package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime/debug"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/codeindex"
	hookintegration "github.com/cloudboy-jh/mimir/internal/harness/hooks"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
	searchpkg "github.com/cloudboy-jh/mimir/internal/search"
	"github.com/cloudboy-jh/mimir/internal/sessions"
	cliui "github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type IO struct {
	In       io.Reader
	Out, Err io.Writer
}

func Execute(ctx context.Context, args []string) error {
	return ExecuteIO(ctx, args, IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
}

func ExecuteIO(ctx context.Context, args []string, ioctx IO) error {
	if ioctx.Out == nil {
		ioctx.Out = os.Stdout
	}
	if ioctx.Err == nil {
		ioctx.Err = os.Stderr
	}
	// Deferred Windows updates are normally completed by their detached
	// helper. Any later CLI command is a second recovery path if that helper
	// timed out or was interrupted. Update handles its own marker so it can
	// report an applied deferred version accurately.
	if len(args) == 0 || (args[0] != "update" && args[0] != "_apply-update" && args[0] != "demo") {
		configureInstall()
		if _, err := installpkg.FinalizePendingUpdate(); err != nil {
			_, _ = fmt.Fprintf(ioctx.Err, "mimir: pending update: %v\n", err)
		}
		installpkg.CleanupStaleUpdateArtifacts()
	}
	if len(args) == 0 {
		return usage(ioctx.Out)
	}
	switch args[0] {
	case "--version":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir --version")
		}
		_, err := fmt.Fprintln(ioctx.Out, versionString())
		return err
	case "version":
		return cmdVersion(args[1:], ioctx.Out)
	case "-h", "--help":
		return usage(ioctx.Out)
	case "help":
		if len(args) == 2 && args[1] == "advanced" {
			return advancedUsage(ioctx.Out)
		}
		return usage(ioctx.Out)
	case "index":
		fs := flag.NewFlagSet("index", flag.ContinueOnError)
		fs.SetOutput(ioctx.Err)
		full := fs.Bool("full", false, "force a full repository index")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		res, err := codeindex.Build(ctx, codeindex.BuildOptions{Dir: ".", Full: *full})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(ioctx.Out, res.Message)
		return err
	case "recall":
		queryArgs, budget, jsonOut, err := parseRecallArgs(args[1:])
		if err != nil {
			return err
		}
		if len(queryArgs) == 0 {
			return fmt.Errorf("usage: mimir recall <query> [--budget 4000] [--json]")
		}
		res, err := runRecall(ctx, recallOptions{Dir: ".", Query: strings.Join(queryArgs, " "), Budget: budget, JSON: jsonOut})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(ioctx.Out, res.Output)
		return err
	case "whoami":
		if !onlyJSONFlag(args[1:]) {
			return fmt.Errorf("usage: mimir whoami [--json]")
		}
		return remotePrint(ctx, ioctx.Out, "GET", "/whoami", nil)
	case "list":
		return cmdListIO(ctx, args[1:], ioctx.In, ioctx.Out)
	case "tui":
		return cmdTUI(ctx, args[1:], ioctx)
	case "sessions":
		if !onlyJSONFlag(args[1:]) {
			return fmt.Errorf("usage: mimir sessions [--json]")
		}
		return remotePrint(ctx, ioctx.Out, "GET", "/sessions", nil)
	case "session":
		return cmdSession(ctx, args[1:], ioctx.Out)
	case "search":
		_, jsonOutput := stripJSONFlag(args[1:])
		query, err := parseSearchArgs(args[1:])
		if err != nil {
			return err
		}
		data, err := searchpkg.New(apiRequester{}).Search(ctx, query)
		if err != nil {
			return err
		}
		if jsonOutput {
			_, err = fmt.Fprintln(ioctx.Out, string(data))
			return err
		}
		return renderSearch(ioctx.Out, data)
	case "mark":
		if len(args) != 3 {
			return fmt.Errorf("usage: mimir mark <session> <landed|discarded|abandoned|unresolved|promoted|unknown>")
		}
		return remotePrint(ctx, ioctx.Out, "POST", "/sessions/"+url.PathEscape(args[1])+"/mark", map[string]string{"outcome": args[2]})
	case "reconcile":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir reconcile")
		}
		data, err := currentSessionService().Reconcile(ctx)
		if err != nil {
			return err
		}
		return printRemoteData(ioctx.Out, data)
	case "config":
		return cmdConfig(ctx, args[1:], ioctx.Out)
	case "setup":
		return setup(ctx, args[1:], ioctx)
	case "deploy":
		return deploy(ctx, args[1:], ioctx)
	case "access":
		return cmdAccess(ctx, args[1:], ioctx)
	case "login":
		return login(ctx, args[1:], ioctx)
	case "install":
		return cmdInstallIO(ctx, args[1:], ioctx)
	case "uninstall":
		return cmdUninstall(ctx, args[1:], ioctx.Out)
	case "dashboard":
		return dashboard(ctx, ioctx)
	case "demo":
		return demo(ctx, args[1:], ioctx)
	case "connection":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir connection")
		}
		return writeConnectionManifest(ioctx.Out)
	case "update":
		return cmdUpdateIO(ctx, args[1:], ioctx)
	case "_apply-update":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir _apply-update")
		}
		return applyPendingUpdateHelper()
	case "_hook":
		if len(args) != 2 {
			return fmt.Errorf("usage: mimir _hook <claude-code|codex|cursor>")
		}
		service, err := hookintegration.New()
		if err != nil {
			return err
		}
		return service.Ingest(ctx, args[1], ioctx.In)
	case "doctor":
		return doctor(ctx, args[1:], ioctx.Out)
	case "_post-update", "_install-integrations":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir _post-update")
		}
		configureInstall()
		report := lifecycleService().Refresh(ctx, "update")
		return json.NewEncoder(ioctx.Out).Encode(report)
	case "outcome":
		if len(args) != 3 || args[1] != "git" {
			return fmt.Errorf("usage: mimir outcome git <session>")
		}
		data, err := currentSessionService().SetGitOutcome(ctx, args[2], checkoutGitEvidence{Dir: "."})
		if err != nil {
			return err
		}
		return printRemoteData(ioctx.Out, data)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type versionReport struct {
	Version        string                            `json:"version"`
	Commit         string                            `json:"commit"`
	Date           string                            `json:"date"`
	BundleVersion  string                            `json:"bundle_version,omitempty"`
	ReceiptPath    string                            `json:"receipt_path"`
	ArtifactCounts map[installpkg.ArtifactStatus]int `json:"artifact_counts"`
}

func cmdVersion(args []string, out io.Writer) error {
	jsonOutput := false
	if len(args) == 1 && args[0] == "--json" {
		jsonOutput = true
	} else if len(args) != 0 {
		return fmt.Errorf("usage: mimir version [--json]")
	}
	configureInstall()
	artifacts, err := installpkg.CheckArtifacts()
	if err != nil {
		return err
	}
	receipt, err := installpkg.LoadReceipt()
	if err != nil {
		return err
	}
	report := versionReport{
		Version: version, Commit: commit, Date: date,
		BundleVersion: receipt.BundleVersion, ReceiptPath: artifacts.ReceiptPath,
		ArtifactCounts: installpkg.ArtifactCounts(artifacts),
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	render := cliui.New(out)
	fields := []bentotui.Field{{Label: "Version", Value: versionString()}}
	if receipt.BundleVersion != "" {
		fields = append(fields, bentotui.Field{Label: "Bundle", Value: receipt.BundleVersion}, bentotui.Field{Label: "Artifacts", Value: installpkg.ArtifactSummary(artifacts)})
	}
	_, err = fmt.Fprintln(out, render.Card("Mimir", fields...))
	return err
}

func cmdInstall(ctx context.Context, args []string, out io.Writer) error {
	return cmdInstallIO(ctx, args, IO{Out: out})
}

func cmdInstallIO(ctx context.Context, args []string, ioctx IO) error {
	jsonOutput, binDir := false, ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			jsonOutput = true
		case args[i] == "--bin-dir" && i+1 < len(args):
			if strings.HasPrefix(args[i+1], "-") {
				return fmt.Errorf("--bin-dir requires a value")
			}
			binDir = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--bin-dir="):
			binDir = strings.TrimPrefix(args[i], "--bin-dir=")
		default:
			return fmt.Errorf("usage: mimir install [--bin-dir <dir>] [--json]")
		}
	}
	if strings.TrimSpace(binDir) == "" && containsBinDirArg(args) {
		return fmt.Errorf("--bin-dir requires a value")
	}
	guard := startInterruptGuard(ctx)
	defer guard.Stop()
	ctx = guard.Context()
	guard.Commit()
	if !jsonOutput {
		fmt.Fprintln(ioctx.Out, "Installing Mimir...")
	}
	report, err := runLifecycleInstall(ctx, binDir, nil)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	return renderInstall(ioctx.Out, report)
}

func containsBinDirArg(args []string) bool {
	for _, arg := range args {
		if arg == "--bin-dir" || strings.HasPrefix(arg, "--bin-dir=") {
			return true
		}
	}
	return false
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
	render := cliui.New(out)
	items := []cliui.StatusItem{{Title: report.Binary.Path, Stat: report.Binary.Status, Tone: cliui.ToneForStatus(report.Binary.Status)}}
	for _, artifact := range report.Artifacts {
		if artifact.Status == installpkg.ArtifactUnowned {
			continue
		}
		status := string(artifact.Status)
		items = append(items, cliui.StatusItem{Title: artifact.Path, Stat: status, Tone: cliui.ToneForStatus(status)})
	}
	items = append(items, cliui.StatusItem{Title: "Hermes", Detail: report.Hermes.Detail, Stat: report.Hermes.State, Tone: cliui.ToneForStatus(report.Hermes.State)})
	_, err = fmt.Fprintf(out, "%s\n\n%s\n\n%s\n", render.Heading("Uninstall complete"), render.StatusItems(items), render.Callout(bentotui.ToneInfo, report.Summary, "Connection, local Worker files, Cloudflare deployment, and install log preserved."))
	return err
}

func cmdSession(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "get" {
		if len(args) < 2 || strings.HasPrefix(args[1], "-") || len(args) > 3 || (len(args) == 3 && args[2] != "--json") {
			return fmt.Errorf("usage: mimir session get <id> [--json]")
		}
		return remotePrint(ctx, out, "GET", "/sessions/"+url.PathEscape(args[1]), nil)
	}
	if len(args) > 0 && args[0] == "status" && (len(args) < 2 || strings.HasPrefix(args[1], "-")) {
		return fmt.Errorf("usage: mimir session status <id> [--json]")
	}
	if len(args) > 0 && args[0] == "end" && len(args) < 2 {
		return fmt.Errorf("usage: mimir session end <id> [--outcome landed|discarded|abandoned|unresolved] [--reason text] [--evidence json]")
	}
	if len(args) > 0 && args[0] == "outcome" && len(args) < 3 {
		return fmt.Errorf("usage: mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text] [--evidence json] [--json]")
	}
	if len(args) == 1 {
		return remotePrint(ctx, out, "GET", "/sessions/"+url.PathEscape(args[0]), nil)
	}
	if len(args) >= 2 && args[0] == "end" {
		parsed, jsonOutput := stripJSONFlag(args[1:])
		id, body, err := parseSessionEndArgs(parsed)
		if err != nil {
			return err
		}
		status, err := currentSessionService().End(ctx, id, body)
		if err != nil {
			return err
		}
		if jsonOutput {
			data, err := sessions.StatusJSON(status)
			if err != nil {
				return err
			}
			return printRemoteData(out, data)
		}
		return renderEndedReceipt(out, status)
	}
	if len(args) >= 2 && args[0] == "status" {
		if len(args) > 3 || (len(args) == 3 && args[2] != "--json") {
			return fmt.Errorf("usage: mimir session status <id> [--json]")
		}
		return printSessionStatus(ctx, out, args[1], len(args) == 3)
	}
	if len(args) >= 3 && args[0] == "outcome" {
		parsed, _ := stripJSONFlag(args[1:])
		id, outcome, reason, evidence, err := parseSessionOutcomeArgs(parsed)
		if err != nil {
			return err
		}
		options := sessions.SetOutcomeOptions{Outcome: outcome, Reason: reason}
		if evidence != nil {
			options.Evidence, options.EvidenceSet = evidence.Value, true
		}
		data, err := currentSessionService().SetOutcome(ctx, id, options)
		if err != nil {
			return err
		}
		return printRemoteData(out, data)
	}
	return fmt.Errorf("usage: mimir session <id> | mimir session status <id> [--json] | mimir session end <id> [--outcome value] [--reason text] [--evidence json] | mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text] [--evidence json]")
}

func parseSessionEndArgs(args []string) (string, sessions.EndOptions, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", sessions.EndOptions{}, fmt.Errorf("usage: mimir session end <id> [--outcome landed|discarded|abandoned|unresolved] [--reason text] [--evidence json]")
	}
	id := args[0]
	options := sessions.EndOptions{}
	hasOutcome, hasReason := false, false
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--outcome" && i+1 < len(args):
			if hasOutcome {
				return "", sessions.EndOptions{}, fmt.Errorf("--outcome may only be specified once")
			}
			hasOutcome, options.Outcome = true, args[i+1]
			i++
		case strings.HasPrefix(args[i], "--outcome="):
			if hasOutcome {
				return "", sessions.EndOptions{}, fmt.Errorf("--outcome may only be specified once")
			}
			hasOutcome, options.Outcome = true, strings.TrimPrefix(args[i], "--outcome=")
		case args[i] == "--reason" && i+1 < len(args):
			if hasReason {
				return "", sessions.EndOptions{}, fmt.Errorf("--reason may only be specified once")
			}
			hasReason, options.Reason = true, args[i+1]
			i++
		case strings.HasPrefix(args[i], "--reason="):
			if hasReason {
				return "", sessions.EndOptions{}, fmt.Errorf("--reason may only be specified once")
			}
			hasReason, options.Reason = true, strings.TrimPrefix(args[i], "--reason=")
		case args[i] == "--evidence" && i+1 < len(args):
			if options.EvidenceSet {
				return "", sessions.EndOptions{}, fmt.Errorf("--evidence may only be specified once")
			}
			if err := json.Unmarshal([]byte(args[i+1]), &options.Evidence); err != nil {
				return "", sessions.EndOptions{}, fmt.Errorf("invalid --evidence JSON: %w", err)
			}
			options.EvidenceSet = true
			i++
		case strings.HasPrefix(args[i], "--evidence="):
			if options.EvidenceSet {
				return "", sessions.EndOptions{}, fmt.Errorf("--evidence may only be specified once")
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(args[i], "--evidence=")), &options.Evidence); err != nil {
				return "", sessions.EndOptions{}, fmt.Errorf("invalid --evidence JSON: %w", err)
			}
			options.EvidenceSet = true
		default:
			return "", sessions.EndOptions{}, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	if hasOutcome && !canonicalOutcome(options.Outcome) {
		return "", sessions.EndOptions{}, fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", options.Outcome)
	}
	if hasReason && !hasOutcome {
		return "", sessions.EndOptions{}, fmt.Errorf("--reason requires --outcome")
	}
	if options.EvidenceSet && !hasOutcome {
		return "", sessions.EndOptions{}, fmt.Errorf("--evidence requires --outcome")
	}
	return id, options, nil
}

func printSessionStatus(ctx context.Context, out io.Writer, id string, jsonOutput bool) error {
	status, err := currentSessionService().GetStatus(ctx, id)
	if err != nil {
		return err
	}
	if jsonOutput {
		data, err := sessions.StatusJSON(status)
		if err != nil {
			return err
		}
		return printRemoteData(out, data)
	}
	lastSaved := "never"
	if status.Capture.LastSavedAt != nil {
		lastSaved = *status.Capture.LastSavedAt
	}
	render := cliui.New(out)
	fields := []bentotui.Field{
		{Label: "Capture", Value: sessions.ReceiptSummary(status)},
		{Label: "Session", Value: status.SessionID},
		{Label: "State", Value: render.CaptureBadge(status.Capture.Status)},
		{Label: "Saved", Value: fmt.Sprint(status.Capture.SavedExchanges)},
		{Label: "Pending", Value: fmt.Sprint(status.Capture.PendingExchanges)},
		{Label: "Failed", Value: fmt.Sprint(status.Capture.FailedExchanges)},
		{Label: "Last save", Value: lastSaved},
		{Label: "Outcome", Value: render.OutcomeBadge(status.Outcome)},
	}
	if status.DashboardURL != nil {
		fields = append(fields, bentotui.Field{Label: "Dashboard", Value: *status.DashboardURL})
	}
	_, err = fmt.Fprintln(out, render.Card("Session status", fields...))
	return err
}

type outcomeEvidence struct {
	Value any
}

func parseSessionOutcomeArgs(args []string) (id, outcome, reason string, evidence *outcomeEvidence, err error) {
	if len(args) < 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		return "", "", "", nil, fmt.Errorf("usage: mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text] [--evidence json]")
	}
	id, outcome = args[0], args[1]
	if !canonicalOutcome(outcome) {
		return "", "", "", nil, fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", outcome)
	}
	for i := 2; i < len(args); i++ {
		switch {
		case args[i] == "--reason":
			if reason != "" || i+1 >= len(args) {
				return "", "", "", nil, fmt.Errorf("--reason requires one value")
			}
			reason = args[i+1]
			if strings.TrimSpace(reason) == "" {
				return "", "", "", nil, fmt.Errorf("--reason requires one value")
			}
			i++
		case strings.HasPrefix(args[i], "--reason="):
			if reason != "" {
				return "", "", "", nil, fmt.Errorf("--reason may only be specified once")
			}
			reason = strings.TrimPrefix(args[i], "--reason=")
			if strings.TrimSpace(reason) == "" {
				return "", "", "", nil, fmt.Errorf("--reason requires one value")
			}
		case args[i] == "--evidence":
			if evidence != nil || i+1 >= len(args) {
				return "", "", "", nil, fmt.Errorf("--evidence requires one JSON value")
			}
			var value any
			if err := json.Unmarshal([]byte(args[i+1]), &value); err != nil {
				return "", "", "", nil, fmt.Errorf("invalid --evidence JSON: %w", err)
			}
			evidence = &outcomeEvidence{Value: value}
			i++
		case strings.HasPrefix(args[i], "--evidence="):
			if evidence != nil {
				return "", "", "", nil, fmt.Errorf("--evidence may only be specified once")
			}
			var value any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(args[i], "--evidence=")), &value); err != nil {
				return "", "", "", nil, fmt.Errorf("invalid --evidence JSON: %w", err)
			}
			evidence = &outcomeEvidence{Value: value}
		default:
			return "", "", "", nil, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	return id, outcome, reason, evidence, nil
}

func canonicalOutcome(outcome string) bool {
	return sessions.ValidOutcome(outcome)
}

func cmdConfig(ctx context.Context, args []string, out io.Writer) error {
	args, _ = stripJSONFlag(args)
	if len(args) == 1 && args[0] == "get" {
		return remotePrint(ctx, out, "GET", "/config", nil)
	}
	if len(args) == 3 && args[0] == "set" {
		var value any
		if err := json.Unmarshal([]byte(args[2]), &value); err != nil {
			value = args[2]
		}
		return remotePrint(ctx, out, "PUT", "/config", map[string]any{args[1]: value})
	}
	return fmt.Errorf("usage: mimir config get [--json] | mimir config set <key> <json-value> [--json]")
}

func onlyJSONFlag(args []string) bool {
	return len(args) == 0 || (len(args) == 1 && args[0] == "--json")
}

func stripJSONFlag(args []string) ([]string, bool) {
	if len(args) > 0 && args[len(args)-1] == "--json" {
		return args[:len(args)-1], true
	}
	return args, false
}

func parseSearchArgs(args []string) (string, error) {
	args, _ = stripJSONFlag(args)
	if len(args) == 0 {
		return "", fmt.Errorf("usage: mimir search <query> [--json]")
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--") {
			return "", fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return strings.Join(args, " "), nil
}

func remotePrint(ctx context.Context, out io.Writer, method, path string, body any) error {
	data, err := remoteRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	return printRemoteData(out, data)
}

func printRemoteData(out io.Writer, data []byte) error {
	var formatted bytes.Buffer
	if json.Indent(&formatted, data, "", "  ") == nil {
		data = formatted.Bytes()
	}
	_, err := fmt.Fprintln(out, string(data))
	return err
}

func usage(out io.Writer) error {
	render := cliui.New(out)
	commands := []cliui.CommandItem{
		{Usage: "mimir setup [--quick] [--json]", Description: "Provision or reconnect Mimir."},
		{Usage: "mimir install [--bin-dir <dir>] [--json]", Description: "Install the CLI and managed harness files."},
		{Usage: "mimir uninstall [--keep-binary] [--json]", Description: "Remove owned local files without deleting memory."},
		{Usage: "mimir deploy [--json]", Description: "Deploy the bundled Worker and dashboard."},
		{Usage: "mimir access [options] [--json]", Description: "Configure dashboard Access with --token <api-token> and --email <address>, or existing --aud and --team-domain values."},
		{Usage: "mimir login [--json]", Description: "Authenticate and register this machine."},
		{Usage: "mimir demo [--no-open]", Description: "Explore sample sessions in a local browser."},
		{Usage: "mimir dashboard", Description: "Open the private dashboard."},
		{Usage: "mimir tui", Description: "Open the persistent sessions and agent terminal."},
		{Usage: "mimir list [filters] [--no-interactive] [--json]", Description: "Browse captured work sessions."},
		{Usage: "mimir search <query> [--json]", Description: "Search session evidence and local code."},
		{Usage: "mimir session status <id> [--json]", Description: "Inspect capture and outcome state."},
		{Usage: "mimir session end <id> [options]", Description: "Finalize capture and optionally record an outcome."},
		{Usage: "mimir doctor [--json] [--tui]", Description: "Check the installation and connection."},
		{Usage: "mimir update [--check] [--force] [--json]", Description: "Check or apply CLI and harness updates."},
		{Usage: "mimir version [--json]", Description: "Show CLI, bundle, and artifact versions."},
	}
	content := bentotui.Stack(render.Commands(commands), render.ActionHint("mimir help advanced", "Show diagnostic and development commands."))
	_, err := fmt.Fprintf(out, "%s\n\n%s\n", render.Heading("Mimir remembers"), content)
	return err
}

func advancedUsage(out io.Writer) error {
	render := cliui.New(out)
	commands := []cliui.CommandItem{
		{Usage: "mimir connection", Description: "Print the active connection manifest."},
		{Usage: "mimir whoami [--json]", Description: "Verify machine authentication."},
		{Usage: "mimir sessions [--json]", Description: "Fetch the canonical session collection."},
		{Usage: "mimir session <id>", Description: "Fetch one canonical session record."},
		{Usage: "mimir session get <id> [--json]", Description: "Fetch the complete canonical session record."},
		{Usage: "mimir session outcome <id> <outcome> [options]", Description: "Record an evidenced work outcome."},
		{Usage: "mimir reconcile", Description: "Reconcile pending session state."},
		{Usage: "mimir mark <session> <outcome>", Description: "Compatibility alias for recording an outcome."},
		{Usage: "mimir outcome git <session>", Description: "Infer an outcome from repository evidence."},
		{Usage: "mimir config get [--json]", Description: "Read canonical Worker configuration."},
		{Usage: "mimir config set <key> <json-value> [--json]", Description: "Update canonical Worker configuration."},
		{Usage: "mimir index [--full]", Description: "Refresh the local repository code index."},
		{Usage: "mimir recall <query> [--budget 4000] [--json]", Description: "Search local code memory."},
	}
	_, err := fmt.Fprintf(out, "%s\n\n%s\n", render.Heading("Mimir advanced commands"), render.Commands(commands))
	return err
}

func parseRecallArgs(args []string) ([]string, int, bool, error) {
	budget, jsonOut := 4000, false
	query := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOut = true
		case "--budget":
			if i+1 >= len(args) {
				return nil, 0, false, fmt.Errorf("--budget requires a value")
			}
			if _, err := fmt.Sscanf(args[i+1], "%d", &budget); err != nil || budget <= 0 {
				return nil, 0, false, fmt.Errorf("invalid --budget value")
			}
			i++
		default:
			query = append(query, args[i])
		}
	}
	return query, budget, jsonOut, nil
}

var (
	version = "0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

func SetBuildInfo(buildVersion, buildCommit, buildDate string) {
	if info, ok := debug.ReadBuildInfo(); ok {
		buildVersion, buildCommit, buildDate = resolveBuildInfo(buildVersion, buildCommit, buildDate, info)
	}
	version = buildVersion
	commit = buildCommit
	date = buildDate
}

func resolveBuildInfo(buildVersion, buildCommit, buildDate string, info *debug.BuildInfo) (string, string, string) {
	if (buildVersion == "" || buildVersion == "0.0.0" || buildVersion == "0.0.0-dev") && info.Main.Version != "" && info.Main.Version != "(devel)" {
		buildVersion = strings.TrimPrefix(info.Main.Version, "v")
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if (buildCommit == "" || buildCommit == "unknown") && setting.Value != "" {
				buildCommit = setting.Value
				if len(buildCommit) > 12 {
					buildCommit = buildCommit[:12]
				}
			}
		case "vcs.time":
			if buildDate == "" || buildDate == "unknown" {
				buildDate = setting.Value
			}
		}
	}
	return buildVersion, buildCommit, buildDate
}

func versionString() string {
	if commit == "unknown" {
		return version
	}
	return version + " (" + commit + ")"
}
