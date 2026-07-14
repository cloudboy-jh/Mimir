package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type cloudflareIdentity struct {
	LoggedIn bool   `json:"loggedIn"`
	AuthType string `json:"authType"`
	Email    string `json:"email"`
	Accounts []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"accounts"`
}

func login(ctx context.Context, args []string, ioctx IO) error {
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
		case "--url":
			opts.URL = args[i+1]
		case "--worker-dir":
			opts.WorkerDir = args[i+1]
		case "--worker-name":
			opts.WorkerName = args[i+1]
		case "--database-name":
			opts.DatabaseName = args[i+1]
		default:
			return fmt.Errorf("unknown login option %q", args[i])
		}
		i++
	}
	if !opts.JSON {
		printSetupBanner(ioctx.Out)
	}
	dir, err := workerDir(opts.WorkerDir)
	if err != nil {
		return err
	}
	dir, err = materializeWorker(dir)
	if err != nil {
		return err
	}
	if _, err := runCommand(ctx, dir, nil, "npm", "ci", "--silent"); err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Worker prepared")
	if err := ensureCloudflareAuth(ctx, dir, ioctx, opts.JSON, opts.Progress); err != nil {
		return err
	}
	identity, err := readCloudflareIdentity(ctx, dir)
	if err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Cloudflare authenticated")
	output, err := runWrangler(ctx, dir, nil, "d1", "list", "--json")
	if err != nil {
		return err
	}
	opts.DatabaseID = listedDatabaseID(output, opts.DatabaseName)
	if opts.DatabaseID == "" {
		return setupStateError{State: "deployment_missing", Message: "no Mimir deployment found in this Cloudflare account"}
	}
	if err := updateWranglerConfig(filepath.Join(dir, "wrangler.jsonc"), opts); err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Deployment found")
	url := strings.TrimRight(opts.URL, "/")
	if url == "" {
		url, err = deploymentURL(ctx, dir, opts.DatabaseName)
		if err != nil {
			return err
		}
	}
	if url == "" {
		return setupStateError{State: "deployment_url_missing", Message: "run mimir login --url <worker-url>"}
	}
	if err := validateDeploymentURL(url); err != nil {
		return err
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	if err := registerMachineToken(ctx, dir, opts.DatabaseName, token); err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Machine registered")
	pointer := Pointer{URL: url, Token: token}
	if err := verifyPointer(ctx, pointer); err != nil {
		return err
	}
	if err := savePointer(pointer); err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Connection verified")
	result := addConnectionManifest(map[string]any{"state": "connected", "url": url, "user": identity}, url)
	return writeSetupResult(ioctx.Out, opts.JSON, result, loginSummary(identity, url, terminalColor(ioctx.Out)))
}

func readCloudflareIdentity(ctx context.Context, dir string) (cloudflareIdentity, error) {
	output, err := runWrangler(ctx, dir, nil, "whoami", "--json")
	if err != nil {
		return cloudflareIdentity{}, fmt.Errorf("reading Cloudflare user: %w", err)
	}
	var identity cloudflareIdentity
	if err := json.Unmarshal([]byte(output), &identity); err != nil {
		return cloudflareIdentity{}, fmt.Errorf("reading Cloudflare user: %w", err)
	}
	if !identity.LoggedIn {
		return cloudflareIdentity{}, fmt.Errorf("Cloudflare user is not logged in")
	}
	return identity, nil
}

func loginSummary(identity cloudflareIdentity, url string, color bool) string {
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

func deploymentURL(ctx context.Context, dir, database string) (string, error) {
	output, err := runWrangler(ctx, dir, nil, "d1", "execute", database, "--remote", "--command", "SELECT value FROM config WHERE key = 'deployment.url'", "--json")
	if err != nil {
		return "", err
	}
	return parseDeploymentURL(output)
}

func parseDeploymentURL(output string) (string, error) {
	var result []struct {
		Results []struct {
			Value string `json:"value"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return "", err
	}
	if len(result) == 0 || len(result[0].Results) == 0 {
		return "", nil
	}
	return strings.TrimRight(result[0].Results[0].Value, "/"), nil
}
