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
	"runtime"
	"strings"
)

func runWrangler(ctx context.Context, dir string, stdin io.Reader, args ...string) (string, error) {
	if local := localWranglerPath(dir); local != "" {
		return runCommand(ctx, dir, stdin, local, args...)
	}
	return runCommand(ctx, dir, stdin, "npx", append([]string{"wrangler"}, args...)...)
}

func localWranglerPath(dir string) string {
	local := filepath.Join(dir, "node_modules", ".bin", "wrangler")
	if runtime.GOOS == "windows" && pathExists(local+".cmd") {
		return local + ".cmd"
	}
	if pathExists(local) {
		return local
	}
	return ""
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
	name, commandArgs := localWranglerPath(dir), args
	if name == "" {
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
	if err := json.Unmarshal(stripJSONC(data), &config); err != nil {
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

// updateWranglerVars merges Worker environment variables into wrangler.jsonc.
func updateWranglerVars(path string, vars map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var config map[string]any
	if err := json.Unmarshal(stripJSONC(data), &config); err != nil {
		return err
	}
	existing, _ := config["vars"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	for key, value := range vars {
		existing[key] = value
	}
	config["vars"] = existing
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

// stripJSONC converts JSONC to strict JSON by removing comments outside
// strings and dropping trailing commas, so hand-edited wrangler.jsonc files
// parse with encoding/json.
func stripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case inString:
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++
		case c == ',':
			// Drop the comma when the next significant byte closes an
			// object or array (trailing comma).
			j := i + 1
			for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\r' || data[j] == '\n') {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
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
