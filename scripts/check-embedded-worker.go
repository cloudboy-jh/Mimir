//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cloudboy-jh/mimir/internal/install"
)

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check() error {
	home, err := os.MkdirTemp("", "mimir-embedded-worker-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(home)
	if err := os.Setenv("MIMIR_HOME", home); err != nil {
		return err
	}

	// Use deploy's default materialization, never the checkout or --worker-dir.
	dir, err := install.WorkerDir("")
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := install.EnsureWorkerDependencies(ctx, dir); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "node", filepath.Join(dir, "node_modules", "wrangler", "bin", "wrangler.js"), "deploy", "--dry-run")
	command.Dir = dir
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("compiling embedded Worker: %w", err)
	}
	fmt.Println("embedded Worker deployment bundle verified")
	return nil
}
