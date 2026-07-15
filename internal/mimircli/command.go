package mimircli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
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
	case "sessions":
		return remotePrint(ctx, ioctx.Out, "GET", "/sessions", nil)
	case "session":
		if len(args) != 2 {
			return fmt.Errorf("usage: mimir session <id>")
		}
		return remotePrint(ctx, ioctx.Out, "GET", "/sessions/"+args[1], nil)
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
			return fmt.Errorf("usage: mimir mark <session> <promoted|discarded|abandoned|unknown>")
		}
		return remotePrint(ctx, ioctx.Out, "POST", "/sessions/"+args[1]+"/mark", map[string]string{"outcome": args[2]})
	case "config":
		return cmdConfig(ctx, args[1:], ioctx.Out)
	case "setup":
		return setup(ctx, args[1:], ioctx)
	case "login":
		return login(ctx, args[1:], ioctx)
	case "connection":
		return writeConnectionManifest(ioctx.Out)
	case "outcome":
		if len(args) != 3 || args[1] != "git" {
			return fmt.Errorf("usage: mimir outcome git <session>")
		}
		data, err := markGitOutcome(ctx, args[2])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(ioctx.Out, string(data))
		return err
	default:
		return fmt.Errorf("unknown command %q", args[0])
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
	var formatted any
	if json.Unmarshal(data, &formatted) == nil {
		data, _ = json.MarshalIndent(formatted, "", "  ")
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}

func usage(out io.Writer) error {
	_, err := fmt.Fprintln(out, `mimir remembers

Usage:
  mimir setup [--quick] [--json]
  mimir login [--json]

Run "mimir help advanced" for diagnostic commands.`)
	return err
}

func advancedUsage(out io.Writer) error {
	_, err := fmt.Fprintln(out, `mimir advanced commands

These commands support harness integrations, diagnostics, and development.

Usage:
  mimir connection
  mimir whoami
  mimir sessions
  mimir session <id>
  mimir search <query>
  mimir mark <session> <outcome>
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
	version = buildVersion
	commit = buildCommit
	date = buildDate
}

func versionString() string {
	if commit == "unknown" {
		return version
	}
	return version + " (" + commit + ")"
}
