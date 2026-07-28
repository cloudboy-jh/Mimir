package deployment

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/jsonconfig"
)

func TestStripJSONC(t *testing.T) {
	input := []byte(`{
  // line comment
  "name": "mimir", // trailing line comment
  "url": "https://example.com/a//b", /* block comment */
  "escaped": "quote \" not a comment //",
  "list": [1, 2,],
  "nested": {"a": true,},
}`)
	var parsed map[string]any
	if err := json.Unmarshal(jsonconfig.StripJSONC(input), &parsed); err != nil {
		t.Fatalf("stripped JSONC did not parse: %v", err)
	}
	if parsed["name"] != "mimir" || parsed["url"] != "https://example.com/a//b" || parsed["escaped"] != "quote \" not a comment //" {
		t.Fatalf("values %#v", parsed)
	}
}

func TestWranglerParsers(t *testing.T) {
	if got := databaseID(`database_id = "123e4567-e89b-12d3-a456-426614174000"`); got != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("database ID %q", got)
	}
	if got := listedDatabaseID(`[{"uuid":"123e4567-e89b-12d3-a456-426614174000","name":"mimir"}]`, "mimir"); got == "" {
		t.Fatal("listed database not found")
	}
	if !listedSecret(`[{"name":"OPENROUTER_API_KEY"}]`, "OPENROUTER_API_KEY") {
		t.Fatal("secret not found")
	}
	if got := workerURL("Published mimir (https://mimir.example.workers.dev)"); got != "https://mimir.example.workers.dev" {
		t.Fatalf("worker URL %q", got)
	}
	if got, err := parseDeploymentURL(`[{"results":[{"value":"https://mimir.example.workers.dev/"}]}]`); err != nil || got != "https://mimir.example.workers.dev" {
		t.Fatalf("deployment URL %q, %v", got, err)
	}
	if got := sqlQuote("jack's machine"); got != "jack''s machine" {
		t.Fatalf("SQL quote %q", got)
	}
}

func TestUpdateVarsPreservesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrangler.jsonc")
	initial := "{\n  // worker name\n  \"name\": \"mimir\",\n  \"vars\": {\"KEEP\": \"1\"},\n}\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (Wrangler{}).UpdateVars(path, map[string]string{"DASHBOARD_ACCESS_AUD": "aud-1"}); err != nil {
		t.Fatal(err)
	}
	config, err := jsonconfig.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	vars := config["vars"].(map[string]any)
	if vars["KEEP"] != "1" || vars["DASHBOARD_ACCESS_AUD"] != "aud-1" {
		t.Fatalf("config %v", config)
	}
}

func TestUpdateConfigRequiresExpectedD1Binding(t *testing.T) {
	for _, test := range []struct {
		name      string
		databases any
	}{
		{name: "zero", databases: []any{}},
		{name: "multiple", databases: []any{map[string]any{"binding": "DB"}, map[string]any{"binding": "OTHER"}}},
		{name: "wrong", databases: []any{map[string]any{"binding": "OTHER"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "wrangler.jsonc")
			data, err := json.Marshal(map[string]any{"d1_databases": test.databases})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			err = (Wrangler{}).UpdateConfig(path, Config{DatabaseName: "mimir", DatabaseID: "resolved-id"})
			if err == nil || !strings.Contains(err.Error(), "exactly one D1 binding named DB") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestUpdateConfigWritesResolvedDatabaseID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrangler.jsonc")
	if err := os.WriteFile(path, []byte(`{"d1_databases":[{"binding":"DB","database_id":"wrong"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Wrangler{}).UpdateConfig(path, Config{WorkerName: "mimir", DatabaseName: "mimir", DatabaseID: "resolved-id", BucketName: "logs"}); err != nil {
		t.Fatal(err)
	}
	config, err := jsonconfig.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	database, err := expectedD1Binding(config)
	if err != nil || database["database_id"] != "resolved-id" {
		t.Fatalf("database=%v error=%v", database, err)
	}
}

func TestEmbeddedConfigRoutesIntegrationsThroughWorker(t *testing.T) {
	config, err := jsonconfig.Read(filepath.Join("..", "..", "worker", "wrangler.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	assets, _ := config["assets"].(map[string]any)
	routes, _ := assets["run_worker_first"].([]any)
	found := false
	for _, route := range routes {
		if route == "/integrations/*" {
			found = true
		}
	}
	if !found {
		t.Fatalf("run_worker_first = %v", routes)
	}
}

func TestReadIdentityUsesLocalWrangler(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bin, "wrangler")
	script := "#!/bin/sh\nprintf '%s' '{\"loggedIn\":true,\"authType\":\"OAuth Token\",\"email\":\"user@example.com\",\"accounts\":[{\"id\":\"abc\",\"name\":\"Example Account\"}]}'\n"
	if runtime.GOOS == "windows" {
		path += ".cmd"
		script = "@echo off\r\necho {\"loggedIn\":true,\"authType\":\"OAuth Token\",\"email\":\"user@example.com\",\"accounts\":[{\"id\":\"abc\",\"name\":\"Example Account\"}]}\r\n"
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	identity, err := NewService(nil).ReadIdentity(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Email != "user@example.com" || len(identity.Accounts) != 1 {
		t.Fatalf("identity %#v", identity)
	}
}

func TestLoginUsesWranglerResolvableDatabaseTarget(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	const databaseID = "123e4567-e89b-12d3-a456-426614174000"
	path := filepath.Join(bin, "wrangler")
	script := `#!/bin/sh
if [ "$1" = "whoami" ]; then
  printf '%s' '{"loggedIn":true,"accounts":[{"id":"account","name":"Account"}]}'
elif [ "$1" = "d1" ] && [ "$2" = "list" ]; then
  printf '%s' '[{"uuid":"` + databaseID + `","name":"mimir"}]'
elif [ "$1" = "d1" ] && [ "$2" = "execute" ]; then
  if [ "$3" != "mimir" ] && [ "$3" != "DB" ]; then
    printf 'Error: Could not find a D1 DB with the name or binding %s' "$3" >&2
    exit 1
  fi
  printf '%s' '[{"results":[{"value":"https://mimir.example.workers.dev"}]}]'
fi
`
	if runtime.GOOS == "windows" {
		path += ".cmd"
		script = `@echo off
if "%1"=="whoami" echo {"loggedIn":true,"accounts":[{"id":"account","name":"Account"}]}& exit /b 0
if "%1"=="d1" if "%2"=="list" echo [{"uuid":"` + databaseID + `","name":"mimir"}]& exit /b 0
if "%1"=="d1" if "%2"=="execute" if not "%3"=="mimir" if not "%3"=="DB" echo Error: Could not find a D1 DB with the name or binding %3 1>&2& exit /b 1
if "%1"=="d1" if "%2"=="execute" echo [{"results":[{"value":"https://mimir.example.workers.dev"}]}]& exit /b 0
`
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "wrangler.jsonc")
	if err := os.WriteFile(configPath, []byte(`{"d1_databases":[{"binding":"DB"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil)
	service.Installer = fakeInstaller{dir: dir}
	service.Hostname = func() (string, error) { return "test-machine", nil }
	if _, err := service.Login(context.Background(), DefaultOptions(), Hooks{}, ""); err != nil {
		t.Fatal(err)
	}
	config, err := jsonconfig.Read(configPath)
	if err != nil {
		t.Fatal(err)
	}
	database, err := expectedD1Binding(config)
	if err != nil || database["database_id"] != databaseID {
		t.Fatalf("database=%v error=%v", database, err)
	}
}

type fakeWrangler struct {
	calls []string
	vars  map[string]string
}

func (f *fakeWrangler) Run(_ context.Context, _ string, _ io.Reader, args ...string) (string, error) {
	f.calls = append(f.calls, args[0])
	switch args[0] {
	case "whoami":
		return `{"loggedIn":true,"accounts":[{"id":"account-1"}]}`, nil
	case "d1":
		return `[{"uuid":"123e4567-e89b-12d3-a456-426614174000","name":"mimir"}]`, nil
	case "deploy":
		if f.vars["DASHBOARD_ACCESS_AUD"] == "" || f.vars["DASHBOARD_ACCESS_TEAM_DOMAIN"] == "" {
			return "", io.ErrUnexpectedEOF
		}
	}
	return "", nil
}
func (f *fakeWrangler) Interactive(context.Context, string, Streams, ...string) error { return nil }
func (f *fakeWrangler) UpdateConfig(string, Config) error                             { return nil }
func (f *fakeWrangler) UpdateVars(_ string, vars map[string]string) error {
	f.calls = append(f.calls, "vars")
	f.vars = vars
	return nil
}

type fakeInstaller struct{ dir string }

func (f fakeInstaller) WorkerDir(string) (string, error)                       { return f.dir, nil }
func (f fakeInstaller) MaterializeWorker(string) (string, error)               { return f.dir, nil }
func (f fakeInstaller) EnsureWorkerDependencies(context.Context, string) error { return nil }
func (f fakeInstaller) BuildDashboard(context.Context, string) error           { return nil }

func TestObserveWranglerCopiesCommandOutputAndErrors(t *testing.T) {
	base := &fakeWrangler{vars: map[string]string{}}
	var observed strings.Builder
	wrapped := ObserveWrangler(base, &observed)
	if _, err := wrapped.Run(context.Background(), ".", nil, "whoami"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(observed.String(), `"loggedIn":true`) {
		t.Fatalf("success output not observed: %q", observed.String())
	}
	if _, err := wrapped.Run(context.Background(), ".", nil, "deploy"); err == nil {
		t.Fatal("expected fake deploy failure")
	}
	if !strings.Contains(observed.String(), "unexpected EOF") {
		t.Fatalf("failure not observed: %q", observed.String())
	}
}

func TestConfigureAccessPersistsManualVarsBeforeDeploy(t *testing.T) {
	wrangler := &fakeWrangler{}
	service := NewService(nil)
	service.Installer, service.Wrangler = fakeInstaller{dir: t.TempDir()}, wrangler
	opts := AccessOptions{Options: DefaultOptions(), URL: "https://mimir.example.workers.dev", Aud: "aud-manual", TeamDomain: "https://team.cloudflareaccess.com"}
	result, err := service.ConfigureAccess(context.Background(), opts, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Aud != "aud-manual" || len(wrangler.calls) < 4 || wrangler.calls[len(wrangler.calls)-2] != "vars" || wrangler.calls[len(wrangler.calls)-1] != "deploy" {
		t.Fatalf("result=%+v calls=%v vars=%v", result, wrangler.calls, wrangler.vars)
	}
}

func TestConfigureAccessWithoutEmailReturnsActionRequiredWithoutDeploy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any
		switch {
		case strings.HasSuffix(r.URL.Path, "/access/organizations"):
			result = map[string]any{"auth_domain": "team.cloudflareaccess.com"}
		case strings.HasSuffix(r.URL.Path, "/access/apps"):
			result = []AccessApp{{UID: "uid-1", Aud: "aud-1", Domain: "mimir.example/dashboard", SelfHostedDomains: DashboardAccessDomains("mimir.example")}}
		default:
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer server.Close()
	wrangler := &fakeWrangler{}
	service := NewService(server.Client())
	service.Access.Base = server.URL
	service.Installer, service.Wrangler = fakeInstaller{dir: t.TempDir()}, wrangler
	outcome, err := service.ConfigureAccess(context.Background(), AccessOptions{Options: DefaultOptions(), URL: "https://mimir.example", Token: "cf-token"}, Hooks{})
	if err != nil || outcome.State != "action-required" {
		t.Fatalf("outcome=%#v error=%v", outcome, err)
	}
	if slices.Contains(wrangler.calls, "deploy") {
		t.Fatalf("action-required Access was deployed: %v", wrangler.calls)
	}
}

type missingURLWrangler struct{}

func (missingURLWrangler) Run(_ context.Context, _ string, _ io.Reader, args ...string) (string, error) {
	switch args[0] {
	case "whoami":
		return `{"loggedIn":true,"authType":"OAuth Token","email":"user@example.com","accounts":[{"id":"account-1"}]}`, nil
	case "d1":
		if len(args) > 1 && args[1] == "list" {
			return `[{"uuid":"123e4567-e89b-12d3-a456-426614174000","name":"mimir"}]`, nil
		}
		return `[]`, nil
	default:
		return "", nil
	}
}

func (missingURLWrangler) Interactive(context.Context, string, Streams, ...string) error {
	return nil
}
func (missingURLWrangler) UpdateConfig(string, Config) error          { return nil }
func (missingURLWrangler) UpdateVars(string, map[string]string) error { return nil }

func TestLoginMissingURLRecommendsSupportedRecovery(t *testing.T) {
	service := NewService(nil)
	service.Installer = fakeInstaller{dir: t.TempDir()}
	service.Wrangler = missingURLWrangler{}
	_, err := service.Login(context.Background(), DefaultOptions(), Hooks{}, "")
	state, ok := err.(StateError)
	if !ok || state.State != "deployment_url_missing" {
		t.Fatalf("error = %#v", err)
	}
	if state.Message != "run mimir deploy, then rerun mimir login" {
		t.Fatalf("guidance = %q", state.Message)
	}
	if strings.Contains(state.Message, "--url") || strings.Contains(state.Message, "wrangler deploy") {
		t.Fatalf("guidance recommends unsupported recovery: %q", state.Message)
	}
}
