package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mimirassets "github.com/cloudboy-jh/mimir"
	"github.com/cloudboy-jh/mimir/internal/deployment"
	"github.com/cloudboy-jh/mimir/internal/harness"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
	cliui "github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func TestConnectExistingEndpointJSON(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HERMES_HOME", t.TempDir())
	t.Setenv("MIMIR_TOKEN", "machine-token")
	t.Setenv("OPENROUTER_API_KEY", "hermes-openrouter-key")
	useStableTestExecutable(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer machine-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/whoami":
			fmt.Fprint(w, `{"sessions":0,"log":0}`)
		case "/integrations/hermes/authorize":
			fmt.Fprint(w, `{"authorized":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var output bytes.Buffer
	if err := setup(context.Background(), []string{"--url", server.URL, "--json"}, IO{In: bytes.NewBuffer(nil), Out: &output, Err: &output}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		State        string                     `json:"state"`
		URL          string                     `json:"url"`
		Connection   harness.ConnectionManifest `json:"connection"`
		Artifacts    installpkg.ArtifactReport  `json:"artifacts"`
		Integrations harness.IntegrationReport  `json:"integrations"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != "connected" || result.URL != server.URL || result.Connection.OpenAIBaseURL != server.URL+"/v1" {
		t.Fatalf("result %#v", result)
	}
	if result.Integrations.Hermes.State != "skipped" || !strings.Contains(result.Integrations.Hermes.Detail, "no managed installation receipt") {
		t.Fatalf("Hermes integration %#v", result.Integrations.Hermes)
	}
	if result.Integrations.OpenCode.State != "skipped" {
		t.Fatalf("OpenCode integration %#v", result.Integrations.OpenCode)
	}
	if result.Artifacts.Operation != "setup" || len(result.Artifacts.Artifacts) == 0 {
		t.Fatalf("artifact refresh %#v", result.Artifacts)
	}
	receipt, err := installpkg.LoadReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Artifacts) != 0 {
		t.Fatalf("setup enrolled unmanaged artifacts: %#v", receipt.Artifacts)
	}
	paths, err := installpkg.Paths()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.Log} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("setup created installation lifecycle file %s", path)
		}
	}
	pointer, err := loadPointer()
	if err != nil {
		t.Fatal(err)
	}
	if pointer.Token != "machine-token" {
		t.Fatal("machine token was not persisted")
	}
	configPath, _ := pointerPath()
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(config, []byte("machine-token")) {
		t.Fatal("token leaked into pointer config")
	}
}

func TestConnectExistingEndpointJSONNeedsToken(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	t.Setenv("MIMIR_TOKEN", "")
	err := setup(context.Background(), []string{"--url", "https://mimir.example.workers.dev", "--json"}, IO{In: bytes.NewBuffer(nil), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	state, ok := err.(deployment.StateError)
	if !ok || state.State != "mimir_token_required" {
		t.Fatalf("error %#v", err)
	}
}

type setupInstaller struct{}

func (setupInstaller) WorkerDir(dir string) (string, error)                   { return dir, nil }
func (setupInstaller) MaterializeWorker(dir string) (string, error)           { return dir, nil }
func (setupInstaller) EnsureWorkerDependencies(context.Context, string) error { return nil }
func (setupInstaller) EnsureDashboardDependencies(context.Context, string) error {
	return nil
}
func (setupInstaller) BuildDashboard(context.Context, string) error { return nil }

type setupWrangler struct{}

func (setupWrangler) Run(_ context.Context, _ string, _ io.Reader, args ...string) (string, error) {
	switch strings.Join(args, " ") {
	case "whoami":
		return "authenticated", nil
	case "whoami --json":
		return `{"loggedIn":true,"accounts":[{"id":"account","name":"Account"}]}`, nil
	case "secret list --format json":
		return `[{"name":"OPENROUTER_API_KEY"}]`, nil
	case "deploy":
		return "https://mimir.example.workers.dev", nil
	default:
		return "", nil
	}
}
func (setupWrangler) Interactive(context.Context, string, deployment.Streams, ...string) error {
	return nil
}
func (setupWrangler) UpdateConfig(string, deployment.Config) error { return nil }
func (setupWrangler) UpdateVars(string, map[string]string) error   { return nil }

type setupRoundTripFunc func(*http.Request) (*http.Response, error)

func (f setupRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestProvisionJSONSuccessHasNoProgressPanic(t *testing.T) {
	isolatedInstallation(t, false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/whoami" {
			http.NotFound(w, request)
			return
		}
		fmt.Fprint(w, `{"sessions":0}`)
	}))
	defer server.Close()
	serverURL := server.URL
	oldHTTPClient := httpClient
	httpClient = &http.Client{Transport: setupRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		request = request.Clone(request.Context())
		request.URL.Scheme = "http"
		request.URL.Host = strings.TrimPrefix(serverURL, "http://")
		return http.DefaultTransport.RoundTrip(request)
	})}
	t.Cleanup(func() { httpClient = oldHTTPClient })
	oldFactory := newDeploymentService
	newDeploymentService = func(deployment.HTTPDoer) *deployment.Service {
		service := deployment.NewService(nil)
		service.Installer = setupInstaller{}
		service.Wrangler = setupWrangler{}
		service.Hostname = func() (string, error) { return "test-machine", nil }
		return service
	}
	t.Cleanup(func() { newDeploymentService = oldFactory })
	var output bytes.Buffer
	opts := setupOptions{JSON: true, WorkerDir: t.TempDir(), WorkerName: "mimir", DatabaseName: "mimir", DatabaseID: "database-uuid", BucketName: "mimir-logs"}
	if err := provision(context.Background(), opts, IO{Out: &output, Err: &output}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != "ready" {
		t.Fatalf("result = %s", output.String())
	}
}

func TestLoginSummaryShowsUserAndConnection(t *testing.T) {
	var identity deployment.Identity
	identity.LoggedIn = true
	identity.AuthType = "OAuth Token"
	identity.Email = "user@example.com"
	identity.Accounts = append(identity.Accounts, struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{ID: "abc", Name: "Example Account"})

	summary := loginSummary(identity, "https://mimir.example.workers.dev/", false)
	for _, want := range []string{"◆ Cloudflare", "Email:", "user@example.com", "Account:", "Example Account", "Auth:", "OAuth Token", "◆ Connection", "Worker:", "https://mimir.example.workers.dev", "Status:", "[✓] connected"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "┌") || strings.Contains(summary, "Mimir Login") {
		t.Fatalf("summary contains redundant title box:\n%s", summary)
	}
	if strings.Contains(summary, "\x1b[") {
		t.Fatalf("plain summary contains ANSI escapes:\n%s", summary)
	}
}

func TestLoginSummaryUsesMimirPalette(t *testing.T) {
	identity := deployment.Identity{LoggedIn: true, AuthType: "OAuth Token", Email: "user@example.com"}
	summary := loginSummary(identity, "https://mimir.example.workers.dev", true)
	for _, color := range []string{
		fmt.Sprintf("%d;%d;%d", bentotui.Mimir.Accent.R, bentotui.Mimir.Accent.G, bentotui.Mimir.Accent.B),
		fmt.Sprintf("%d;%d;%d", bentotui.Mimir.Success.R, bentotui.Mimir.Success.G, bentotui.Mimir.Success.B),
		fmt.Sprintf("%d;%d;%d", bentotui.Mimir.Muted.R, bentotui.Mimir.Muted.G, bentotui.Mimir.Muted.B),
	} {
		if !strings.Contains(summary, "38;2;"+color+"m") {
			t.Fatalf("summary missing palette color %s", color)
		}
	}
}

func TestLoginSummaryFitsNarrowTerminal(t *testing.T) {
	identity := deployment.Identity{LoggedIn: true, AuthType: "OAuth Token", Email: "user-with-a-long-address@example.com"}
	identity.Accounts = append(identity.Accounts, struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{Name: "A Cloudflare account with an unusually long display name"})
	render := cliui.Renderer{Width: 40, Theme: bentotui.Mimir}
	summary := loginSummaryWithRenderer(identity, "https://mimir.example.workers.dev/with/a/long/path", render)
	if !bentotui.FitsWidth(summary, 40) {
		t.Fatalf("summary exceeds terminal width:\n%s", summary)
	}
}

func TestConnectionManifestContainsNoCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envMimirHome, home)
	useStableTestExecutable(t)
	manifest, err := currentConnectionManifest("https://mimir.example.workers.dev")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.OpenAIBaseURL != "https://mimir.example.workers.dev/v1" || manifest.AnthropicBaseURL != "https://mimir.example.workers.dev" {
		t.Fatalf("manifest %#v", manifest)
	}
	if manifest.CredentialFile != filepath.Join(home, "token") {
		t.Fatalf("credential file %q", manifest.CredentialFile)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(data), []byte("mcp")) {
		t.Fatalf("connection manifest retained removed local-server fields: %s", data)
	}
}

func useStableTestExecutable(t *testing.T) {
	t.Helper()
	original := executablePath
	source, err := original()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mimir-test")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeTemp := filepath.Join(filepath.Dir(path), "runtime-temp")
	if err := os.MkdirAll(runtimeTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMP", runtimeTemp)
	t.Setenv("TEMP", runtimeTemp)
	t.Setenv("TMPDIR", runtimeTemp)
	executablePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { executablePath = original })
}

func TestSetupProgressStopIsIdempotent(t *testing.T) {
	var output bytes.Buffer
	progress := startOperationProgress(context.Background(), IO{Out: &output}, "Mimir setup", []string{"testing"}, func() {})
	progress.Stop()
	first := output.String()
	progress.Stop()
	if output.String() != first {
		t.Fatal("second stop wrote additional output")
	}
}

func TestNilSetupProgressLifecycleIsSafe(t *testing.T) {
	var progress *setupProgress
	progress.Pause()
	progress.Resume()
	progress.Complete("complete")
	progress.Stop()
}

func TestWriteITermImage(t *testing.T) {
	var output bytes.Buffer
	writeITermImage(&output, []byte("png"), 64)
	if !strings.Contains(output.String(), "File=inline=1;width=64") {
		t.Fatalf("unexpected iTerm image sequence: %q", output.String())
	}
}

func TestWriteKittyImageChunks(t *testing.T) {
	var output bytes.Buffer
	writeKittyImage(&output, bytes.Repeat([]byte("x"), 5000), 64)
	if !strings.Contains(output.String(), "a=T,f=100,t=d,c=64") || !strings.Contains(output.String(), "m=0;") {
		t.Fatalf("unexpected Kitty image sequence")
	}
}

func TestWriteANSIImage(t *testing.T) {
	var output bytes.Buffer
	if err := writeANSIImage(&output, mimirassets.LogoPNG, 32); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "\x1b[38;2;") {
		t.Fatal("ANSI image has no true-color pixels")
	}
}

func TestWarpUsesITermImageProtocol(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("LC_TERMINAL", "")
	if got := terminalImageProtocol(); got != "iterm" {
		t.Fatalf("terminalImageProtocol() = %q, want iterm", got)
	}
}
