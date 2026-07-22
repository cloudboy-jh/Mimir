package mimircli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
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
	if len(args) == 0 {
		return usage(ioctx.Out)
	}
	switch args[0] {
	case "--version", "version":
		_, err := fmt.Fprintln(ioctx.Out, versionString())
		return err
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
		res, err := runIndex(ctx, indexOptions{Dir: ".", Full: *full})
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
	case "serve":
		return serveMCP(ctx, mcpOptions{Dir: ".", In: ioctx.In, Out: ioctx.Out})
	case "whoami":
		return remotePrint(ctx, ioctx.Out, "GET", "/whoami", nil)
	case "list":
		return cmdList(ctx, args[1:], ioctx.Out)
	case "sessions":
		return remotePrint(ctx, ioctx.Out, "GET", "/sessions", nil)
	case "session":
		return cmdSession(ctx, args[1:], ioctx.Out)
	case "search":
		if len(args) < 2 {
			return fmt.Errorf("usage: mimir search <query>")
		}
		data, err := federatedSearch(ctx, strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(ioctx.Out, string(data))
		return err
	case "mark":
		if len(args) != 3 {
			return fmt.Errorf("usage: mimir mark <session> <landed|discarded|abandoned|unresolved|promoted|unknown>")
		}
		return remotePrint(ctx, ioctx.Out, "POST", "/sessions/"+args[1]+"/mark", map[string]string{"outcome": args[2]})
	case "reconcile":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir reconcile")
		}
		data, err := runReconcile(ctx)
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
	case "dashboard":
		return dashboard(ctx, ioctx)
	case "connection":
		return writeConnectionManifest(ioctx.Out)
	case "update":
		return cmdUpdate(ctx, args[1:], ioctx.Out)
	case "doctor":
		return doctor(ctx, args[1:], ioctx.Out)
	case "_install-opencode":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir _install-opencode")
		}
		return installCurrentOpenCodeIntegration()
	case "_install-integrations":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir _install-integrations")
		}
		report, err := installCurrentHarnessIntegrations(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(ioctx.Out).Encode(report)
	case "outcome":
		if len(args) != 3 || args[1] != "git" {
			return fmt.Errorf("usage: mimir outcome git <session>")
		}
		data, err := markGitOutcome(ctx, args[2])
		if err != nil {
			return err
		}
		return printRemoteData(ioctx.Out, data)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func cmdSession(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "status" && len(args) < 2 {
		return fmt.Errorf("usage: mimir session status <id> [--json]")
	}
	if len(args) > 0 && args[0] == "end" && len(args) < 2 {
		return fmt.Errorf("usage: mimir session end <id> [--outcome landed|discarded|abandoned|unresolved] [--reason text]")
	}
	if len(args) == 1 {
		return remotePrint(ctx, out, "GET", "/sessions/"+args[0], nil)
	}
	if len(args) >= 2 && args[0] == "end" {
		id, body, err := parseSessionEndArgs(args[1:])
		if err != nil {
			return err
		}
		status, err := endRemoteSession(ctx, id, body)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, endedReceiptText(status))
		return err
	}
	if len(args) >= 2 && args[0] == "status" {
		if len(args) > 3 || (len(args) == 3 && args[2] != "--json") {
			return fmt.Errorf("usage: mimir session status <id> [--json]")
		}
		return printSessionStatus(ctx, out, args[1], len(args) == 3)
	}
	if len(args) >= 3 && args[0] == "outcome" {
		id, outcome, reason, err := parseSessionOutcomeArgs(args[1:])
		if err != nil {
			return err
		}
		body := map[string]any{"outcome": outcome, "source": "agent"}
		if reason != "" {
			body["reason"] = reason
		}
		return remotePrint(ctx, out, "POST", "/sessions/"+id+"/outcome", body)
	}
	return fmt.Errorf("usage: mimir session <id> | mimir session status <id> [--json] | mimir session end <id> [--outcome value] [--reason text] | mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text]")
}

func parseSessionEndArgs(args []string) (string, map[string]any, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, fmt.Errorf("usage: mimir session end <id> [--outcome landed|discarded|abandoned|unresolved] [--reason text]")
	}
	id := args[0]
	body := map[string]any{}
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--outcome" && i+1 < len(args):
			if _, exists := body["outcome"]; exists {
				return "", nil, fmt.Errorf("--outcome may only be specified once")
			}
			body["outcome"] = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--outcome="):
			if _, exists := body["outcome"]; exists {
				return "", nil, fmt.Errorf("--outcome may only be specified once")
			}
			body["outcome"] = strings.TrimPrefix(args[i], "--outcome=")
		case args[i] == "--reason" && i+1 < len(args):
			if _, exists := body["reason"]; exists {
				return "", nil, fmt.Errorf("--reason may only be specified once")
			}
			body["reason"] = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--reason="):
			if _, exists := body["reason"]; exists {
				return "", nil, fmt.Errorf("--reason may only be specified once")
			}
			body["reason"] = strings.TrimPrefix(args[i], "--reason=")
		default:
			return "", nil, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	if value, exists := body["outcome"]; exists {
		outcome, _ := value.(string)
		if !canonicalOutcome(outcome) {
			return "", nil, fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", outcome)
		}
	}
	if _, hasReason := body["reason"]; hasReason {
		if _, hasOutcome := body["outcome"]; !hasOutcome {
			return "", nil, fmt.Errorf("--reason requires --outcome")
		}
	}
	return id, body, nil
}

func printSessionStatus(ctx context.Context, out io.Writer, id string, jsonOutput bool) error {
	status, err := getSessionStatus(ctx, id)
	if err != nil {
		return err
	}
	if jsonOutput {
		data, err := sessionStatusJSON(status)
		if err != nil {
			return err
		}
		return printRemoteData(out, data)
	}
	lastSaved := "never"
	if status.Capture.LastSavedAt != nil {
		lastSaved = *status.Capture.LastSavedAt
	}
	_, err = fmt.Fprintf(out, "%s\nSession   %s\nCapture   %s\nSaved     %d\nPending   %d\nFailed    %d\nLast save %s\nOutcome   %s\n", receiptSummary(status), status.SessionID, displayState(status.Capture.Status), status.Capture.SavedExchanges, status.Capture.PendingExchanges, status.Capture.FailedExchanges, lastSaved, displayState(status.Outcome))
	if err == nil && status.DashboardURL != nil {
		_, err = fmt.Fprintf(out, "Dashboard %s\n", *status.DashboardURL)
	}
	return err
}

func displayState(value string) string {
	if value == "" {
		return "Unavailable"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func parseSessionOutcomeArgs(args []string) (id, outcome, reason string, err error) {
	if len(args) < 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		return "", "", "", fmt.Errorf("usage: mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text]")
	}
	id, outcome = args[0], args[1]
	if !canonicalOutcome(outcome) {
		return "", "", "", fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", outcome)
	}
	for i := 2; i < len(args); i++ {
		switch {
		case args[i] == "--reason":
			if reason != "" || i+1 >= len(args) {
				return "", "", "", fmt.Errorf("--reason requires one value")
			}
			reason = args[i+1]
			if strings.TrimSpace(reason) == "" {
				return "", "", "", fmt.Errorf("--reason requires one value")
			}
			i++
		case strings.HasPrefix(args[i], "--reason="):
			if reason != "" {
				return "", "", "", fmt.Errorf("--reason may only be specified once")
			}
			reason = strings.TrimPrefix(args[i], "--reason=")
			if strings.TrimSpace(reason) == "" {
				return "", "", "", fmt.Errorf("--reason requires one value")
			}
		default:
			return "", "", "", fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	return id, outcome, reason, nil
}

func canonicalOutcome(outcome string) bool {
	switch outcome {
	case "landed", "discarded", "abandoned", "unresolved":
		return true
	default:
		return false
	}
}

func cmdConfig(ctx context.Context, args []string, out io.Writer) error {
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
	return fmt.Errorf("usage: mimir config get | mimir config set <key> <json-value>")
}

func remotePrint(ctx context.Context, out io.Writer, method, path string, body any) error {
	data, err := remoteRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	return printRemoteData(out, data)
}

func printRemoteData(out io.Writer, data []byte) error {
	var formatted any
	if json.Unmarshal(data, &formatted) == nil {
		data, _ = json.MarshalIndent(formatted, "", "  ")
	}
	_, err := fmt.Fprintln(out, string(data))
	return err
}

func usage(out io.Writer) error {
	_, err := fmt.Fprintln(out, `mimir remembers

Usage:
  mimir setup [--quick] [--json]
  mimir deploy [--json]
  mimir access [--token <api-token> | --aud <tag> --team-domain <domain>]
  mimir login [--json]
  mimir dashboard
  mimir list [--repo name] [--outcome landed|discarded|abandoned|unresolved] [--limit 20]
  mimir session status <id> [--json]
  mimir session end <id> [--outcome landed|discarded|abandoned|unresolved] [--reason text]
  mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text]
  mimir reconcile
  mimir doctor [--json]
  mimir update [--check]

Run "mimir help advanced" for diagnostic commands.`)
	return err
}

func advancedUsage(out io.Writer) error {
	_, err := fmt.Fprintln(out, `mimir advanced commands

These commands support harness integrations, diagnostics, and development.

Usage:
  mimir connection
  mimir doctor [--json]
  mimir whoami
  mimir list [--repo name] [--outcome landed|discarded|abandoned|unresolved] [--limit 20]
  mimir sessions
  mimir session <id>
  mimir session status <id>
  mimir session end <id> [--outcome landed|discarded|abandoned|unresolved] [--reason text]
  mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text]
  mimir search <query>
  mimir reconcile
  mimir mark <session> <landed|discarded|abandoned|unresolved|promoted|unknown>
  mimir outcome git <session>
  mimir config get
  mimir config set <key> <json-value>
  mimir index [--full]
  mimir recall <query> [--budget 4000] [--json]
  mimir serve`)
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
