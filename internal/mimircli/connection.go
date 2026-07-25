package mimircli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
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

func currentConnectionManifest(url string) (harness.ConnectionManifest, error) {
	configureInstall()
	return lifecycleService().Manifest(url)
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

const envMimirHome = mimirapi.EnvHome

func pointerPath() (string, error) {
	return mimirapi.ConfigPath()
}

func tokenPath() (string, error) {
	return mimirapi.TokenPath()
}

func loadPointer() (mimirapi.Pointer, error) {
	return mimirapi.LoadPointer()
}

func savePointer(p mimirapi.Pointer) error {
	return mimirapi.SavePointer(p)
}
