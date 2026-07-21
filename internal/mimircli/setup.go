package mimircli

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

type setupStateError struct {
	State   string `json:"state"`
	Message string `json:"message"`
}

func (e setupStateError) Error() string {
	data, _ := json.Marshal(e)
	return string(data)
}

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
		if err := validateDeploymentURL(opts.URL); err != nil {
			return err
		}
		opts.Token = strings.TrimSpace(os.Getenv("MIMIR_TOKEN"))
		if opts.Token == "" && opts.JSON {
			return setupStateError{State: "mimir_token_required", Message: "set MIMIR_TOKEN to connect an existing endpoint"}
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
		pointer := Pointer{URL: strings.TrimRight(opts.URL, "/"), Token: opts.Token}
		if err := verifyPointer(ctx, pointer); err != nil {
			return fmt.Errorf("verifying existing deployment: %w", err)
		}
		if err := savePointer(pointer); err != nil {
			return err
		}
		setupStep(opts.Progress, ioctx.Out, opts.JSON, "Connection verified")
		opts.Progress.Stop()
		return writeSetupResult(ioctx.Out, opts.JSON, addConnectionManifest(map[string]any{"state": "connected", "url": strings.TrimRight(opts.URL, "/")}, opts.URL), connectionSummary(opts.URL))
	}
	return provision(ctx, opts, ioctx)
}

func provision(ctx context.Context, opts setupOptions, ioctx IO) error {
	dir, err := workerDir(opts.WorkerDir)
	if err != nil {
		return err
	}
	dir, err = materializeWorker(dir)
	if err != nil {
		return err
	}
	if err := ensureWorkerDependencies(ctx, dir); err != nil {
		return fmt.Errorf("installing Worker dependencies: %w", err)
	}
	if err := buildDashboard(ctx, dir); err != nil {
		return fmt.Errorf("building dashboard: %w", err)
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Worker prepared")
	if err := ensureCloudflareAuth(ctx, dir, ioctx, opts.JSON, opts.Progress); err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Cloudflare authenticated")
	if opts.DatabaseID == "" {
		output, err := runWrangler(ctx, dir, nil, "d1", "list", "--json")
		if err != nil {
			return err
		}
		opts.DatabaseID = listedDatabaseID(output, opts.DatabaseName)
		if opts.DatabaseID == "" {
			output, err = runWrangler(ctx, dir, nil, "d1", "create", opts.DatabaseName)
			if err != nil {
				return err
			}
			opts.DatabaseID = databaseID(output)
		}
		if opts.DatabaseID == "" {
			return fmt.Errorf("could not read the D1 database ID; retry with --database-id")
		}
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Database ready")
	if _, err := runWrangler(ctx, dir, nil, "r2", "bucket", "create", opts.BucketName); err != nil && !alreadyExists(err.Error()) {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Archive ready")
	if err := updateWranglerConfig(filepath.Join(dir, "wrangler.jsonc"), opts); err != nil {
		return err
	}
	if _, err := runWrangler(ctx, dir, nil, "d1", "migrations", "apply", opts.DatabaseName, "--remote"); err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Schema current")
	key := opts.OpenRouterKey
	if key == "" {
		key = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	secretOutput, secretErr := runWrangler(ctx, dir, nil, "secret", "list", "--format", "json")
	secretReady := secretErr == nil && listedSecret(secretOutput, "OPENROUTER_API_KEY")
	if key == "" && !secretReady {
		if opts.JSON {
			return setupStateError{State: "openrouter_key_required", Message: "set OPENROUTER_API_KEY and rerun setup"}
		}
		opts.Progress.Pause()
		key, err = promptSecret(ioctx, "OpenRouter API key: ")
		opts.Progress.Resume()
		if err != nil {
			return err
		}
	}
	if key == "" && !secretReady {
		return fmt.Errorf("OpenRouter API key is required")
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	if err := registerMachineToken(ctx, dir, opts.DatabaseName, token); err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Machine registered")
	if key != "" {
		if _, err := runWrangler(ctx, dir, strings.NewReader(key), "secret", "put", "OPENROUTER_API_KEY"); err != nil {
			return err
		}
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "OpenRouter connected")
	output, err := runWrangler(ctx, dir, nil, "deploy")
	if err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Worker deployed")
	url := workerURL(output)
	if url == "" {
		return fmt.Errorf("deployment succeeded but its workers.dev URL was not found; reconnect with mimir setup --url <url> --token <token>")
	}
	pointer := Pointer{URL: strings.TrimRight(url, "/"), Token: token}
	if err := verifyPointer(ctx, pointer); err != nil {
		return fmt.Errorf("Worker deployed but whoami verification failed: %w", err)
	}
	if err := savePointer(pointer); err != nil {
		return err
	}
	if err := storeDeploymentURL(ctx, dir, opts.DatabaseName, url); err != nil {
		return err
	}
	accessToken := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
	if accessToken == "" && !opts.JSON {
		opts.Progress.Pause()
		fmt.Fprint(ioctx.Out, accessTokenHint)
		accessToken, err = promptSecret(ioctx, "Cloudflare API token (enables automatic dashboard Access; Enter to skip): ")
		opts.Progress.Resume()
		if err != nil {
			return err
		}
	}
	access, err := setupDashboardAccess(ctx, dir, opts, url, accessToken)
	if err != nil {
		return fmt.Errorf("configuring dashboard Access: %w", err)
	}
	if access.State == "configured" {
		if _, err := runWrangler(ctx, dir, nil, "deploy"); err != nil {
			return fmt.Errorf("applying dashboard Access configuration: %w", err)
		}
		setupStep(opts.Progress, ioctx.Out, opts.JSON, "Dashboard Access configured")
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Connection verified")
	opts.Progress.Stop()
	result := map[string]any{"state": "ready", "url": strings.TrimRight(url, "/"), "memory": true, "access": access}
	human := connectionSummary(url)
	if access.State == "manual" && !opts.JSON {
		human += "\n\n" + accessChecklist(url)
	}
	return writeSetupResult(ioctx.Out, opts.JSON, addConnectionManifest(result, url), human)
}

func ensureCloudflareAuth(ctx context.Context, dir string, ioctx IO, noninteractive bool, progress *setupProgress) error {
	if _, err := runWrangler(ctx, dir, nil, "whoami"); err == nil {
		return nil
	}
	if noninteractive {
		return setupStateError{State: "cloudflare_auth_required", Message: "run wrangler login in an interactive terminal"}
	}
	progress.Pause()
	fmt.Fprintln(ioctx.Out, "Cloudflare login required. Opening Wrangler authentication...")
	if err := runWranglerInteractive(ctx, dir, ioctx, "login"); err != nil {
		return fmt.Errorf("Cloudflare login failed: %w", err)
	}
	progress.Resume()
	if _, err := runWrangler(ctx, dir, nil, "whoami"); err != nil {
		return fmt.Errorf("Cloudflare login could not be verified: %w", err)
	}
	return nil
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

func registerMachineToken(ctx context.Context, dir, database, token string) error {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	label, err := os.Hostname()
	if err != nil || strings.TrimSpace(label) == "" {
		label = "machine"
	}
	sql := fmt.Sprintf("INSERT INTO access_tokens(token_hash, label, created_at) VALUES('%s', '%s', '%s') ON CONFLICT(token_hash) DO UPDATE SET revoked_at = NULL", sqlQuote(hash), sqlQuote(label), time.Now().UTC().Format(time.RFC3339))
	if _, err := runWrangler(ctx, dir, nil, "d1", "execute", database, "--remote", "--command", sql); err != nil {
		return fmt.Errorf("registering this machine: %w", err)
	}
	return nil
}

func storeDeploymentURL(ctx context.Context, dir, database, url string) error {
	sql := fmt.Sprintf("INSERT INTO config(key, value) VALUES('deployment.url', '%s') ON CONFLICT(key) DO UPDATE SET value = excluded.value", sqlQuote(strings.TrimRight(url, "/")))
	_, err := runWrangler(ctx, dir, nil, "d1", "execute", database, "--remote", "--command", sql)
	return err
}

func sqlQuote(value string) string { return strings.ReplaceAll(value, "'", "''") }

func promptSecret(ioctx IO, label string) (string, error) {
	if _, err := fmt.Fprint(ioctx.Out, label); err != nil {
		return "", err
	}
	file, terminal := ioctx.In.(*os.File)
	if terminal && isTerminal(file) {
		disable := exec.Command("stty", "-echo")
		disable.Stdin = file
		if err := disable.Run(); err != nil {
			return "", fmt.Errorf("cannot securely hide secret input: %w", err)
		}
		defer func() {
			restore := exec.Command("stty", "echo")
			restore.Stdin = file
			_ = restore.Run()
			fmt.Fprintln(ioctx.Out)
		}()
	}
	line, err := bufio.NewReader(ioctx.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
