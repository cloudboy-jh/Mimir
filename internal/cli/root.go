package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	churnapp "github.com/cloudboy-jh/churn/internal/app"
	churngit "github.com/cloudboy-jh/churn/internal/git"
	"github.com/cloudboy-jh/churn/internal/indexer"
	"github.com/cloudboy-jh/churn/internal/mcp"
	"github.com/cloudboy-jh/churn/internal/recall"
	"github.com/cloudboy-jh/churn/internal/version"
)

func Execute(ctx context.Context, args []string) error {
	cmd := NewRootCommand(ctx)
	cmd.SetArgs(args)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}
	return nil
}

func NewRootCommand(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "churn",
		Short:         "Durable local code memory for AI coding agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return churnapp.Run(cmd.Context(), ".")
		},
	}

	cmd.AddCommand(newTUICommand())
	cmd.AddCommand(newStatusCommand())
	cmd.AddCommand(newDoctorCommand())
	cmd.AddCommand(newIndexCommand())
	cmd.AddCommand(newRecallCommand())
	cmd.AddCommand(newServeCommand())
	cmd.AddCommand(newConfigCommand())
	cmd.AddCommand(newModelCommand())

	return cmd
}

func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the Churn TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return churnapp.Run(cmd.Context(), ".")
		},
	}
}

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show repository and .churn store freshness",
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := churngit.Detect(cmd.Context(), ".")
			if errors.Is(err, churngit.ErrNotRepo) {
				return fmt.Errorf("not inside a git repo")
			}
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), formatStatus(info))
			return nil
		},
	}
}

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local Churn prerequisites",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "churn doctor")
			fmt.Fprintf(out, "version: %s\n", version.String())

			if _, err := os.Stat(filepath.Clean(".")); err == nil {
				fmt.Fprintln(out, "cwd: ok")
			}

			if _, err := churngit.Run(cmd.Context(), ".", "--version"); err != nil {
				fmt.Fprintln(out, "git: missing or unavailable")
			} else {
				fmt.Fprintln(out, "git: ok")
			}

			if info, err := churngit.Detect(cmd.Context(), "."); err == nil {
				fmt.Fprintf(out, "repo: %s\n", info.Root)
				fmt.Fprintf(out, "store: %s\n", storeState(info))
			} else {
				fmt.Fprintln(out, "repo: not inside git repo")
			}

			return nil
		},
	}
}

func newIndexCommand() *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build or update the durable .churn store",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := indexer.Run(cmd.Context(), indexer.Options{Dir: ".", Full: full})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Message)
			return nil
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "force a full repository index")
	return cmd
}

func newRecallCommand() *cobra.Command {
	var jsonOut bool
	var budget int
	cmd := &cobra.Command{
		Use:   "recall <query>",
		Short: "Retrieve token-budgeted context from the .churn store",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := recall.Run(cmd.Context(), recall.Options{Dir: ".", Query: joinArgs(args), Budget: budget, JSON: jsonOut})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), res.Output)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print JSON output")
	cmd.Flags().IntVar(&budget, "budget", 4000, "token budget for returned context")
	return cmd
}

func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Churn MCP stdio server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mcp.Serve(cmd.Context(), mcp.Options{Dir: ".", In: os.Stdin, Out: cmd.OutOrStdout()})
		},
	}
}

func newConfigCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show Churn config location",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "config: ~/.config/churn/config.json")
			return nil
		},
	}
}

func newModelCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "model",
		Short: "Configure optional model provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "models are optional; phase 1-3 core works offline with no API key")
			return nil
		},
	}
}

func formatStatus(info churngit.RepoInfo) string {
	return fmt.Sprintf(`repo:        %s
branch:      %s
head:        %s
remote:      %s
store:       %s
indexed sha: %s
stale:       %t`, info.Root, emptyDash(info.Branch), shortSHA(info.HeadSHA), emptyDash(info.Remote), storeState(info), emptyDash(shortSHA(info.IndexedSHA)), info.Stale)
}

func storeState(info churngit.RepoInfo) string {
	if info.StoreExists {
		return "present"
	}
	return "missing"
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	out := args[0]
	for _, arg := range args[1:] {
		out += " " + arg
	}
	return out
}
