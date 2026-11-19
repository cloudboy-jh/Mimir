package mimircli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	"github.com/cloudboy-jh/mimir/internal/tailnet"
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
		case "--worker-dir":
			opts.WorkerDir = args[i+1]
		case "--worker-name":
			opts.WorkerName = args[i+1]
		case "--database-name":
			opts.DatabaseName = args[i+1]
		case "--account-id":
			opts.AccountID = args[i+1]
			if strings.TrimSpace(opts.AccountID) == "" {
				return fmt.Errorf("--account-id requires a non-empty value")
			}
		default:
			return fmt.Errorf("unknown login option %q", args[i])
		}
		forceDiscovery = true
		i++
	}
	if !forceDiscovery {
		pointer, pointerErr := loadPointer()
		identity, identityErr := deployment.LoadIdentity()
		if pointerErr == nil && identityErr == nil {
			client := mimirapi.Client{HTTPClient: httpClient, Pointer: pointer}
			if err := associateMachineIdentity(ctx, client); err == nil {
				setupStep(ioctx.Out, opts.JSON, "Connection verified")
				return writeLoginResult(ctx, ioctx, opts.JSON, identity, pointer)
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
	result, err := newDeploymentService(httpClient).Login(ctx, domainOpts, deployment.Hooks{
		Streams: deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err},
		Step:    func(message string) { setupStep(ioctx.Out, opts.JSON, message) },
		Login:   func(ctx context.Context, dir string) error { return cloudflareLogin(ctx, dir, ioctx) },
		Verify: func(ctx context.Context, url, token string) error {
			return (mimirapi.Client{HTTPClient: httpClient, Pointer: mimirapi.Pointer{URL: url, Token: token}}).Verify(ctx)
		},
	}, opts.URL)
	if err != nil {
		return err
	}
	pointer := mimirapi.Pointer{URL: result.URL, Token: result.Token}
	if err := savePointer(pointer); err != nil {
		return err
	}
	return writeLoginResult(ctx, ioctx, opts.JSON, result.Identity, pointer)
}

func writeLoginResult(ctx context.Context, ioctx IO, jsonOutput bool, identity deployment.Identity, pointer mimirapi.Pointer) error {
	if err := deployment.SaveIdentity(identity); err != nil {
		return err
	}
	saveCurrentExecutable(ioctx.Err)
	tailnetState := "unavailable"
	reported, err := tailnet.Report(ctx, mimirapi.Client{HTTPClient: httpClient, Pointer: pointer})
	if err != nil {
		setupWarning(ioctx.Err, "tailnet report failed: %v", err)
	} else if reported {
		tailnetState = "reported"
	}
	lifecycle := refreshConnectedLifecycleIntegrations(ctx, "login")
	if !lifecycle.OK {
		setupWarning(ioctx.Err, "harness integration refresh: %s", lifecycle.Error)
	}
	result := addConnectionManifest(map[string]any{"state": "connected", "url": pointer.URL, "user": identity, "tailnet": tailnetState, "artifacts": lifecycle.Artifacts, "integrations": lifecycle.Integrations}, pointer.URL)
	human := loginSummary(identity, pointer.URL)
	if summary := integrationSummary(lifecycle.Integrations); summary != "" {
		human += "\n\n" + summary
	}
	return writeSetupResult(ioctx.Out, jsonOutput, result, human)
}

func loginSummary(identity deployment.Identity, url string) string {
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
	return fmt.Sprintf("✓ Logged in\n  Email:   %s\n  Account: %s\n  Auth:    %s\n  Worker:  %s\n  Machine: %s",
		identity.Email, accounts, identity.AuthType, strings.TrimRight(url, "/"), machineName())
}

func machineName() string {
	name, _ := os.Hostname()
	if strings.TrimSpace(name) == "" {
		return "registered"
	}
	return name
}
