package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

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
	setupStep(ioctx.Out, opts.JSON, "Worker package ready")
	if err := ensureCloudflareAuth(ctx, dir, ioctx, opts.JSON); err != nil {
		return err
	}
	setupStep(ioctx.Out, opts.JSON, "Cloudflare authenticated")
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
	if _, err := runWrangler(ctx, dir, nil, "d1", "migrations", "apply", opts.DatabaseName, "--remote"); err != nil {
		return err
	}
	setupStep(ioctx.Out, opts.JSON, "Existing deployment found")
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
	token, err := randomToken()
	if err != nil {
		return err
	}
	if err := registerMachineToken(ctx, dir, opts.DatabaseName, token); err != nil {
		return err
	}
	setupStep(ioctx.Out, opts.JSON, "This machine registered")
	if err := savePointer(Pointer{URL: url, Token: token}); err != nil {
		return err
	}
	if err := verifyDeployment(ctx); err != nil {
		return err
	}
	setupStep(ioctx.Out, opts.JSON, "Connection verified")
	return writeSetupResult(ioctx.Out, opts.JSON, map[string]any{"state": "connected", "url": url}, fmt.Sprintf("Connected this machine to %s", url))
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
