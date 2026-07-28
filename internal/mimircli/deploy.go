package mimircli

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	cliui "github.com/cloudboy-jh/mimir/internal/ui"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
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
	domainOpts := deployment.DefaultOptions()
	domainOpts.WorkerDir, domainOpts.WorkerName, domainOpts.DatabaseName = opts.WorkerDir, opts.WorkerName, opts.DatabaseName
	domainOpts.Noninteractive = opts.JSON
	if !opts.JSON {
		opts.Progress = startProgress(ioctx.Out, "Mimir deploy", []string{"Preparing Worker", "Authenticating Cloudflare", "Configuring database", "Applying schema", "Deploying Worker"})
		defer opts.Progress.Stop()
	}
	fallback := ""
	if pointer, err := loadPointer(); err == nil {
		fallback = pointer.URL
	}
	domainResult, err := deployment.NewService(httpClient).Deploy(ctx, domainOpts, deployment.Hooks{
		Streams: deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err},
		Step:    func(message string) { setupStep(opts.Progress, ioctx.Out, opts.JSON, message) },
		Login: func(ctx context.Context, dir string) error {
			opts.Progress.Pause()
			defer opts.Progress.Resume()
			fmt.Fprintln(ioctx.Out, "Cloudflare login required. Opening Wrangler authentication...")
			return deployment.Wrangler{}.Interactive(ctx, dir, deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err}, "login")
		},
	}, fallback)
	if err != nil {
		opts.Progress.Fail()
		return err
	}
	url := domainResult.URL
	opts.Progress.Stop()
	result := map[string]any{"state": "deployed", "url": strings.TrimRight(url, "/")}
	human := cliui.New(ioctx.Out).Card("Deployment complete", bentotui.Field{Label: "Worker", Value: strings.TrimRight(url, "/")}, bentotui.Field{Label: "Status", Value: "ready"})
	return writeSetupResult(ioctx.Out, opts.JSON, result, human)
}
