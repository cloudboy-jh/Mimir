package mimircli

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	"github.com/cloudboy-jh/mimir/internal/ui/lineoutput"
)

// deploy is the single supported way to ship Worker code and dashboard assets.
// It materializes the packaged Worker and compiled dashboard, writes the real
// D1 database ID into the materialized config, and runs wrangler deploy. An
// explicit development Worker override builds its dashboard before deployment.
func deploy(ctx context.Context, args []string, ioctx IO) error {
	opts, err := parseDeployOptions(args)
	if err != nil {
		return err
	}
	domainOpts := deployment.Options{WorkerDir: opts.WorkerDir, WorkerName: opts.WorkerName, DatabaseName: opts.DatabaseName, BucketName: opts.BucketName, AccountID: opts.AccountID}
	domainOpts.Noninteractive = opts.JSON
	lines := lineoutput.New(ioctx.Out)
	if !opts.JSON {
		_ = lines.Phase("Preparing deployment")
	}
	fallback := ""
	if pointer, err := loadPointer(); err == nil {
		fallback = pointer.URL
	}
	service := newDeploymentService(httpClient)
	domainResult, err := service.Deploy(ctx, domainOpts, deployment.Hooks{
		Streams: deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err},
		Step: func(message string) {
			if !opts.JSON {
				_ = lines.Phase(message)
			}
		},
		Login: func(ctx context.Context, dir string) error {
			if !opts.JSON {
				_ = lines.Warning("Cloudflare login required; opening Wrangler authentication")
			}
			return deployment.Wrangler{}.Interactive(ctx, dir, deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err}, "login")
		},
	}, fallback)
	if err != nil {
		return err
	}
	url := domainResult.URL
	result := map[string]any{"state": "deployed", "url": strings.TrimRight(url, "/")}
	if opts.JSON {
		return writeSetupResult(ioctx.Out, true, result, "")
	}
	if url == "" {
		return lines.Success("Deployment complete")
	}
	return lines.Success("Deployment complete: " + strings.TrimRight(url, "/"))
}

func parseDeployOptions(args []string) (setupOptions, error) {
	opts := setupOptions{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--json" {
			opts.JSON = true
			continue
		}
		if i+1 >= len(args) {
			return setupOptions{}, fmt.Errorf("%s requires a value", args[i])
		}
		switch args[i] {
		case "--worker-dir":
			opts.WorkerDir = args[i+1]
		case "--worker-name":
			opts.WorkerName = args[i+1]
		case "--database-name":
			opts.DatabaseName = args[i+1]
		case "--account-id":
			opts.AccountID = args[i+1]
			if strings.TrimSpace(opts.AccountID) == "" {
				return setupOptions{}, fmt.Errorf("--account-id requires a non-empty value")
			}
		default:
			return setupOptions{}, fmt.Errorf("unknown deploy option %q", args[i])
		}
		i++
	}
	return opts, nil
}
