package mimircli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

func login(ctx context.Context, args []string, ioctx IO) error {
	opts := setupOptions{WorkerName: "mimir", DatabaseName: "mimir", BucketName: "mimir-logs"}
	forceDiscovery := false
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
		default:
			return fmt.Errorf("unknown login option %q", args[i])
		}
		i++
	}
	if !opts.JSON {
		printSetupBanner(ioctx.Out)
		opts.Progress = startProgress(ioctx.Out, "Mimir login", []string{"Preparing Worker", "Authenticating Cloudflare", "Finding deployment", "Registering machine", "Verifying connection"})
		defer opts.Progress.Stop()
	}
	if !forceDiscovery {
		pointer, pointerErr := loadPointer()
		identity, identityErr := deployment.LoadIdentity()
		if pointerErr == nil && identityErr == nil {
			if _, err := remoteRequestWithPointer(ctx, pointer, "GET", "/whoami", nil); err == nil {
				setupStep(opts.Progress, ioctx.Out, opts.JSON, "Connection verified")
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
	result, err := deployment.NewService(httpClient).Login(ctx, domainOpts, deployment.Hooks{
		Streams: deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err},
		Step:    func(message string) { setupStep(opts.Progress, ioctx.Out, opts.JSON, message) },
		Login: func(ctx context.Context, dir string) error {
			opts.Progress.Pause()
			defer opts.Progress.Resume()
			fmt.Fprintln(ioctx.Out, "Cloudflare login required. Opening Wrangler authentication...")
			return deployment.Wrangler{}.Interactive(ctx, dir, deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err}, "login")
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
		return err
	}
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
	human := loginSummary(identity, url, terminalColor(ioctx.Out))
	if summary := integrationSummary(integrations); summary != "" {
		human += "\n\n" + summary
	}
	return writeSetupResult(ioctx.Out, jsonOutput, result, human)
}

func loginSummary(identity deployment.Identity, url string, color bool) string {
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

	var summary strings.Builder
	fmt.Fprintln(&summary, cliColor(color, "◆ Cloudflare", mimirMint, true))
	writeSummaryRow(&summary, color, "Email", identity.Email)
	writeSummaryRow(&summary, color, "Account", accounts)
	writeSummaryRow(&summary, color, "Auth", identity.AuthType)
	fmt.Fprintln(&summary)
	fmt.Fprintln(&summary, cliColor(color, "◆ Connection", mimirMint, true))
	writeSummaryRow(&summary, color, "Worker", strings.TrimRight(url, "/"))
	writeSummaryRow(&summary, color, "Machine", machine)
	status := cliColor(color, "✓", mimirGreen, true) + " connected"
	writeSummaryRow(&summary, color, "Status", status)
	return strings.TrimRight(summary.String(), "\n")
}

func writeSummaryRow(out io.Writer, color bool, label, value string) {
	if strings.TrimSpace(value) == "" {
		value = "unavailable"
	}
	fmt.Fprintf(out, "  %s %s\n", cliColor(color, fmt.Sprintf("%-9s", label+":"), mimirMutedGreen, false), value)
}
