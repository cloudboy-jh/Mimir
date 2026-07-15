package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

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
