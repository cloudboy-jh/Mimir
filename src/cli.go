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
			return fmt.Errorf("usage: churn recall <query> [--budget 4000] [--json]")
		}
		res, err := runRecall(ctx, recallOptions{Dir: ".", Query: strings.Join(queryArgs, " "), Budget: budget, JSON: jsonOut})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(ioctx.Out, res.Output)
		return err
	case "deps":
		if len(args) < 2 {
			return fmt.Errorf("usage: churn deps <file_path>")
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
			return fmt.Errorf("usage: churn locate <symbol_name>")
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

func usage(out io.Writer) error {
	_, err := fmt.Fprintln(out, `churn remembers the code

Usage:
  churn status
  churn index [--full]
  churn recall <query> [--budget 4000] [--json]
  churn deps <file_path>
  churn locate <symbol_name>
  churn serve
  churn doctor
  churn --version`)
	return err
}

func status(ctx context.Context, out io.Writer) error {
	info, err := detectRepo(ctx, ".")
	if errors.Is(err, errNotRepo) {
		return fmt.Errorf("not inside a git repo")
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
	fmt.Fprintf(out, "churn %s\n", versionString())
	if _, err := runGit(ctx, ".", "--version"); err != nil {
		fmt.Fprintln(out, "git: missing")
		return nil
	}
	fmt.Fprintln(out, "git: ok")
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
