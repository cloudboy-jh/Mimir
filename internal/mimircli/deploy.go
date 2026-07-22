package mimircli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// deploy is the single supported way to ship Worker code and dashboard assets.
// It materializes the packaged Worker, builds the dashboard, writes the real
// D1 database ID into the materialized config, and runs wrangler deploy.
func deploy(ctx context.Context, args []string, ioctx IO) error {
	opts := setupOptions{WorkerName: "mimir", DatabaseName: "mimir", BucketName: "mimir-logs"}
	for i := 0; i < len(args); i++ {
		if args[i] == "--json" {
			opts.JSON = true
			continue
		}
		if i+1 >= len(args) {
			return fmt.Errorf("%s requires a value", args[i])
		}
		switch args[i] {
		case "--worker-dir":
			opts.WorkerDir = args[i+1]
		case "--worker-name":
			opts.WorkerName = args[i+1]
		case "--database-name":
			opts.DatabaseName = args[i+1]
		default:
			return fmt.Errorf("unknown deploy option %q", args[i])
		}
		i++
	}
	dir, err := workerDir(opts.WorkerDir)
	if err != nil {
		return err
	}
	dir, err = materializeWorker(dir)
	if err != nil {
		return err
	}
	if err := ensureWorkerDependencies(ctx, dir); err != nil {
		return fmt.Errorf("installing Worker dependencies: %w", err)
	}
	if err := buildDashboard(ctx, dir); err != nil {
		return fmt.Errorf("building dashboard: %w", err)
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Worker prepared")
	if err := ensureCloudflareAuth(ctx, dir, ioctx, opts.JSON, opts.Progress); err != nil {
		return err
	}
	output, err := runWrangler(ctx, dir, nil, "d1", "list", "--json")
	if err != nil {
		return err
	}
	opts.DatabaseID = listedDatabaseID(output, opts.DatabaseName)
	if opts.DatabaseID == "" {
		return setupStateError{State: "deployment_missing", Message: "no Mimir D1 database found; run mimir setup first"}
	}
	if err := updateWranglerConfig(filepath.Join(dir, "wrangler.jsonc"), opts); err != nil {
		return err
	}
	if _, err := runWrangler(ctx, dir, nil, "d1", "migrations", "apply", opts.DatabaseName, "--remote"); err != nil {
		return fmt.Errorf("applying database migrations: %w", err)
	}
	deployOutput, err := runWrangler(ctx, dir, nil, "deploy")
	if err != nil {
		return err
	}
	url := workerURL(deployOutput)
	if url == "" {
		if pointer, err := loadPointer(); err == nil {
			url = pointer.URL
		}
	}
	if url != "" {
		if err := storeDeploymentURL(ctx, dir, opts.DatabaseName, url); err != nil {
			return err
		}
	}
	result := map[string]any{"state": "deployed", "url": strings.TrimRight(url, "/")}
	human := fmt.Sprintf("Mimir deployed\n\n  Worker %s", strings.TrimRight(url, "/"))
	return writeSetupResult(ioctx.Out, opts.JSON, result, human)
}
