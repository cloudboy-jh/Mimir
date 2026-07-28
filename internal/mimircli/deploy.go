package mimircli

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	cliui "github.com/cloudboy-jh/mimir/internal/ui/appframe"
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
	operationCtx, cancelOperation := context.WithCancel(ctx)
	defer cancelOperation()
	if !opts.JSON {
		opts.Progress = startOperationProgress(operationCtx, ioctx, "Mimir deploy", []string{"Preparing Worker", "Authenticating Cloudflare", "Configuring database", "Applying schema", "Deploying Worker"}, cancelOperation)
		defer opts.Progress.Stop()
	}
	fallback := ""
	if pointer, err := loadPointer(); err == nil {
		fallback = pointer.URL
	}
	service := deployment.NewService(httpClient)
	if opts.Progress != nil {
		service.Wrangler = deployment.ObserveWrangler(service.Wrangler, opts.Progress.Output())
	}
	domainResult, err := service.Deploy(operationCtx, domainOpts, deployment.Hooks{
		Streams: deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err},
		Step:    func(message string) { setupStep(opts.Progress, ioctx.Out, opts.JSON, message) },
		Login: func(ctx context.Context, dir string) error {
			return opts.Progress.Handoff("Waiting for Cloudflare authentication", func() error {
				cloudflareLoginNotice(ioctx.Out)
				return deployment.Wrangler{}.Interactive(ctx, dir, deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err}, "login")
			})
		},
	}, fallback)
	if err != nil {
		opts.Progress.Fail()
		return err
	}
	url := domainResult.URL
	opts.Progress.Finish("Deployment complete")
	opts.Progress.Stop()
	result := map[string]any{"state": "deployed", "url": strings.TrimRight(url, "/")}
	render := cliui.New(ioctx.Out)
	human := render.Card("Deployment complete", bentotui.Field{Label: "Worker", Value: strings.TrimRight(url, "/")}, bentotui.Field{Label: "Status", Value: bentotui.Badge(render.Theme, render.Color, "READY", bentotui.VariantSuccess)})
	return writeSetupResult(ioctx.Out, opts.JSON, result, human)
}
