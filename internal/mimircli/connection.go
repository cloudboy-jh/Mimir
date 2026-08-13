package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness"
	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	cliui "github.com/cloudboy-jh/mimir/internal/ui/appframe"
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

func associateMachineIdentity(ctx context.Context, client mimirapi.Client) error {
	whoami, err := client.WhoAmI(ctx)
	if err != nil {
		return err
	}
	if !whoami.HasCapability("machine_identity_association") {
		return nil
	}
	installationID, err := install.EnsureInstallationID()
	if err != nil {
		return fmt.Errorf("creating machine identity: %w", err)
	}
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		name = "machine"
	}
	name = strings.TrimSpace(name)
	if len(name) > 200 {
		name = name[:200]
	}
	return client.AssociateMachine(ctx, mimirapi.MachineAssociation{
		Version: 1, InstallationID: installationID, Name: name,
		Platform: strings.ToLower(runtime.GOOS), Arch: strings.ToLower(runtime.GOARCH),
	})
}
