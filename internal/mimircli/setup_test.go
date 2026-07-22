package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mimirassets "github.com/cloudboy-jh/mimir"
)

func TestDatabaseID(t *testing.T) {
	got := databaseID("database_id = \"123e4567-e89b-12d3-a456-426614174000\"")
	if got != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("database ID %q", got)
	}
}

func TestListedDatabaseID(t *testing.T) {
	got := listedDatabaseID(`[{"uuid":"123e4567-e89b-12d3-a456-426614174000","name":"mimir"}]`, "mimir")
	if got != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("database ID %q", got)
	}
}

func TestListedSecret(t *testing.T) {
	if !listedSecret(`[{"name":"OPENROUTER_API_KEY","type":"secret_text"}]`, "OPENROUTER_API_KEY") {
		t.Fatal("secret not found")
	}
	if listedSecret(`[]`, "OPENROUTER_API_KEY") {
		t.Fatal("missing secret found")
	}
}

func TestWorkerURL(t *testing.T) {
	got := workerURL("Published mimir (https://mimir.example.workers.dev)")
	if got != "https://mimir.example.workers.dev" {
		t.Fatalf("worker URL %q", got)
	}
}

func TestMaterializeWorker(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "worker")
	if err := os.MkdirAll(filepath.Join(source, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "wrangler.jsonc"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "src", "index.ts"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets", "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mimir-readme.png", "mimir-favicon-32.png", "mimir-favicon-180.png"} {
		if err := os.WriteFile(filepath.Join(root, "assets", "images", name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(envMimirHome, t.TempDir())
	target, err := materializeWorker(source)
	if err != nil {
		t.Fatal(err)
	}
	if !pathExists(filepath.Join(target, "src", "index.ts")) {
		t.Fatal("worker source was not materialized")
	}
	for _, name := range []string{"mimir-readme.png", "mimir-favicon-32.png", "mimir-favicon-180.png"} {
		if !pathExists(filepath.Join(filepath.Dir(target), "assets", "images", name)) {
			t.Fatalf("shared dashboard image %s was not materialized", name)
		}
	}
	if err := updateWranglerVars(filepath.Join(target, "wrangler.jsonc"), map[string]string{"DASHBOARD_ACCESS_AUD": "aud-1", "DASHBOARD_ACCESS_TEAM_DOMAIN": "https://team.cloudflareaccess.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := materializeWorker(source); err != nil {
		t.Fatal(err)
	}
	vars := preservedWranglerVars(filepath.Join(target, "wrangler.jsonc"))
	if vars["DASHBOARD_ACCESS_AUD"] != "aud-1" || vars["DASHBOARD_ACCESS_TEAM_DOMAIN"] != "https://team.cloudflareaccess.com" {
		t.Fatalf("access vars were not preserved across materialization: %v", vars)
	}
}

func TestWorkerDependencyHashTracksPackageLock(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "package-lock.json")
	if err := os.WriteFile(lock, []byte(`{"lockfileVersion":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "web"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "web", "bun.lock"), []byte("lockfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := workerDependencyHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte(`{"lockfileVersion":3,"packages":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := workerDependencyHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("dependency hash did not change with package lock")
	}
}

func TestBuildDashboard(t *testing.T) {
	dir := t.TempDir()
	web := filepath.Join(dir, "web")
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(web, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	bun, script := filepath.Join(bin, "bun"), "#!/bin/sh\n[ \"$1 $2\" = \"run build\" ] || exit 2\nmkdir -p dist\ntouch dist/index.html\n"
	if runtime.GOOS == "windows" {
		bun += ".cmd"
		script = "@echo off\r\nif not \"%1 %2\"==\"run build\" exit /b 2\r\nif not exist dist mkdir dist\r\ntype nul > dist\\index.html\r\n"
	}
	if err := os.WriteFile(bun, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := buildDashboard(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if !pathExists(filepath.Join(web, "dist", "index.html")) {
		t.Fatal("dashboard was not built")
	}
}

func TestConnectExistingEndpointJSON(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MIMIR_TOKEN", "machine-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/whoami" || r.Header.Get("Authorization") != "Bearer machine-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"sessions":0,"log":0}`)
	}))
	defer server.Close()
	var output bytes.Buffer
	if err := setup(context.Background(), []string{"--url", server.URL, "--json"}, IO{In: bytes.NewBuffer(nil), Out: &output, Err: &output}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		State      string             `json:"state"`
		URL        string             `json:"url"`
		Connection connectionManifest `json:"connection"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != "connected" || result.URL != server.URL || result.Connection.OpenAIBaseURL != server.URL+"/v1" {
		t.Fatalf("result %#v", result)
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
	state, ok := err.(setupStateError)
	if !ok || state.State != "mimir_token_required" {
		t.Fatalf("error %#v", err)
	}
}

func TestParseDeploymentURL(t *testing.T) {
	got, err := parseDeploymentURL(`[{"results":[{"value":"https://mimir.example.workers.dev"}]}]`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://mimir.example.workers.dev" {
		t.Fatalf("URL %q", got)
	}
}

func TestSQLQuote(t *testing.T) {
	if got := sqlQuote("jack's machine"); got != "jack''s machine" {
		t.Fatalf("SQL quote %q", got)
	}
}

func TestReadCloudflareIdentity(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	wrangler := filepath.Join(bin, "wrangler")
	script := "#!/bin/sh\nprintf '%s' '{\"loggedIn\":true,\"authType\":\"OAuth Token\",\"email\":\"user@example.com\",\"accounts\":[{\"id\":\"abc\",\"name\":\"Example Account\"}]}'\n"
	if runtime.GOOS == "windows" {
		wrangler += ".cmd"
		script = "@echo off\r\necho {\"loggedIn\":true,\"authType\":\"OAuth Token\",\"email\":\"user@example.com\",\"accounts\":[{\"id\":\"abc\",\"name\":\"Example Account\"}]}\r\n"
	}
	if err := os.WriteFile(wrangler, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	identity, err := readCloudflareIdentity(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Email != "user@example.com" || len(identity.Accounts) != 1 || identity.Accounts[0].Name != "Example Account" {
		t.Fatalf("identity %#v", identity)
	}
}

func TestLoginSummaryShowsUserAndConnection(t *testing.T) {
	var identity cloudflareIdentity
	identity.LoggedIn = true
	identity.AuthType = "OAuth Token"
	identity.Email = "user@example.com"
	identity.Accounts = append(identity.Accounts, struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{ID: "abc", Name: "Example Account"})

	summary := loginSummary(identity, "https://mimir.example.workers.dev/", false)
	for _, want := range []string{"◆ Cloudflare", "Email:    user@example.com", "Account:  Example Account", "Auth:     OAuth Token", "◆ Connection", "Worker:   https://mimir.example.workers.dev", "Status:   ✓ connected"} {
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
	identity := cloudflareIdentity{LoggedIn: true, AuthType: "OAuth Token", Email: "user@example.com"}
	summary := loginSummary(identity, "https://mimir.example.workers.dev", true)
	for _, color := range []string{mimirMint, mimirGreen, mimirMutedGreen} {
		if !strings.Contains(summary, "38;2;"+color+"m") {
			t.Fatalf("summary missing palette color %s", color)
		}
	}
}

func TestCloudflareIdentityCacheRoundTrip(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	identity := cloudflareIdentity{LoggedIn: true, AuthType: "OAuth Token", Email: "user@example.com"}
	if err := saveCloudflareIdentity(identity); err != nil {
		t.Fatal(err)
	}
	got, err := loadCloudflareIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if got.LoggedIn != identity.LoggedIn || got.AuthType != identity.AuthType || got.Email != identity.Email {
		t.Fatalf("identity %#v", got)
	}
}

func TestConnectionManifestContainsNoCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envMimirHome, home)
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
	if len(manifest.MCPCommand) != 2 || !filepath.IsAbs(manifest.MCPCommand[0]) || manifest.MCPCommand[1] != "serve" {
		t.Fatalf("MCP command %#v", manifest.MCPCommand)
	}
}

func TestPointerRoundTrip(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	want := Pointer{URL: "https://mimir.example.workers.dev", Token: "secret"}
	if err := savePointer(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadPointer()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSetupProgressStopIsIdempotent(t *testing.T) {
	var output bytes.Buffer
	progress := &setupProgress{out: &output, enabled: true, phases: []string{"testing"}}
	progress.Resume()
	progress.Stop()
	first := output.String()
	progress.Stop()
	if output.String() != first {
		t.Fatal("second stop wrote additional output")
	}
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
