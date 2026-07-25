package mimircli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

type setupOptions struct {
	Mode          string
	URL           string
	Token         string
	WorkerDir     string
	WorkerName    string
	DatabaseName  string
	DatabaseID    string
	BucketName    string
	OpenRouterKey string
	AccessEmail   string
	JSON          bool
	Progress      *setupProgress
}

var newDeploymentService = deployment.NewService

func setup(ctx context.Context, args []string, ioctx IO) error {
	opts := setupOptions{Mode: "quick", WorkerName: "mimir", DatabaseName: "mimir", BucketName: "mimir-logs"}
	for i := 0; i < len(args); i++ {
		value := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", args[i])
			}
			i++
			return args[i], nil
		}
		switch args[i] {
		case "--quick":
			opts.Mode = "quick"
		case "--json":
			opts.JSON = true
		case "--url":
			var err error
			opts.URL, err = value()
			if err != nil {
				return err
			}
		case "--token":
			return fmt.Errorf("do not pass Mimir tokens as command arguments; use MIMIR_TOKEN or the secure prompt")
		case "--worker-dir":
			var err error
			opts.WorkerDir, err = value()
			if err != nil {
				return err
			}
		case "--worker-name":
			var err error
			opts.WorkerName, err = value()
			if err != nil {
				return err
			}
		case "--database-name":
			var err error
			opts.DatabaseName, err = value()
			if err != nil {
				return err
			}
		case "--database-id":
			var err error
			opts.DatabaseID, err = value()
			if err != nil {
				return err
			}
		case "--bucket-name":
			var err error
			opts.BucketName, err = value()
			if err != nil {
				return err
			}
		case "--access-email":
			var err error
			opts.AccessEmail, err = value()
			if err != nil {
				return err
			}
		case "--openrouter-key":
			return fmt.Errorf("do not pass OpenRouter keys as command arguments; enter it at the prompt")
		default:
			return fmt.Errorf("unknown setup option %q", args[i])
		}
	}
	if !opts.JSON {
		printSetupBanner(ioctx.Out)
		opts.Progress = startSetupProgress(ioctx.Out, []string{"Preparing Worker", "Authenticating Cloudflare", "Provisioning database", "Provisioning archive", "Applying schema", "Configuring credentials", "Connecting OpenRouter", "Deploying Worker", "Verifying connection"})
		defer func() { opts.Progress.Stop() }()
	}
	if opts.URL != "" {
		if err := mimirapi.ValidateDeploymentURL(opts.URL); err != nil {
			return err
		}
		opts.Token = strings.TrimSpace(os.Getenv("MIMIR_TOKEN"))
		if opts.Token == "" && opts.JSON {
			return deployment.StateError{State: "mimir_token_required", Message: "set MIMIR_TOKEN to connect an existing endpoint"}
		}
		if opts.Token == "" {
			var err error
			opts.Progress.Pause()
			opts.Token, err = promptSecret(ioctx, "Mimir token: ")
			opts.Progress.Resume()
			if err != nil {
				return err
			}
		}
		pointer := mimirapi.Pointer{URL: strings.TrimRight(opts.URL, "/"), Token: opts.Token}
		if err := (mimirapi.Client{HTTPClient: httpClient, Pointer: pointer}).Verify(ctx); err != nil {
			return fmt.Errorf("verifying existing deployment: %w", err)
		}
		if err := savePointer(pointer); err != nil {
			return err
		}
		lifecycle := refreshConnectedLifecycleIntegrations(ctx, "setup")
		if !lifecycle.OK {
			fmt.Fprintf(ioctx.Err, "harness integration refresh warning: %s\n", lifecycle.Error)
		}
		integrations := lifecycle.Integrations
		setupStep(opts.Progress, ioctx.Out, opts.JSON, "Connection verified")
		opts.Progress.Stop()
		result := addConnectionManifest(map[string]any{"state": "connected", "url": strings.TrimRight(opts.URL, "/"), "artifacts": lifecycle.Artifacts, "integrations": integrations}, opts.URL)
		human := connectionSummary(opts.URL)
		if summary := integrationSummary(integrations); summary != "" {
			human += "\n\n" + summary
		}
		return writeSetupResult(ioctx.Out, opts.JSON, result, human)
	}
	return provision(ctx, opts, ioctx)
}

func provision(ctx context.Context, opts setupOptions, ioctx IO) error {
	service := newDeploymentService(httpClient)
	domainOpts := deployment.DefaultOptions()
	domainOpts.WorkerDir, domainOpts.WorkerName = opts.WorkerDir, opts.WorkerName
	domainOpts.DatabaseName, domainOpts.DatabaseID, domainOpts.BucketName = opts.DatabaseName, opts.DatabaseID, opts.BucketName
	domainOpts.OpenRouterKey = strings.TrimSpace(opts.OpenRouterKey)
	if domainOpts.OpenRouterKey == "" {
		domainOpts.OpenRouterKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	domainOpts.AccessEmail = strings.TrimSpace(opts.AccessEmail)
	if domainOpts.AccessEmail == "" {
		domainOpts.AccessEmail = strings.TrimSpace(os.Getenv("MIMIR_ACCESS_EMAIL"))
	}
	domainOpts.Noninteractive = opts.JSON
	var lifecycle lifecyclepkg.Report
	domainResult, err := service.Provision(ctx, domainOpts, deployment.Hooks{
		Streams: deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err},
		Step:    func(message string) { setupStep(opts.Progress, ioctx.Out, opts.JSON, message) },
		Login: func(ctx context.Context, dir string) error {
			opts.Progress.Pause()
			defer opts.Progress.Resume()
			fmt.Fprintln(ioctx.Out, "Cloudflare login required. Opening Wrangler authentication...")
			return deployment.Wrangler{}.Interactive(ctx, dir, deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err}, "login")
		},
		PromptOpenRouterKey: func() (string, error) {
			opts.Progress.Pause()
			defer opts.Progress.Resume()
			return promptSecret(ioctx, "OpenRouter API key: ")
		},
		PromptAccessToken: func() (string, error) {
			token := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
			if token != "" || opts.JSON {
				return token, nil
			}
			opts.Progress.Pause()
			defer opts.Progress.Resume()
			fmt.Fprint(ioctx.Out, accessTokenHint)
			return promptSecret(ioctx, "Cloudflare API token (enables automatic dashboard Access; Enter to skip): ")
		},
		Verify: func(ctx context.Context, url, token string) error {
			return (mimirapi.Client{HTTPClient: httpClient, Pointer: mimirapi.Pointer{URL: url, Token: token}}).Verify(ctx)
		},
		Connected: func(ctx context.Context, url, token string) error {
			if err := savePointer(mimirapi.Pointer{URL: strings.TrimRight(url, "/"), Token: token}); err != nil {
				return err
			}
			lifecycle = refreshConnectedLifecycleIntegrations(ctx, "setup")
			if !lifecycle.OK {
				fmt.Fprintf(ioctx.Err, "harness integration refresh warning: %s\n", lifecycle.Error)
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	url, access := domainResult.URL, domainResult.Access
	integrations := lifecycle.Integrations
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Connection verified")
	opts.Progress.Stop()
	result := map[string]any{"state": "ready", "url": strings.TrimRight(url, "/"), "memory": true, "access": access, "artifacts": lifecycle.Artifacts, "integrations": integrations}
	human := connectionSummary(url)
	if summary := integrationSummary(integrations); summary != "" {
		human += "\n\n" + summary
	}
	if access.State != "configured" && !opts.JSON {
		human += "\n\n" + accessChecklist(url)
	}
	return writeSetupResult(ioctx.Out, opts.JSON, addConnectionManifest(result, url), human)
}

func writeSetupResult(out io.Writer, jsonOutput bool, result map[string]any, human string) error {
	if jsonOutput {
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	}
	_, err := fmt.Fprintln(out, human)
	return err
}

func promptSecret(ioctx IO, label string) (string, error) {
	if _, err := fmt.Fprint(ioctx.Out, label); err != nil {
		return "", err
	}
	file, terminal := ioctx.In.(*os.File)
	if terminal && isTerminal(file) {
		// Fall back to visible input when echo cannot be disabled; failing the
		// prompt outright would block the whole flow on exotic terminals.
		if restore, err := disableEcho(file); err == nil {
			defer func() {
				restore()
				fmt.Fprintln(ioctx.Out)
			}()
		}
	}
	line, err := bufio.NewReader(ioctx.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
