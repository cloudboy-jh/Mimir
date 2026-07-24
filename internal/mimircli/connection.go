package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func connectionSummary(url string) string {
	machine, _ := os.Hostname()
	if strings.TrimSpace(machine) == "" {
		machine = "registered"
	}
	credential, _ := tokenPath()
	manifest, _ := currentConnectionManifest(url)
	return fmt.Sprintf("Mimir connected\n\n  Worker      %s\n  Machine     %s\n  Credential  %s\n  OpenAI      %s\n  Anthropic   %s\n  MCP         mimir serve\n  Memory      enabled\n  Status      ready for harness connection", strings.TrimRight(url, "/"), machine, credential, manifest.OpenAIBaseURL, manifest.AnthropicBaseURL)
}

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
	executable, err := manifestExecutable()
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
		OptionalHeaders:   []string{"x-mimir-session", "x-mimir-repo", "x-mimir-harness", "x-mimir-git-ref", "x-mimir-request-kind"},
	}, nil
}

func manifestExecutable() (string, error) {
	receipt, err := loadInstallReceipt()
	if err != nil {
		return "", err
	}
	if receipt.CLI.Path != "" && receipt.CLI.Hash != "" {
		path, err := filepath.Abs(receipt.CLI.Path)
		if err != nil {
			return "", err
		}
		if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
			return "", err
		} else if symlink {
			return "", fmt.Errorf("receipt-recorded Mimir executable is symlinked: %s", path)
		}
		info, statErr := os.Lstat(path)
		if statErr == nil && info.Mode().IsRegular() {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return "", readErr
			}
			if hashBytes(data) == receipt.CLI.Hash {
				return path, nil
			}
		}
		return "", fmt.Errorf("receipt-recorded Mimir executable is missing or changed; run mimir install")
	}
	executable, err := executablePath()
	if err != nil {
		return "", err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", err
	}
	if temporaryExecutable(executable) {
		return "", fmt.Errorf("refusing to publish a temporary go-run executable; run mimir install first")
	}
	info, err := os.Lstat(executable)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("current Mimir executable is unavailable: %s", executable)
	}
	return executable, nil
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
