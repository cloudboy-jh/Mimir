package mimircli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	cliui "github.com/cloudboy-jh/mimir/internal/ui"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func connectionSummary(out io.Writer, url string) string {
	machine, _ := os.Hostname()
	if strings.TrimSpace(machine) == "" {
		machine = "registered"
	}
	credential, _ := tokenPath()
	manifest, _ := currentConnectionManifest(url)
	render := cliui.New(out)
	return render.Card("Ready",
		bentotui.Field{Label: "Worker", Value: strings.TrimRight(url, "/")},
		bentotui.Field{Label: "Machine", Value: machine},
		bentotui.Field{Label: "Credential", Value: credential},
		bentotui.Field{Label: "OpenAI", Value: manifest.OpenAIBaseURL},
		bentotui.Field{Label: "Anthropic", Value: manifest.AnthropicBaseURL},
		bentotui.Field{Label: "Memory", Value: "enabled"},
	)
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
