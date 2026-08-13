package deployment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/cloudboy-jh/mimir/internal/jsonconfig"
)

type Streams struct {
	In       io.Reader
	Out, Err io.Writer
}

type Config struct {
	WorkerName   string
	DatabaseName string
	DatabaseID   string
	BucketName   string
}

type WranglerClient interface {
	Run(context.Context, string, io.Reader, ...string) (string, error)
	Interactive(context.Context, string, Streams, ...string) error
	UpdateConfig(string, Config) error
	UpdateVars(string, map[string]string) error
}

type observingWrangler struct {
	base WranglerClient
	out  io.Writer
}

func ObserveWrangler(base WranglerClient, out io.Writer) WranglerClient {
	if base == nil || out == nil {
		return base
	}
	if wrangler, ok := base.(Wrangler); ok {
		wrangler.observe = out
		return wrangler
	}
	return observingWrangler{base: base, out: out}
}

func (w observingWrangler) Run(ctx context.Context, dir string, input io.Reader, args ...string) (string, error) {
	output, err := w.base.Run(ctx, dir, input, args...)
	if strings.TrimSpace(output) != "" {
		_, _ = io.WriteString(w.out, output+"\n")
	}
	if err != nil {
		_, _ = io.WriteString(w.out, err.Error()+"\n")
	}
	flushOutput(w.out)
	return output, err
}

func (w observingWrangler) Interactive(ctx context.Context, dir string, streams Streams, args ...string) error {
	return w.base.Interactive(ctx, dir, streams, args...)
}

func (w observingWrangler) UpdateConfig(path string, config Config) error {
	return w.base.UpdateConfig(path, config)
}

func (w observingWrangler) UpdateVars(path string, vars map[string]string) error {
	return w.base.UpdateVars(path, vars)
}

type Wrangler struct {
	observe io.Writer
}

type commandOutput struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	observe io.Writer
}

func (w *commandOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.buffer.Write(data)
	if w.observe != nil {
		_, _ = w.observe.Write(data)
	}
	return len(data), nil
}

func (w *commandOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

func (w Wrangler) Run(ctx context.Context, dir string, stdin io.Reader, args ...string) (string, error) {
	if accountID := wranglerAccountID(ctx); accountID != "" {
		if _, err := removeMismatchedWranglerAccountCache(dir, accountID); err != nil {
			return "", err
		}
	}
	return w.run(ctx, dir, stdin, args...)
}

func (w Wrangler) run(ctx context.Context, dir string, stdin io.Reader, args ...string) (string, error) {
	name, commandArgs := wranglerCommand(dir, args)
	cmd := exec.CommandContext(ctx, name, commandArgs...)
	cmd.Dir, cmd.Stdin = dir, stdin
	cmd.Env = wranglerEnvironment(os.Environ(), wranglerAccountID(ctx))
	var output string
	var err error
	if w.observe == nil {
		data, runErr := cmd.CombinedOutput()
		output, err = string(data), runErr
	} else {
		capture := &commandOutput{observe: w.observe}
		cmd.Stdout, cmd.Stderr = capture, capture
		err = cmd.Run()
		output = capture.String()
	}
	if err != nil {
		flushOutput(w.observe)
		return "", fmt.Errorf("%s %s: %s: %w", name, strings.Join(commandArgs, " "), strings.TrimSpace(output), err)
	}
	flushOutput(w.observe)
	return output, nil
}

func flushOutput(out io.Writer) {
	if flusher, ok := out.(interface{ Flush() }); ok {
		flusher.Flush()
	}
}

func (Wrangler) Interactive(ctx context.Context, dir string, streams Streams, args ...string) error {
	name, commandArgs := wranglerCommand(dir, args)
	cmd := exec.CommandContext(ctx, name, commandArgs...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = dir, streams.In, streams.Out, streams.Err
	cmd.Env = wranglerEnvironment(os.Environ(), wranglerAccountID(ctx))
	return cmd.Run()
}

func wranglerEnvironment(current []string, accountID string) []string {
	if strings.TrimSpace(accountID) == "" {
		return current
	}
	env := make([]string, 0, len(current)+1)
	for _, entry := range current {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, "CLOUDFLARE_ACCOUNT_ID") {
			env = append(env, entry)
		}
	}
	return append(env, "CLOUDFLARE_ACCOUNT_ID="+accountID)
}

func removeMismatchedWranglerAccountCache(dir, accountID string) (bool, error) {
	if strings.TrimSpace(accountID) == "" {
		return false, nil
	}
	cacheDir := dir
	for _, component := range []string{"node_modules", ".cache", "wrangler"} {
		cacheDir = filepath.Join(cacheDir, component)
		info, err := os.Lstat(cacheDir)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("checking Wrangler account cache directory %s: %w", cacheDir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false, fmt.Errorf("Wrangler account cache directory %s is not a real directory; remove the stale cache manually", cacheDir)
		}
	}
	path := filepath.Join(cacheDir, "wrangler-account.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking Wrangler account cache %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("Wrangler account cache %s is not a regular file; remove it manually before deploying", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("reading Wrangler account cache %s: %w", path, err)
	}
	var cached struct {
		Account struct {
			ID string `json:"id"`
		} `json:"account"`
	}
	if json.Unmarshal(data, &cached) != nil || strings.TrimSpace(cached.Account.ID) == "" || strings.TrimSpace(cached.Account.ID) == strings.TrimSpace(accountID) {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("removing stale Wrangler account cache %s: %w", path, err)
	}
	return true, nil
}

func wranglerCommand(dir string, args []string) (string, []string) {
	local := filepath.Join(dir, "node_modules", ".bin", "wrangler")
	if runtime.GOOS == "windows" && pathExists(local+".cmd") {
		return local + ".cmd", args
	}
	if pathExists(local) {
		return local, args
	}
	return "npx", append([]string{"wrangler"}, args...)
}

func (Wrangler) UpdateConfig(path string, opts Config) error {
	config, err := jsonconfig.Read(path)
	if err != nil {
		return err
	}
	database, err := expectedD1Binding(config)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.DatabaseID) == "" {
		return fmt.Errorf("D1 binding DB requires a resolved database ID")
	}
	config["name"] = opts.WorkerName
	database["database_name"], database["database_id"] = opts.DatabaseName, opts.DatabaseID
	if buckets, ok := config["r2_buckets"].([]any); ok && len(buckets) == 1 {
		if bucket, ok := buckets[0].(map[string]any); ok {
			bucket["bucket_name"] = opts.BucketName
		}
	}
	if err := jsonconfig.Write(path, config); err != nil {
		return err
	}
	written, err := jsonconfig.Read(path)
	if err != nil {
		return fmt.Errorf("verifying Worker configuration: %w", err)
	}
	writtenDatabase, err := expectedD1Binding(written)
	if err != nil {
		return fmt.Errorf("verifying Worker configuration: %w", err)
	}
	if deployedID, _ := writtenDatabase["database_id"].(string); deployedID != opts.DatabaseID {
		return fmt.Errorf("Worker D1 binding DB uses database ID %q, want resolved ID %q", deployedID, opts.DatabaseID)
	}
	return nil
}

func expectedD1Binding(config map[string]any) (map[string]any, error) {
	databases, ok := config["d1_databases"].([]any)
	if !ok || len(databases) != 1 {
		return nil, fmt.Errorf("Worker configuration must contain exactly one D1 binding named DB")
	}
	database, ok := databases[0].(map[string]any)
	if !ok || database["binding"] != "DB" {
		return nil, fmt.Errorf("Worker configuration must contain exactly one D1 binding named DB")
	}
	return database, nil
}

func (Wrangler) UpdateVars(path string, vars map[string]string) error {
	return jsonconfig.UpdateVars(path, vars)
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

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
