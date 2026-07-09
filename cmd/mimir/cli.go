package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
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
	case "-h", "--help", "help":
		return usage(ioctx.Out)
	case "status":
		return status(ctx, ioctx.Out)
	case "doctor":
		return doctor(ctx, ioctx.Out)
	case "control":
		return cmdControl(ctx, args[1:], ioctx)
	case "session":
		return cmdSession(ctx, args[1:], ioctx)
	case "index":
		fs := flag.NewFlagSet("index", flag.ContinueOnError)
		fs.SetOutput(ioctx.Err)
		full := fs.Bool("full", false, "force a full repository index")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		start := time.Now()
		res, err := runIndex(ctx, indexOptions{Dir: ".", Full: *full})
		if err != nil {
			writeReceipt(ioctx.Out, failReceipt("code", "index", err.Error()))
			return err
		}
		ms := time.Since(start).Milliseconds()
		cfg := mustLoadCfgOrDefault()
		_ = appendLog(cfg, "code.index", fmt.Sprintf("%s %s files=%d %dms", res.Project, res.Mode, res.Indexed, ms), "ok")
		writeReceipt(ioctx.Out, Receipt{
			Plane:   "code",
			Verb:    "index",
			Subject: res.Project + "  " + res.Mode,
			Meaning: fmt.Sprintf("+%d files · %dms · sha %s", res.Indexed, ms, short(res.HeadSHA)),
			Status:  "ok",
		})
		return nil
	case "recall":
		queryArgs, budget, jsonOut, err := parseRecallArgs(args[1:])
		if err != nil {
			return err
		}
		if len(queryArgs) == 0 {
			return fmt.Errorf("usage: mimir recall <query> [--budget 4000] [--json]")
		}
		query := strings.Join(queryArgs, " ")
		res, err := runRecall(ctx, recallOptions{Dir: ".", Query: query, Budget: budget, JSON: jsonOut})
		if err != nil {
			return err
		}
		if !jsonOut {
			writeReceipt(ioctx.Out, Receipt{
				Plane:   "code",
				Verb:    "recall",
				Subject: query,
			})
		}
		_, err = fmt.Fprintln(ioctx.Out, res.Output)
		return err
	case "deps":
		if len(args) < 2 {
			return fmt.Errorf("usage: mimir deps <file_path>")
		}
		fi, downstream, err := fileDeps(ctx, ".", args[1])
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(map[string]any{"file": args[1], "dependencies": fi.Dependencies, "downstream": downstream}, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(ioctx.Out, string(data))
		return err
	case "locate":
		if len(args) < 2 {
			return fmt.Errorf("usage: mimir locate <symbol_name>")
		}
		sym, ok, err := locateSymbol(ctx, ".", args[1])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("symbol not found: %s", args[1])
		}
		data, err := json.MarshalIndent(sym, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(ioctx.Out, string(data))
		return err
	case "serve":
		return serveMCP(ctx, mcpOptions{Dir: ".", In: ioctx.In, Out: ioctx.Out})
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func cmdControl(ctx context.Context, args []string, ioctx IO) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mimir control <init|status>")
	}
	switch args[0] {
	case "init":
		machine := ""
		for i := 1; i < len(args); i++ {
			if args[i] == "--machine" && i+1 < len(args) {
				machine = args[i+1]
				i++
			}
		}
		cfg, err := controlInit(machine)
		if err != nil {
			writeReceipt(ioctx.Out, failReceipt("control", "init", err.Error()))
			return err
		}
		sess := "off"
		if cfg.Sessions.Enabled {
			sess = "on"
		}
		writeReceipt(ioctx.Out, Receipt{
			Plane:   "control",
			Verb:    "init",
			Subject: "machine=" + cfg.Machine,
			Meaning: "sessions " + sess + " · code mcp optional",
			Status:  "ok",
		})
		return nil
	case "status":
		return controlStatus(ioctx.Out)
	default:
		return fmt.Errorf("unknown control subcommand %q", args[0])
	}
}

func controlStatus(out io.Writer) error {
	home, cfgPath, logPath, err := controlPaths()
	if err != nil {
		return err
	}
	cfg, err := loadControlConfig()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "home:     %s\n", home)
	fmt.Fprintf(out, "config:   %s (%s)\n", cfgPath, presentMissing(pathExists(cfgPath)))
	fmt.Fprintf(out, "machine:  %s\n", dash(cfg.Machine))
	fmt.Fprintf(out, "sessions: enabled=%t repo=%s\n", cfg.Sessions.Enabled, dash(cfg.Sessions.Repo))
	sp, _ := sessionsAbsPath(cfg)
	fmt.Fprintf(out, "sesspath: %s (%s)\n", sp, presentMissing(pathExists(sp)))
	lp, err := logAbsPath(cfg)
	if err != nil {
		lp = logPath
	}
	fmt.Fprintf(out, "log:      %s (%s)\n", lp, presentMissing(pathExists(lp)))
	return nil
}

func cmdSession(ctx context.Context, args []string, ioctx IO) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mimir session <init|push|pull|list>")
	}
	switch args[0] {
	case "init":
		repo := ""
		for i := 1; i < len(args); i++ {
			if args[i] == "--repo" && i+1 < len(args) {
				repo = args[i+1]
				i++
			}
		}
		res, err := sessionInit(ctx, sessionInitOptions{Repo: repo})
		writeReceipt(ioctx.Out, res.Receipt)
		return err
	case "push":
		opts := sessionPushOptions{}
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--id":
				if i+1 >= len(args) {
					return fmt.Errorf("--id requires a value")
				}
				opts.ID = args[i+1]
				i++
			case "--harness":
				if i+1 >= len(args) {
					return fmt.Errorf("--harness requires a value")
				}
				opts.Harness = args[i+1]
				i++
			case "--project":
				if i+1 >= len(args) {
					return fmt.Errorf("--project requires a value")
				}
				opts.Project = args[i+1]
				i++
			case "--goal":
				if i+1 >= len(args) {
					return fmt.Errorf("--goal requires a value")
				}
				opts.Goal = args[i+1]
				i++
			case "--body":
				if i+1 >= len(args) {
					return fmt.Errorf("--body requires a value")
				}
				bodyArg := args[i+1]
				i++
				if bodyArg == "-" {
					b, err := io.ReadAll(ioctx.In)
					if err != nil {
						return err
					}
					opts.Body = string(b)
				} else {
					b, err := os.ReadFile(bodyArg)
					if err != nil {
						return err
					}
					opts.Body = string(b)
				}
			case "--no-push":
				opts.NoPush = true
			}
		}
		if opts.ID == "" {
			return fmt.Errorf("usage: mimir session push --id SLUG [--harness NAME] [--project NAME] [--goal TEXT] [--body PATH|-]")
		}
		res, err := sessionPush(ctx, opts)
		writeReceipt(ioctx.Out, res.Receipt)
		return err
	case "pull":
		id := ""
		for i := 1; i < len(args); i++ {
			if args[i] == "--id" && i+1 < len(args) {
				id = args[i+1]
				i++
			}
		}
		res, err := sessionPull(ctx, id)
		writeReceipt(ioctx.Out, res.Receipt)
		if err == nil {
			for _, f := range res.Files {
				fmt.Fprintln(ioctx.Out, f)
			}
		}
		return err
	case "list":
		cfg := mustLoadCfgOrDefault()
		names, err := sessionList(cfg)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			fmt.Fprintln(ioctx.Out, "(no sessions)")
			return nil
		}
		for _, n := range names {
			fmt.Fprintln(ioctx.Out, n)
		}
		return nil
	default:
		return fmt.Errorf("unknown session subcommand %q", args[0])
	}
}

func usage(out io.Writer) error {
	_, err := fmt.Fprintln(out, `mimir remembers the repo and the session

Usage:
  mimir control init [--machine NAME]
  mimir control status
  mimir session init [--repo URL]
  mimir session push --id SLUG [--harness NAME] [--project NAME] [--goal TEXT] [--body PATH|-]
  mimir session pull [--id SLUG]
  mimir session list
  mimir status
  mimir doctor
  mimir index [--full]
  mimir recall <query> [--budget 4000] [--json]
  mimir deps <file_path>
  mimir locate <symbol_name>
  mimir serve
  mimir --version

Agent-primary: daily UX is chat intent, not these commands.`)
	return err
}

func status(ctx context.Context, out io.Writer) error {
	// Control plane brief
	if home, err := mimirHome(); err == nil {
		fmt.Fprintf(out, "control:     %s (%s)\n", home, presentMissing(pathExists(home)))
	}
	if cfg, err := loadControlConfig(); err == nil && cfg.Machine != "" {
		fmt.Fprintf(out, "machine:     %s\n", cfg.Machine)
		fmt.Fprintf(out, "sessions:    enabled=%t\n", cfg.Sessions.Enabled)
	}

	info, err := detectRepo(ctx, ".")
	if errors.Is(err, errNotRepo) {
		fmt.Fprintln(out, "repo:        -")
		return nil
	}
	if err != nil {
		return err
	}
	idx, _ := loadIndex(info.Root)
	fmt.Fprintf(out, "repo:        %s\n", info.Root)
	fmt.Fprintf(out, "branch:      %s\n", dash(info.Branch))
	fmt.Fprintf(out, "head:        %s\n", short(info.HeadSHA))
	fmt.Fprintf(out, "store:       %s\n", storeState(info))
	fmt.Fprintf(out, "indexed sha: %s\n", dash(short(info.IndexedSHA)))
	fmt.Fprintf(out, "stale:       %t\n", info.Stale)
	fmt.Fprintf(out, "files:       %d\n", len(idx.Files))
	fmt.Fprintf(out, "symbols:     %d\n", len(idx.Symbols))
	if idx.Timestamp != "" {
		fmt.Fprintf(out, "updated:     %s\n", idx.Timestamp)
	}
	return nil
}

func doctor(ctx context.Context, out io.Writer) error {
	fmt.Fprintf(out, "mimir %s\n", versionString())

	// control
	home, cfgPath, _, err := controlPaths()
	if err != nil {
		fmt.Fprintf(out, "control: error (%v)\n", err)
	} else {
		fmt.Fprintf(out, "control home: %s (%s)\n", home, presentMissing(pathExists(home)))
		fmt.Fprintf(out, "config: %s (%s)\n", cfgPath, presentMissing(pathExists(cfgPath)))
		if cfg, err := loadControlConfig(); err == nil {
			fmt.Fprintf(out, "machine: %s\n", dash(cfg.Machine))
			sp, _ := sessionsAbsPath(cfg)
			fmt.Fprintf(out, "sessions enabled: %t path=%s (%s)\n", cfg.Sessions.Enabled, sp, presentMissing(pathExists(sp)))
			if cfg.Sessions.Repo != "" {
				fmt.Fprintf(out, "sessions repo: %s\n", cfg.Sessions.Repo)
			}
			lp, _ := logAbsPath(cfg)
			fmt.Fprintf(out, "log: %s (%s)\n", lp, presentMissing(pathExists(lp)))
		}
	}

	if _, err := runGit(ctx, ".", "--version"); err != nil {
		fmt.Fprintln(out, "git: missing")
	} else {
		fmt.Fprintln(out, "git: ok")
	}
	if _, err := runCmd(ctx, "", "gh", "--version"); err != nil {
		fmt.Fprintln(out, "gh: missing")
	} else {
		fmt.Fprintln(out, "gh: ok")
		if login, err := ghLogin(ctx); err != nil {
			fmt.Fprintln(out, "gh auth: none")
		} else {
			fmt.Fprintf(out, "gh auth: %s\n", login)
		}
	}
	if info, err := detectRepo(ctx, "."); err == nil {
		fmt.Fprintf(out, "repo: %s\n", info.Root)
		fmt.Fprintf(out, "store: %s\n", storeState(info))
	} else {
		fmt.Fprintln(out, "repo: not inside git repo")
	}
	return nil
}

func parseRecallArgs(args []string) ([]string, int, bool, error) {
	budget := 4000
	jsonOut := false
	query := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOut = true
		case "--budget":
			if i+1 >= len(args) {
				return nil, 0, false, fmt.Errorf("--budget requires a value")
			}
			var parsed int
			if _, err := fmt.Sscanf(args[i+1], "%d", &parsed); err != nil || parsed <= 0 {
				return nil, 0, false, fmt.Errorf("invalid --budget value: %s", args[i+1])
			}
			budget = parsed
			i++
		default:
			query = append(query, args[i])
		}
	}
	return query, budget, jsonOut, nil
}

func storeState(info repoInfo) string {
	if info.StoreExists {
		return "present"
	}
	return "missing"
}

func presentMissing(ok bool) string {
	if ok {
		return "present"
	}
	return "missing"
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
