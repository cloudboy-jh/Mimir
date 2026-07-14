package main

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
	"regexp"
	"sort"
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
}

func setup(ctx context.Context, args []string, ioctx IO) error {
	printSetupBanner(ioctx.Out)
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
		case "--quick", "--full", "--minimal":
			opts.Mode = strings.TrimPrefix(args[i], "--")
		case "--url":
			var err error
			opts.URL, err = value()
			if err != nil {
				return err
			}
		case "--token":
			var err error
			opts.Token, err = value()
			if err != nil {
				return err
			}
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
		case "--openrouter-key":
			return fmt.Errorf("do not pass OpenRouter keys as command arguments; enter it at the prompt")
		default:
			return fmt.Errorf("unknown setup option %q", args[i])
		}
	}
	if opts.URL != "" || opts.Token != "" {
		if opts.URL == "" || opts.Token == "" {
			return fmt.Errorf("--url and --token must be supplied together")
		}
		return connectPointer(ioctx.Out, opts.URL, opts.Token)
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
	if _, err := runCommand(ctx, dir, nil, "npm", "ci", "--silent"); err != nil {
		return fmt.Errorf("installing Worker dependencies: %w", err)
	}
	if err := ensureCloudflareAuth(ctx, dir, ioctx); err != nil {
		return err
	}
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
	if _, err := runWrangler(ctx, dir, nil, "r2", "bucket", "create", opts.BucketName); err != nil && !alreadyExists(err.Error()) {
		return err
	}
	if err := updateWranglerConfig(filepath.Join(dir, "wrangler.jsonc"), opts); err != nil {
		return err
	}
	if _, err := runWrangler(ctx, dir, nil, "d1", "migrations", "apply", opts.DatabaseName, "--remote"); err != nil {
		return err
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	if err := registerMachineToken(ctx, dir, opts.DatabaseName, token); err != nil {
		return err
	}
	key := opts.OpenRouterKey
	if key == "" {
		key = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	if key == "" {
		key, err = promptSecret(ioctx, "OpenRouter API key: ")
		if err != nil {
			return err
		}
	}
	if key == "" {
		return fmt.Errorf("OpenRouter API key is required")
	}
	if _, err := runWrangler(ctx, dir, strings.NewReader(key), "secret", "put", "OPENROUTER_API_KEY"); err != nil {
		return err
	}
	output, err := runWrangler(ctx, dir, nil, "deploy")
	if err != nil {
		return err
	}
	url := workerURL(output)
	if url == "" {
		return fmt.Errorf("deployment succeeded but its workers.dev URL was not found; reconnect with mimir setup --url <url> --token <token>")
	}
	if err := savePointer(Pointer{URL: strings.TrimRight(url, "/"), Token: token}); err != nil {
		return err
	}
	if err := storeDeploymentURL(ctx, dir, opts.DatabaseName, url); err != nil {
		return err
	}
	if opts.Mode == "minimal" {
		if _, err := remoteRequest(ctx, "PUT", "/config", map[string]bool{"save.enabled": false}); err != nil {
			return fmt.Errorf("Worker deployed but minimal config could not be applied: %w", err)
		}
	}
	if err := verifyDeployment(ctx); err != nil {
		return fmt.Errorf("Worker deployed but whoami verification failed: %w", err)
	}
	_, err = fmt.Fprintln(ioctx.Out, "Mimir connected, deployed, and verified.")
	return err
}

func ensureCloudflareAuth(ctx context.Context, dir string, ioctx IO) error {
	if _, err := runWrangler(ctx, dir, nil, "whoami"); err == nil {
		return nil
	}
	fmt.Fprintln(ioctx.Out, "Cloudflare login required. Opening Wrangler authentication...")
	if err := runWranglerInteractive(ctx, dir, ioctx, "login"); err != nil {
		return fmt.Errorf("Cloudflare login failed: %w", err)
	}
	if _, err := runWrangler(ctx, dir, nil, "whoami"); err != nil {
		return fmt.Errorf("Cloudflare login could not be verified: %w", err)
	}
	return nil
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

func verifyDeployment(ctx context.Context) error {
	var last error
	for attempt := 0; attempt < 8; attempt++ {
		if _, err := remoteRequest(ctx, "GET", "/whoami", nil); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return last
}

func connectPointer(out io.Writer, url, token string) error {
	if err := savePointer(Pointer{URL: strings.TrimRight(url, "/"), Token: token}); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "Mimir connected.")
	return err
}

func workerDir(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		for _, candidate := range []string{dir, filepath.Join(dir, "worker")} {
			if pathExists(filepath.Join(candidate, "wrangler.jsonc")) {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	cache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err == nil {
		matches, _ := filepath.Glob(filepath.Join(strings.TrimSpace(string(cache)), "github.com", "cloudboy-jh", "mimir@*", "worker", "wrangler.jsonc"))
		if len(matches) > 0 {
			sort.Strings(matches)
			return filepath.Dir(matches[len(matches)-1]), nil
		}
	}
	return "", fmt.Errorf("could not find the Mimir Worker package; install with go install github.com/cloudboy-jh/mimir/cmd/mimir@latest or pass --worker-dir")
}

// materializeWorker keeps Wrangler's generated config and node modules outside
// the source checkout and Go's read-only module cache.
func materializeWorker(source string) (string, error) {
	pointer, err := pointerPath()
	if err != nil {
		return "", err
	}
	target := filepath.Join(filepath.Dir(pointer), "worker")
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(target, 0o700)
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == ".wrangler" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(target, rel), 0o700)
		}
		if strings.HasPrefix(rel, "node_modules"+string(filepath.Separator)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(target, rel), data, 0o600)
	}); err != nil {
		return "", err
	}
	return target, nil
}

func runWrangler(ctx context.Context, dir string, stdin io.Reader, args ...string) (string, error) {
	return runCommand(ctx, dir, stdin, "npx", append([]string{"wrangler"}, args...)...)
}

func runCommand(ctx context.Context, dir string, stdin io.Reader, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Stdin = dir, stdin
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}

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

func runWranglerInteractive(ctx context.Context, dir string, ioctx IO, args ...string) error {
	cmd := exec.CommandContext(ctx, "npx", append([]string{"wrangler"}, args...)...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = dir, ioctx.In, ioctx.Out, ioctx.Err
	return cmd.Run()
}

func updateWranglerConfig(path string, opts setupOptions) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	config["name"] = opts.WorkerName
	if databases, ok := config["d1_databases"].([]any); ok && len(databases) == 1 {
		if database, ok := databases[0].(map[string]any); ok {
			database["database_name"], database["database_id"] = opts.DatabaseName, opts.DatabaseID
		}
	}
	if buckets, ok := config["r2_buckets"].([]any); ok && len(buckets) == 1 {
		if bucket, ok := buckets[0].(map[string]any); ok {
			bucket["bucket_name"] = opts.BucketName
		}
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
func databaseID(output string) string {
	return regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f-]{27,}`).FindString(strings.ToLower(output))
}
func listedDatabaseID(output, name string) string {
	var databases []struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(output), &databases) != nil {
		return ""
	}
	for _, database := range databases {
		if database.Name == name {
			return database.UUID
		}
	}
	return ""
}
func workerURL(output string) string {
	return regexp.MustCompile(`https://[a-z0-9.-]+\.workers\.dev`).FindString(strings.ToLower(output))
}
func alreadyExists(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "already exists") || strings.Contains(lower, "already owned")
}
