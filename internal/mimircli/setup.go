package mimircli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

type setupOptions struct {
	URL           string
	Token         string
	WorkerDir     string
	WorkerName    string
	DatabaseName  string
	DatabaseID    string
	BucketName    string
	AccountID     string
	OpenRouterKey string
	AccessEmail   string
	JSON          bool
}

var newDeploymentService = deployment.NewService

func setup(ctx context.Context, args []string, ioctx IO) error {
	opts := setupOptions{WorkerName: "mimir", DatabaseName: "mimir", BucketName: "mimir-logs"}
	for i := 0; i < len(args); i++ {
		value := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", args[i])
			}
			i++
			return args[i], nil
		}
		var err error
		switch args[i] {
		case "--quick":
		case "--json":
			opts.JSON = true
		case "--url":
			opts.URL, err = value()
		case "--token":
			return fmt.Errorf("do not pass Mimir tokens as command arguments; use MIMIR_TOKEN or the secure prompt")
		case "--worker-dir":
			opts.WorkerDir, err = value()
		case "--worker-name":
			opts.WorkerName, err = value()
		case "--database-name":
			opts.DatabaseName, err = value()
		case "--database-id":
			opts.DatabaseID, err = value()
		case "--bucket-name":
			opts.BucketName, err = value()
		case "--account-id":
			opts.AccountID, err = value()
			if err == nil && strings.TrimSpace(opts.AccountID) == "" {
				err = fmt.Errorf("--account-id requires a non-empty value")
			}
		case "--access-email":
			opts.AccessEmail, err = value()
		case "--openrouter-key":
			return fmt.Errorf("do not pass OpenRouter keys as command arguments; enter it at the prompt")
		default:
			return fmt.Errorf("unknown setup option %q", args[i])
		}
		if err != nil {
			return err
		}
	}
	if opts.URL == "" {
		return provision(ctx, opts, ioctx)
	}
	if err := mimirapi.ValidateDeploymentURL(opts.URL); err != nil {
		return err
	}
	url := strings.TrimRight(opts.URL, "/")
	opts.Token = strings.TrimSpace(os.Getenv("MIMIR_TOKEN"))
	if opts.Token == "" && opts.JSON {
		return deployment.StateError{State: "mimir_token_required", Message: "set MIMIR_TOKEN to connect an existing endpoint"}
	}
	if opts.Token == "" {
		var err error
		if opts.Token, err = promptSecret(ioctx, "Mimir token: "); err != nil {
			return err
		}
	}
	pointer := mimirapi.Pointer{URL: url, Token: opts.Token}
	client := mimirapi.Client{HTTPClient: httpClient, Pointer: pointer}
	if err := client.Verify(ctx); err != nil {
		return fmt.Errorf("verifying existing deployment: %w", err)
	}
	if err := associateMachineIdentity(ctx, client); err != nil {
		return fmt.Errorf("associating this machine: %w", err)
	}
	if err := savePointer(pointer); err != nil {
		return err
	}
	saveCurrentExecutable(ioctx.Err)
	lifecycle := refreshConnectedLifecycleIntegrations(ctx, "setup")
	if !lifecycle.OK {
		setupWarning(ioctx.Err, "harness integration refresh: %s", lifecycle.Error)
	}
	setupStep(ioctx.Out, opts.JSON, "Connection verified")
	result := addConnectionManifest(map[string]any{"state": "connected", "url": url, "artifacts": lifecycle.Artifacts, "integrations": lifecycle.Integrations}, url)
	human := connectionSummary(url)
	if summary := integrationSummary(lifecycle.Integrations); summary != "" {
		human += "\n\n" + summary
	}
	return writeSetupResult(ioctx.Out, opts.JSON, result, human)
}

func provision(ctx context.Context, opts setupOptions, ioctx IO) error {
	service := newDeploymentService(httpClient)
	domainOpts := deployment.DefaultOptions()
	domainOpts.WorkerDir, domainOpts.WorkerName = opts.WorkerDir, opts.WorkerName
	domainOpts.DatabaseName, domainOpts.DatabaseID, domainOpts.BucketName = opts.DatabaseName, opts.DatabaseID, opts.BucketName
	domainOpts.AccountID = opts.AccountID
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
		Step:    func(message string) { setupStep(ioctx.Out, opts.JSON, message) },
		Login:   func(ctx context.Context, dir string) error { return cloudflareLogin(ctx, dir, ioctx) },
		PromptOpenRouterKey: func() (string, error) {
			return promptSecret(ioctx, "OpenRouter API key: ")
		},
		PromptAccessToken: func() (string, error) {
			token := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
			if token != "" || opts.JSON {
				return token, nil
			}
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
			saveCurrentExecutable(ioctx.Err)
			lifecycle = refreshConnectedLifecycleIntegrations(ctx, "setup")
			if !lifecycle.OK {
				setupWarning(ioctx.Err, "harness integration refresh: %s", lifecycle.Error)
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	url, access := strings.TrimRight(domainResult.URL, "/"), domainResult.Access
	setupStep(ioctx.Out, opts.JSON, "Connection verified")
	result := map[string]any{"state": "ready", "url": url, "memory": true, "access": access, "artifacts": lifecycle.Artifacts, "integrations": lifecycle.Integrations}
	human := connectionSummary(url)
	if summary := integrationSummary(lifecycle.Integrations); summary != "" {
		human += "\n\n" + summary
	}
	if access.State != "configured" {
		human += "\n\n" + accessChecklist(url)
	}
	return writeSetupResult(ioctx.Out, opts.JSON, addConnectionManifest(result, url), human)
}

// saveCurrentExecutable records the running binary in $MIMIR_HOME/config so
// plugins can launch Mimir without PATH. Failure only warns.
func saveCurrentExecutable(errOut io.Writer) {
	path, err := executablePath()
	if err == nil {
		path, err = filepath.EvalSymlinks(path)
	}
	if err == nil {
		err = mimirapi.SaveExecutable(path)
	}
	if err != nil {
		setupWarning(errOut, "could not record the mimir executable path: %v", err)
	}
}

func cloudflareLogin(ctx context.Context, dir string, ioctx IO) error {
	fmt.Fprintln(ioctx.Err, "Cloudflare login required; opening Wrangler authentication in your browser.")
	return deployment.Wrangler{}.Interactive(ctx, dir, deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err}, "login")
}

func setupStep(out io.Writer, jsonOutput bool, label string) {
	if !jsonOutput {
		fmt.Fprintln(out, "✓ "+label)
	}
}

func setupWarning(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, "! "+format+"\n", args...)
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

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func promptValue(ioctx IO, label string) (string, error) {
	if _, err := fmt.Fprint(ioctx.Out, label); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(ioctx.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
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
