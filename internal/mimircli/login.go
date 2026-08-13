package mimircli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	cliui "github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func login(ctx context.Context, args []string, ioctx IO) error {
	opts := setupOptions{WorkerName: "mimir", DatabaseName: "mimir", BucketName: "mimir-logs"}
	forceDiscovery := strings.TrimSpace(os.Getenv("CLOUDFLARE_ACCOUNT_ID")) != ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--json" {
			opts.JSON = true
			continue
		}
		if i+1 >= len(args) {
			return fmt.Errorf("%s requires a value", args[i])
		}
		switch args[i] {
		case "--url":
			opts.URL = args[i+1]
			forceDiscovery = true
		case "--worker-dir":
			opts.WorkerDir = args[i+1]
			forceDiscovery = true
		case "--worker-name":
			opts.WorkerName = args[i+1]
			forceDiscovery = true
		case "--database-name":
			opts.DatabaseName = args[i+1]
			forceDiscovery = true
		case "--account-id":
			opts.AccountID = args[i+1]
			if strings.TrimSpace(opts.AccountID) == "" {
				return fmt.Errorf("--account-id requires a non-empty value")
			}
			forceDiscovery = true
		default:
			return fmt.Errorf("unknown login option %q", args[i])
		}
		i++
	}
	operationCtx, cancelOperation := context.WithCancel(ctx)
	defer cancelOperation()
	ctx = operationCtx
	if !opts.JSON {
		printSetupBanner(ioctx.Out)
		opts.Progress = startOperationProgress(ctx, ioctx, "Mimir login", []string{"Preparing Worker", "Authenticating Cloudflare", "Finding deployment", "Registering machine", "Verifying connection"}, cancelOperation)
		defer opts.Progress.Stop()
	}
	if !forceDiscovery {
		pointer, pointerErr := loadPointer()
		identity, identityErr := deployment.LoadIdentity()
		if pointerErr == nil && identityErr == nil {
			if _, err := remoteRequestWithPointer(ctx, pointer, "GET", "/whoami", nil); err == nil {
				setupStep(opts.Progress, ioctx.Out, opts.JSON, "Connection verified")
				opts.Progress.Finish("Login complete")
				opts.Progress.Stop()
				return writeLoginResult(ctx, ioctx, opts.JSON, identity, pointer.URL)
			}
		}
	}
	if opts.URL != "" {
		if err := mimirapi.ValidateDeploymentURL(opts.URL); err != nil {
			return err
		}
	}
	domainOpts := deployment.DefaultOptions()
	domainOpts.WorkerDir, domainOpts.WorkerName, domainOpts.DatabaseName, domainOpts.Noninteractive = opts.WorkerDir, opts.WorkerName, opts.DatabaseName, opts.JSON
	domainOpts.AccountID = opts.AccountID
	service := newDeploymentService(httpClient)
	if opts.Progress != nil {
		service.Wrangler = deployment.ObserveWrangler(service.Wrangler, opts.Progress.Output())
	}
	result, err := service.Login(ctx, domainOpts, deployment.Hooks{
		Streams: deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err},
		Step:    func(message string) { setupStep(opts.Progress, ioctx.Out, opts.JSON, message) },
		Login: func(ctx context.Context, dir string) error {
			return opts.Progress.Handoff("Waiting for Cloudflare authentication", func() error {
				cloudflareLoginNotice(ioctx.Out)
				return deployment.Wrangler{}.Interactive(ctx, dir, deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err}, "login")
			})
		},
		Verify: func(ctx context.Context, url, token string) error {
			return (mimirapi.Client{HTTPClient: httpClient, Pointer: mimirapi.Pointer{URL: url, Token: token}}).Verify(ctx)
		},
	}, opts.URL)
	if err != nil {
		opts.Progress.Fail()
		return err
	}
	pointer := mimirapi.Pointer{URL: result.URL, Token: result.Token}
	if err := savePointer(pointer); err != nil {
		opts.Progress.Fail()
		return err
	}
	opts.Progress.Finish("Login complete")
	opts.Progress.Stop()
	return writeLoginResult(ctx, ioctx, opts.JSON, result.Identity, result.URL)
}

func writeLoginResult(ctx context.Context, ioctx IO, jsonOutput bool, identity deployment.Identity, url string) error {
	if err := deployment.SaveIdentity(identity); err != nil {
		return err
	}
	lifecycle := refreshConnectedLifecycleIntegrations(ctx, "login")
	if !lifecycle.OK {
		fmt.Fprintf(ioctx.Err, "harness integration refresh warning: %s\n", lifecycle.Error)
	}
	integrations := lifecycle.Integrations
	result := addConnectionManifest(map[string]any{"state": "connected", "url": url, "user": identity, "artifacts": lifecycle.Artifacts, "integrations": integrations}, url)
	human := loginSummaryWithRenderer(identity, url, cliui.New(ioctx.Out))
	if summary := integrationSummary(integrations); summary != "" {
		human += "\n\n" + summary
	}
	return writeSetupResult(ioctx.Out, jsonOutput, result, human)
}

func loginSummary(identity deployment.Identity, url string, color bool) string {
	return loginSummaryWithRenderer(identity, url, cliui.Renderer{Color: color, Width: 80, Theme: bentotui.Mimir})
}

func loginSummaryWithRenderer(identity deployment.Identity, url string, render cliui.Renderer) string {
	accountNames := make([]string, 0, len(identity.Accounts))
	for _, account := range identity.Accounts {
		if account.Name != "" {
			accountNames = append(accountNames, account.Name)
		}
	}
	accounts := strings.Join(accountNames, ", ")
	if accounts == "" {
		accounts = "unavailable"
	}
	machine, _ := os.Hostname()
	if strings.TrimSpace(machine) == "" {
		machine = "registered"
	}

	cloudflare := render.KeyValues("Cloudflare",
		bentotui.Field{Label: "Email", Value: identity.Email},
		bentotui.Field{Label: "Account", Value: accounts},
		bentotui.Field{Label: "Auth", Value: identity.AuthType},
	)
	connection := render.KeyValues("Connection",
		bentotui.Field{Label: "Worker", Value: strings.TrimRight(url, "/")},
		bentotui.Field{Label: "Machine", Value: machine},
		bentotui.Field{Label: "Status", Value: bentotui.Badge(render.Theme, render.Color, "✓", bentotui.VariantSuccess) + " connected"},
	)
	return bentotui.Stack(cloudflare, connection)
}
