package mimircli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	mimirassets "github.com/cloudboy-jh/mimir"
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
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Connection verified")
	opts.Progress.Stop()
	return writeSetupResult(ioctx.Out, opts.JSON, addConnectionManifest(map[string]any{"state": "ready", "url": strings.TrimRight(url, "/"), "memory": true}, url), connectionSummary(url))
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

func setupStep(progress *setupProgress, out io.Writer, jsonOutput bool, label string) {
	if !jsonOutput {
		progress.Complete(label)
	}
}

func connectionSummary(url string) string {
	machine, _ := os.Hostname()
	if strings.TrimSpace(machine) == "" {
		machine = "registered"
	}
	credential, _ := tokenPath()
	manifest, _ := currentConnectionManifest(url)
	return fmt.Sprintf("Mimir connected\n\n  Worker      %s\n  Machine     %s\n  Credential  %s\n  OpenAI      %s\n  Anthropic   %s\n  MCP         mimir serve\n  Memory      enabled\n  Status      ready for harness connection", strings.TrimRight(url, "/"), machine, credential, manifest.OpenAIBaseURL, manifest.AnthropicBaseURL)
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

func verifyPointer(ctx context.Context, pointer Pointer) error {
	var last error
	for attempt := 0; attempt < 8; attempt++ {
		if _, err := remoteRequestWithPointer(ctx, pointer, "GET", "/whoami", nil); err == nil {
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
	assetSource := filepath.Join(filepath.Dir(source), "assets", "images", "mimir-readme.png")
	if pathExists(assetSource) {
		assetTarget := filepath.Join(filepath.Dir(target), "assets", "images", "mimir-readme.png")
		if err := os.MkdirAll(filepath.Dir(assetTarget), 0o700); err != nil {
			return "", err
		}
		data, err := os.ReadFile(assetSource)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(assetTarget, data, 0o600); err != nil {
			return "", err
		}
	}
	return target, nil
}

func ensureWorkerDependencies(ctx context.Context, dir string) error {
	hash, err := workerDependencyHash(dir)
	if err != nil {
		return err
	}
	markerPath := filepath.Join(dir, ".mimir-dependencies")
	marker, _ := os.ReadFile(markerPath)
	wranglerReady := pathExists(filepath.Join(dir, "node_modules", ".bin", "wrangler")) || pathExists(filepath.Join(dir, "node_modules", ".bin", "wrangler.cmd"))
	webReady := pathExists(filepath.Join(dir, "web", "node_modules", ".bin", "vite")) || pathExists(filepath.Join(dir, "web", "node_modules", ".bin", "vite.cmd"))
	if wranglerReady && webReady && strings.TrimSpace(string(marker)) == hash {
		return nil
	}
	if _, err := runCommand(ctx, dir, nil, "npm", "ci", "--silent"); err != nil {
		return err
	}
	if _, err := runCommand(ctx, filepath.Join(dir, "web"), nil, "bun", "install", "--frozen-lockfile"); err != nil {
		return err
	}
	return os.WriteFile(markerPath, []byte(hash+"\n"), 0o600)
}

func buildDashboard(ctx context.Context, dir string) error {
	_, err := runCommand(ctx, filepath.Join(dir, "web"), nil, "bun", "run", "build")
	return err
}

func workerDependencyHash(dir string) (string, error) {
	lock, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
	if err != nil {
		return "", fmt.Errorf("reading Worker package lock: %w", err)
	}
	webLock, err := os.ReadFile(filepath.Join(dir, "web", "bun.lock"))
	if err != nil {
		return "", fmt.Errorf("reading dashboard Bun lock: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(append(lock, webLock...))), nil
}

func runWrangler(ctx context.Context, dir string, stdin io.Reader, args ...string) (string, error) {
	local := filepath.Join(dir, "node_modules", ".bin", "wrangler")
	if pathExists(local) {
		return runCommand(ctx, dir, stdin, local, args...)
	}
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
	name, commandArgs := filepath.Join(dir, "node_modules", ".bin", "wrangler"), args
	if !pathExists(name) {
		name, commandArgs = "npx", append([]string{"wrangler"}, args...)
	}
	cmd := exec.CommandContext(ctx, name, commandArgs...)
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
func listedSecret(output, name string) bool {
	var secrets []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(output), &secrets) != nil {
		return false
	}
	for _, secret := range secrets {
		if secret.Name == name {
			return true
		}
	}
	return false
}
func workerURL(output string) string {
	return regexp.MustCompile(`https://[a-z0-9.-]+\.workers\.dev`).FindString(strings.ToLower(output))
}
func alreadyExists(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "already exists") || strings.Contains(lower, "already owned")
}

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
	}
	if !forceDiscovery {
		pointer, pointerErr := loadPointer()
		identity, identityErr := loadCloudflareIdentity()
		if pointerErr == nil && identityErr == nil {
			if _, err := remoteRequestWithPointer(ctx, pointer, "GET", "/whoami", nil); err == nil {
				return writeLoginResult(ioctx, opts.JSON, identity, pointer.URL)
			}
		}
	}
	dir, err := workerDir(opts.WorkerDir)
	if err != nil {
		return err
	}
	dir, err = materializeWorker(dir)
	if err != nil {
		return err
	}
	if err := ensureWorkerDependencies(ctx, dir); err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Worker prepared")
	identity, err := ensureCloudflareIdentity(ctx, dir, ioctx, opts.JSON)
	if err != nil {
		return err
	}
	setupStep(opts.Progress, ioctx.Out, opts.JSON, "Cloudflare authenticated")
	if pointer, err := loadPointer(); err == nil {
		if _, err := remoteRequestWithPointer(ctx, pointer, "GET", "/whoami", nil); err == nil {
			return writeLoginResult(ioctx, opts.JSON, identity, pointer.URL)
		}
	}
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
	return writeLoginResult(ioctx, opts.JSON, identity, url)
}

func writeLoginResult(ioctx IO, jsonOutput bool, identity cloudflareIdentity, url string) error {
	if err := saveCloudflareIdentity(identity); err != nil {
		return err
	}
	result := addConnectionManifest(map[string]any{"state": "connected", "url": url, "user": identity}, url)
	return writeSetupResult(ioctx.Out, jsonOutput, result, loginSummary(identity, url, terminalColor(ioctx.Out)))
}

func cloudflareIdentityPath() (string, error) {
	pointer, err := pointerPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(pointer), "cloudflare-user.json"), nil
}

func loadCloudflareIdentity() (cloudflareIdentity, error) {
	path, err := cloudflareIdentityPath()
	if err != nil {
		return cloudflareIdentity{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cloudflareIdentity{}, err
	}
	var identity cloudflareIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return cloudflareIdentity{}, err
	}
	if !identity.LoggedIn {
		return cloudflareIdentity{}, fmt.Errorf("cached Cloudflare user is not logged in")
	}
	return identity, nil
}

func saveCloudflareIdentity(identity cloudflareIdentity) error {
	path, err := cloudflareIdentityPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
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

func ensureCloudflareIdentity(ctx context.Context, dir string, ioctx IO, noninteractive bool) (cloudflareIdentity, error) {
	identity, err := readCloudflareIdentity(ctx, dir)
	if err == nil {
		return identity, nil
	}
	if noninteractive {
		return cloudflareIdentity{}, setupStateError{State: "cloudflare_auth_required", Message: "run wrangler login in an interactive terminal"}
	}
	fmt.Fprintln(ioctx.Out, "Cloudflare login required. Opening Wrangler authentication...")
	if err := runWranglerInteractive(ctx, dir, ioctx, "login"); err != nil {
		return cloudflareIdentity{}, fmt.Errorf("Cloudflare login failed: %w", err)
	}
	identity, err = readCloudflareIdentity(ctx, dir)
	if err != nil {
		return cloudflareIdentity{}, fmt.Errorf("Cloudflare login could not be verified: %w", err)
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

type connectionManifest struct {
	OpenAIBaseURL     string   `json:"openai_base_url"`
	AnthropicBaseURL  string   `json:"anthropic_base_url"`
	CredentialFile    string   `json:"credential_file"`
	CredentialCommand []string `json:"credential_command"`
	MCPCommand        []string `json:"mcp_command"`
	OptionalHeaders   []string `json:"optional_headers"`
}

func currentConnectionManifest(url string) (connectionManifest, error) {
	credential, err := tokenPath()
	if err != nil {
		return connectionManifest{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return connectionManifest{}, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return connectionManifest{}, err
	}
	base := strings.TrimRight(url, "/")
	return connectionManifest{
		OpenAIBaseURL:     base + "/v1",
		AnthropicBaseURL:  base,
		CredentialFile:    credential,
		CredentialCommand: []string{"cat", credential},
		MCPCommand:        []string{executable, "serve"},
		OptionalHeaders:   []string{"x-mimir-session", "x-mimir-repo", "x-mimir-harness"},
	}, nil
}

func writeConnectionManifest(out io.Writer) error {
	pointer, err := loadPointer()
	if err != nil {
		return err
	}
	manifest, err := currentConnectionManifest(pointer.URL)
	if err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}

func addConnectionManifest(result map[string]any, url string) map[string]any {
	if manifest, err := currentConnectionManifest(url); err == nil {
		result["connection"] = manifest
	}
	return result
}

const envMimirHome = "MIMIR_HOME"

type Pointer struct {
	URL   string
	Token string
}

func pointerPath() (string, error) {
	if home := strings.TrimSpace(os.Getenv(envMimirHome)); home != "" {
		return filepath.Join(home, "config"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mimir", "config"), nil
}

func tokenPath() (string, error) {
	path, err := pointerPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "token"), nil
}

func loadPointer() (Pointer, error) {
	path, err := pointerPath()
	if err != nil {
		return Pointer{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Pointer{}, fmt.Errorf("Mimir is not connected; run mimir setup")
		}
		return Pointer{}, err
	}
	var p Pointer
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"")
		switch strings.TrimSpace(key) {
		case "url":
			p.URL = strings.TrimRight(value, "/")
		}
	}
	tokenFile, err := tokenPath()
	if err != nil {
		return Pointer{}, err
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return Pointer{}, fmt.Errorf("Mimir machine token is missing; run mimir login")
	}
	p.Token = strings.TrimSpace(string(token))
	if p.URL == "" || p.Token == "" {
		return Pointer{}, fmt.Errorf("invalid Mimir pointer config: url and token are required")
	}
	return p, nil
}

func savePointer(p Pointer) error {
	if strings.TrimSpace(p.URL) == "" || strings.TrimSpace(p.Token) == "" {
		return fmt.Errorf("url and token are required")
	}
	path, err := pointerPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body := fmt.Sprintf("url = %q\n", strings.TrimRight(p.URL, "/"))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	tokenFile, err := tokenPath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(tokenFile, []byte(p.Token+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(tokenFile, 0o600)
}

type setupProgress struct {
	out     io.Writer
	enabled bool
	mu      sync.Mutex
	done    chan struct{}
	wg      sync.WaitGroup
	running bool
	phases  []string
	phase   int
}

func startSetupProgress(out io.Writer, phases []string) *setupProgress {
	progress := &setupProgress{out: out, phases: phases}
	file, ok := out.(*os.File)
	if !ok || !isTerminal(file) {
		return progress
	}
	progress.enabled = true
	progress.resumeCurrent()
	return progress
}

func (progress *setupProgress) resumeCurrent() {
	if progress == nil || !progress.enabled {
		return
	}
	if progress.phase >= len(progress.phases) {
		return
	}
	label := progress.phases[progress.phase]
	progress.mu.Lock()
	if progress.running {
		progress.mu.Unlock()
		return
	}
	progress.done = make(chan struct{})
	done := progress.done
	progress.running = true
	progress.wg.Add(1)
	progress.mu.Unlock()
	go func() {
		defer progress.wg.Done()
		frames := []byte{'|', '/', '-', '\\'}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; ; i++ {
			fmt.Fprintf(progress.out, "\r\x1b[2K\x1b[38;5;116m%c\x1b[0m %s", frames[i%len(frames)], label)
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (progress *setupProgress) Pause() {
	if progress == nil || !progress.enabled {
		return
	}
	progress.mu.Lock()
	if !progress.running {
		progress.mu.Unlock()
		return
	}
	done := progress.done
	progress.running = false
	progress.mu.Unlock()
	close(done)
	progress.wg.Wait()
	fmt.Fprint(progress.out, "\r\x1b[2K")
}

func (progress *setupProgress) Stop() { progress.Pause() }

func (progress *setupProgress) Resume() { progress.resumeCurrent() }

func (progress *setupProgress) Complete(label string) {
	if progress == nil || !progress.enabled {
		return
	}
	progress.Pause()
	fmt.Fprintf(progress.out, "\x1b[38;5;116m✓\x1b[0m %s\n", label)
	progress.phase++
	progress.resumeCurrent()
}

const setupLogoWidth = 64

func printSetupBanner(out io.Writer) {
	file, ok := out.(*os.File)
	if !ok || !isTerminal(file) {
		return
	}

	switch terminalImageProtocol() {
	case "kitty":
		writeKittyImage(out, mimirassets.LogoPNG, setupLogoWidth)
	case "iterm":
		writeITermImage(out, mimirassets.LogoPNG, setupLogoWidth)
	default:
		if err := writeANSIImage(out, mimirassets.LogoPNG, setupLogoWidth); err != nil {
			fmt.Fprintln(out, "\x1b[1;38;5;116m◆ mimir\x1b[0m")
		}
	}
	fmt.Fprintln(out)
}

func writeANSIImage(out io.Writer, data []byte, width int) error {
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	bounds := source.Bounds()
	// Half-blocks provide two vertical samples per terminal cell. A small
	// correction keeps the artwork from looking compressed in modern IDE
	// terminals whose cells are slightly shorter than the classic 1:2 ratio.
	height := max(2, bounds.Dy()*width*6/(bounds.Dx()*5))
	if height%2 != 0 {
		height++
	}
	for y := 0; y < height; y += 2 {
		for x := 0; x < width; x++ {
			upper := source.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+y*bounds.Dy()/height)
			lower := source.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+(y+1)*bounds.Dy()/height)
			ur, ug, ub, ua := upper.RGBA()
			lr, lg, lb, la := lower.RGBA()
			switch {
			case ua < 0x2000 && la < 0x2000:
				fmt.Fprint(out, "\x1b[0m ")
			case ua >= 0x2000 && la < 0x2000:
				fmt.Fprintf(out, "\x1b[38;2;%d;%d;%dm▀", ur>>8, ug>>8, ub>>8)
			case ua < 0x2000 && la >= 0x2000:
				fmt.Fprintf(out, "\x1b[38;2;%d;%d;%dm▄", lr>>8, lg>>8, lb>>8)
			default:
				fmt.Fprintf(out, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀", ur>>8, ug>>8, ub>>8, lr>>8, lg>>8, lb>>8)
			}
		}
		fmt.Fprintln(out, "\x1b[0m")
	}
	return nil
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func terminalImageProtocol() string {
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	term := strings.ToLower(os.Getenv("TERM"))
	switch {
	case os.Getenv("KITTY_WINDOW_ID") != "", strings.Contains(program, "ghostty"), strings.Contains(program, "wezterm"), strings.Contains(term, "kitty"):
		return "kitty"
	case strings.Contains(program, "iterm"), strings.Contains(program, "warp"), os.Getenv("LC_TERMINAL") == "iTerm2":
		return "iterm"
	default:
		return ""
	}
}

func writeITermImage(out io.Writer, image []byte, width int) {
	encoded := base64.StdEncoding.EncodeToString(image)
	fmt.Fprintf(out, "\x1b]1337;File=inline=1;width=%d;preserveAspectRatio=1:%s\a\n", width, encoded)
}

func writeKittyImage(out io.Writer, image []byte, width int) {
	const chunkSize = 4096
	encoded := base64.StdEncoding.EncodeToString(image)
	for offset := 0; offset < len(encoded); offset += chunkSize {
		end := min(offset+chunkSize, len(encoded))
		more := 0
		if end < len(encoded) {
			more = 1
		}
		if offset == 0 {
			fmt.Fprintf(out, "\x1b_Ga=T,f=100,t=d,c=%d,q=2,m=%d;%s\x1b\\", width, more, encoded[offset:end])
		} else {
			fmt.Fprintf(out, "\x1b_Gm=%d;%s\x1b\\", more, encoded[offset:end])
		}
	}
	fmt.Fprintln(out)
}

const (
	mimirGold       = "197;194;102" // #c5c266
	mimirForest     = "31;50;39"    // #1f3227
	mimirMint       = "126;192;164" // #7ec0a4
	mimirGreen      = "158;192;133" // #9ec085
	mimirTeal       = "30;107;113"  // #1e6b71
	mimirOlive      = "136;127;59"  // #887f3b
	mimirMutedGreen = "106;130;100" // #6a8264
)

func terminalColor(out io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	return ok && isTerminal(file)
}

func cliColor(enabled bool, text, rgb string, bold bool) string {
	if !enabled {
		return text
	}
	weight := ""
	if bold {
		weight = "1;"
	}
	return fmt.Sprintf("\x1b[%s38;2;%sm%s\x1b[0m", weight, rgb, text)
}
