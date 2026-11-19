package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"text/tabwriter"

	hookintegration "github.com/cloudboy-jh/mimir/internal/harness/hooks"
	"github.com/cloudboy-jh/mimir/internal/sessions"
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
	case "whoami":
		if !onlyJSONFlag(args[1:]) {
			return fmt.Errorf("usage: mimir whoami [--json]")
		}
		return remotePrint(ctx, ioctx.Out, "GET", "/whoami", nil)
	case "sessions":
		return cmdSessions(ctx, args[1:], ioctx.Out)
	case "session":
		return cmdSession(ctx, args[1:], ioctx.Out)
	case "turns":
		return cmdTurns(ctx, args[1:], ioctx.Out)
	case "search":
		return cmdSearch(ctx, args[1:], ioctx.Out)
	case "stats":
		return cmdStats(ctx, args[1:], ioctx.Out)
	case "day":
		return cmdDay(ctx, args[1:], ioctx.Out)
	case "devices":
		return cmdDevices(ctx, args[1:], ioctx.Out)
	case "commits":
		return cmdCommits(ctx, args[1:], ioctx.Out)
	case "import":
		return cmdImport(ctx, args[1:], ioctx)
	case "backfill":
		return cmdBackfill(ctx, args[1:], ioctx)
	case "reconcile":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir reconcile")
		}
		data, err := sessions.New(apiRequester{}).Reconcile(ctx)
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
	case "harness":
		return cmdHarness(ctx, args[1:], ioctx)
	case "enable", "disable":
		return cmdPrimaryHarness(ctx, args[0], args[1:], ioctx)
	case "uninstall":
		return cmdUninstall(ctx, args[1:], ioctx.Out)
	case "dashboard":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir dashboard")
		}
		return dashboard(ctx, ioctx)
	case "connection":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir connection")
		}
		return writeConnectionManifest(ioctx.Out)
	case "update":
		return cmdUpdateIO(ctx, args[1:], ioctx)
	case "doctor":
		return doctor(ctx, args[1:], ioctx.Out)
	case "mcp":
		return cmdMCP(ctx, args[1:], ioctx)
	case "_hook":
		if len(args) != 2 {
			return fmt.Errorf("usage: mimir _hook <git|claude-code|codex|cursor>")
		}
		if args[1] == "git" {
			return hookGit(ctx, ioctx)
		}
		service, err := hookintegration.New()
		if err != nil {
			return err
		}
		return service.Ingest(ctx, args[1], ioctx.In)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// hookGit implements `mimir _hook git`: capture commits named on stdin and
// print {captured, pushed, errors}. Only invalid input fails the command.
func hookGit(ctx context.Context, ioctx IO) error {
	in := ioctx.In
	if in == nil {
		in = strings.NewReader("")
	}
	input, err := hookintegration.DecodeGitInput(in)
	if err != nil {
		return fmt.Errorf("invalid git hook input: %w", err)
	}
	result := hookintegration.GitResult{Errors: []string{}}
	if service, err := hookintegration.New(); err != nil {
		result.Errors = append(result.Errors, err.Error())
	} else {
		result = service.CaptureGit(ctx, input)
	}
	return json.NewEncoder(ioctx.Out).Encode(result)
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

type commandHelp struct{ usage, description string }

type commandGroup struct {
	name     string
	commands []commandHelp
}

func writeCommands(out io.Writer, title string, groups []commandGroup, footer string) error {
	table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, title)
	for _, group := range groups {
		fmt.Fprintf(table, "\n%s\n", group.name)
		for _, command := range group.commands {
			fmt.Fprintf(table, "  %s\t%s\n", command.usage, command.description)
		}
	}
	if footer != "" {
		fmt.Fprintf(table, "\n%s\n", footer)
	}
	return table.Flush()
}

func usage(out io.Writer) error {
	return writeCommands(out, "Mimir remembers your agent sessions.", []commandGroup{
		{"Query", []commandHelp{
			{"mimir sessions [filters] [--json]", "List root sessions, newest activity first."},
			{"mimir session <id> [--json]", "Show a session with summary, commits, and files."},
			{"mimir session status <id> [--json]", "Show the capture receipt."},
			{"mimir session end <id> [--json]", "End a session and print its receipt."},
			{"mimir turns <id> [--limit n] [--order asc|desc] [--json]", "Read the conversation turns."},
			{"mimir search <query> [--repo r] [--json]", "Full-text search across captured sessions."},
			{"mimir stats --by <repo|harness|device|model|day> [--json]", "Aggregate sessions, requests, tokens, commits."},
			{"mimir day [YYYY-MM-DD] [--tz zone] [--json]", "One day of sessions grouped into workstreams."},
			{"mimir commits [filters] [--json]", "List captured commits."},
			{"mimir devices [--json]", "List machines and their tailnet status."},
		}},
		{"Set up", []commandHelp{
			{"mimir setup [--account-id <id>] [--json]", "Provision or reconnect Mimir."},
			{"mimir login [--account-id <id>] [--json]", "Authenticate and register this machine."},
			{"mimir install --harness <id|all> [--json]", "Install the CLI and selected harness files."},
			{"mimir harness [--json]", "Show harness integrations and manual steps."},
			{"mimir enable|disable <pi|opencode|hermes>", "Enable or disable a primary harness."},
			{"mimir deploy [--account-id <id>] [--json]", "Deploy the bundled Worker and dashboard."},
			{"mimir access [--token <api-token> --email <address>] [--json]", "Configure Cloudflare Access for the dashboard."},
			{"mimir dashboard", "Open the private dashboard."},
			{"mimir import [harness] [session-id...]", "Import sessions from a local harness."},
			{"mimir backfill [harness] [--since 7d]", "Repair missing capture from local history."},
			{"mimir doctor [--json]", "Check the installation and connection."},
			{"mimir update [--check] [--json]", "Check for or install the latest release."},
			{"mimir uninstall [--keep-binary] [--json]", "Remove Mimir-owned local files."},
			{"mimir version [--json]", "Show the CLI and bundle version."},
		}},
	}, "Run mimir help advanced for diagnostic and integration commands.")
}

func advancedUsage(out io.Writer) error {
	return writeCommands(out, "Mimir advanced commands", []commandGroup{
		{"Diagnostics", []commandHelp{
			{"mimir whoami [--json]", "Verify machine authentication and Worker capabilities."},
			{"mimir connection", "Print the active connection manifest."},
			{"mimir reconcile", "Reconcile pending capture state."},
			{"mimir config get [--json]", "Read Worker configuration."},
			{"mimir config set <key> <json-value> [--json]", "Update Worker configuration."},
			{"mimir harness <enable|disable> <id> [--json]", "Manage any harness integration."},
		}},
		{"Integrations", []commandHelp{
			{"mimir mcp", "Stdio MCP bridge to the Worker query tools."},
			{"mimir _hook git", "Capture commits from a JSON object on stdin."},
			{"mimir _hook <claude-code|codex|cursor>", "Harness hook adapter."},
		}},
	}, "")
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
