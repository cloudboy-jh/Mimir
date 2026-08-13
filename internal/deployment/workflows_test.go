package deployment

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
)

type workflowInstaller struct{ calls []string }

func (i *workflowInstaller) WorkerDir(dir string) (string, error) {
	i.calls = append(i.calls, "worker-dir")
	return dir, nil
}
func (i *workflowInstaller) MaterializeWorker(dir string) (string, error) {
	i.calls = append(i.calls, "materialize")
	return dir, nil
}
func (i *workflowInstaller) EnsureWorkerDependencies(context.Context, string) error {
	i.calls = append(i.calls, "worker-dependencies")
	return nil
}
func (i *workflowInstaller) EnsureDashboardDependencies(context.Context, string) error {
	i.calls = append(i.calls, "dashboard-dependencies")
	return nil
}
func (i *workflowInstaller) BuildDashboard(context.Context, string) error {
	i.calls = append(i.calls, "build-dashboard")
	return nil
}

type workflowWrangler struct {
	calls         [][]string
	accountIDs    []string
	config        Config
	hideDeployURL bool
	databaseList  string
}

func (w *workflowWrangler) Run(ctx context.Context, _ string, _ io.Reader, args ...string) (string, error) {
	w.calls = append(w.calls, slices.Clone(args))
	w.accountIDs = append(w.accountIDs, wranglerAccountID(ctx))
	switch {
	case slices.Equal(args, []string{"whoami", "--json"}):
		return `{"loggedIn":true,"accounts":[{"id":"account","name":"Account"}]}`, nil
	case slices.Equal(args, []string{"whoami"}):
		return "authenticated", nil
	case slices.Equal(args, []string{"secret", "list", "--format", "json"}):
		return `[{"name":"OPENROUTER_API_KEY"}]`, nil
	case slices.Equal(args, []string{"deploy"}):
		if w.hideDeployURL {
			return "deployed", nil
		}
		return "Deployed to https://mimir.example.workers.dev", nil
	case slices.Equal(args, []string{"d1", "list", "--json"}):
		if w.databaseList != "" {
			return w.databaseList, nil
		}
		return `[{"uuid":"database-uuid","name":"custom-db"},{"uuid":"database-uuid","name":"mimir"}]`, nil
	case len(args) > 2 && args[0] == "d1" && args[1] == "execute" && strings.Contains(strings.Join(args, " "), "SELECT value FROM config"):
		return `[{"results":[{"value":"https://mimir.example.workers.dev"}]}]`, nil
	default:
		return "", nil
	}
}

func (w *workflowWrangler) Interactive(context.Context, string, Streams, ...string) error { return nil }
func (w *workflowWrangler) UpdateConfig(_ string, config Config) error {
	w.config = config
	return nil
}
func (w *workflowWrangler) UpdateVars(string, map[string]string) error { return nil }

func TestProvisionUsesDatabaseNameForD1CommandsAndIDForConfig(t *testing.T) {
	wrangler := &workflowWrangler{}
	service := testWorkflowService(wrangler)
	opts := DefaultOptions()
	opts.WorkerDir = t.TempDir()
	opts.DatabaseID = "database-uuid"
	if _, err := service.Provision(context.Background(), opts, Hooks{}); err != nil {
		t.Fatal(err)
	}
	assertD1Targets(t, wrangler.calls, opts.DatabaseName)
	if wrangler.config.DatabaseID != opts.DatabaseID {
		t.Fatalf("configured database ID = %q", wrangler.config.DatabaseID)
	}
}

func TestDeployAndLoginUseDatabaseNameForD1CommandsAndIDForConfig(t *testing.T) {
	for _, operation := range []string{"deploy", "login"} {
		t.Run(operation, func(t *testing.T) {
			wrangler := &workflowWrangler{}
			service := testWorkflowService(wrangler)
			opts := DefaultOptions()
			opts.WorkerDir = t.TempDir()
			opts.DatabaseID = "database-uuid"
			var err error
			if operation == "deploy" {
				_, err = service.Deploy(context.Background(), opts, Hooks{}, "")
			} else {
				_, err = service.Login(context.Background(), opts, Hooks{}, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			assertD1Targets(t, wrangler.calls, opts.DatabaseName)
			if wrangler.config.DatabaseID != opts.DatabaseID {
				t.Fatalf("configured database ID = %q", wrangler.config.DatabaseID)
			}
		})
	}
}

func TestPreparationUsesPrebuiltDashboardForPackagedDeployments(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		mode     preparation
		want     []string
	}{
		{name: "login", mode: prepareLogin, want: []string{"worker-dir", "materialize"}},
		{name: "packaged deployment", mode: prepareDeployment, want: []string{"worker-dir", "materialize", "worker-dependencies"}},
		{name: "development deployment", explicit: "development-worker", mode: prepareDeployment, want: []string{"worker-dir", "materialize", "worker-dependencies", "dashboard-dependencies", "build-dashboard"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			installer := &workflowInstaller{}
			service := NewService(nil)
			service.Installer = installer
			if _, err := service.prepare(context.Background(), test.explicit, test.mode); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(installer.calls, test.want) {
				t.Fatalf("preparation calls = %v, want %v", installer.calls, test.want)
			}
		})
	}
}

func TestProvisionMissingWorkerURLRecommendsSupportedLogin(t *testing.T) {
	wrangler := &workflowWrangler{hideDeployURL: true}
	service := testWorkflowService(wrangler)
	opts := DefaultOptions()
	opts.DatabaseID = "database-uuid"
	_, err := service.Provision(context.Background(), opts, Hooks{})
	if err == nil || !strings.Contains(err.Error(), "mimir login --url <url>") || strings.Contains(err.Error(), "--token") {
		t.Fatalf("error = %v", err)
	}
}

func testWorkflowService(wrangler *workflowWrangler) *Service {
	service := NewService(nil)
	service.Installer = &workflowInstaller{}
	service.Wrangler = wrangler
	service.Hostname = func() (string, error) { return "test-machine", nil }
	service.EnsureInstallationID = func() (string, error) { return "installation-123", nil }
	service.GOOS, service.GOARCH = "darwin", "arm64"
	service.LoadState = func() (DeploymentState, error) { return DeploymentState{}, nil }
	service.SaveState = func(DeploymentState) error { return nil }
	return service
}

func TestProvisionAndLoginRegisterStableMachineIdentity(t *testing.T) {
	for _, operation := range []string{"provision", "login"} {
		t.Run(operation, func(t *testing.T) {
			wrangler := &workflowWrangler{}
			service := testWorkflowService(wrangler)
			ensured := 0
			service.EnsureInstallationID = func() (string, error) {
				ensured++
				return "install'id", nil
			}
			service.Hostname = func() (string, error) { return " host'name ", nil }
			opts := DefaultOptions()
			opts.WorkerDir = t.TempDir()
			opts.DatabaseID = "database-uuid"
			var err error
			if operation == "provision" {
				_, err = service.Provision(context.Background(), opts, Hooks{})
			} else {
				_, err = service.Login(context.Background(), opts, Hooks{}, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			if ensured != 1 {
				t.Fatalf("EnsureInstallationID calls = %d", ensured)
			}
			sql := machineRegistrationSQL(t, wrangler.calls)
			for _, want := range []string{"INSERT INTO machines", "install''id", "host''name", "'darwin'", "'arm64'", "created_at", "updated_at", "ON CONFLICT(installation_id) DO UPDATE SET platform = excluded.platform, arch = excluded.arch, updated_at = excluded.updated_at WHERE machines.revoked_at IS NULL", "INSERT INTO access_tokens", "WHERE EXISTS (SELECT 1 FROM machines WHERE installation_id = 'install''id' AND revoked_at IS NULL)", "installation_id = excluded.installation_id", "WHERE access_tokens.installation_id IS NULL OR access_tokens.installation_id = excluded.installation_id"} {
				if !strings.Contains(sql, want) {
					t.Fatalf("registration SQL missing %q: %s", want, sql)
				}
			}
			if strings.Contains(sql, "revoked_at = NULL") {
				t.Fatalf("registration SQL clears revocation: %s", sql)
			}
			if strings.Contains(sql, "name = excluded.name") {
				t.Fatalf("registration SQL overwrites machine name: %s", sql)
			}
		})
	}
}

func machineRegistrationSQL(t *testing.T, calls [][]string) string {
	t.Helper()
	for _, args := range calls {
		if len(args) >= 6 && args[0] == "d1" && args[1] == "execute" && args[4] == "--command" && strings.Contains(args[5], "INSERT INTO machines") {
			return args[5]
		}
	}
	t.Fatal("machine registration command not found")
	return ""
}

func TestDeployReusesVerifiedDeploymentState(t *testing.T) {
	wrangler := &workflowWrangler{}
	service := testWorkflowService(wrangler)
	service.LoadState = func() (DeploymentState, error) {
		return DeploymentState{Schema: deploymentStateSchema, AccountID: "account", WorkerName: "custom-worker", DatabaseName: "custom-db", DatabaseID: "database-uuid", BucketName: "custom-logs"}, nil
	}
	var saved DeploymentState
	service.SaveState = func(state DeploymentState) error { saved = state; return nil }
	opts := Options{WorkerDir: t.TempDir()}
	if _, err := service.Deploy(context.Background(), opts, Hooks{}, ""); err != nil {
		t.Fatal(err)
	}
	if wrangler.config.WorkerName != "custom-worker" || wrangler.config.DatabaseName != "custom-db" || wrangler.config.DatabaseID != "database-uuid" {
		t.Fatalf("config = %#v", wrangler.config)
	}
	if saved.DatabaseID != "database-uuid" || saved.URL != "https://mimir.example.workers.dev" {
		t.Fatalf("saved = %#v", saved)
	}
}

func TestSelectedAccountIsPinnedBeforeResourceCommands(t *testing.T) {
	for _, operation := range []string{"provision", "deploy", "login", "access"} {
		t.Run(operation, func(t *testing.T) {
			t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
			wrangler := &workflowWrangler{}
			service := testWorkflowService(wrangler)
			service.LoadState = func() (DeploymentState, error) {
				return DeploymentState{AccountID: "account", DatabaseName: "mimir", DatabaseID: "database-uuid", WorkerName: "mimir", BucketName: "mimir-logs"}, nil
			}
			opts := Options{WorkerDir: t.TempDir(), AccountID: "account"}
			var err error
			switch operation {
			case "provision":
				_, err = service.Provision(context.Background(), opts, Hooks{})
			case "deploy":
				_, err = service.Deploy(context.Background(), opts, Hooks{}, "")
			case "login":
				_, err = service.Login(context.Background(), opts, Hooks{}, "")
			case "access":
				_, err = service.ConfigureAccess(context.Background(), AccessOptions{Options: opts, Aud: "aud", TeamDomain: "https://team.cloudflareaccess.com"}, Hooks{})
			}
			if err != nil {
				t.Fatal(err)
			}
			for index, args := range wrangler.calls {
				if len(args) == 0 || args[0] == "whoami" {
					continue
				}
				if wrangler.accountIDs[index] != "account" {
					t.Fatalf("command %v used account %q", args, wrangler.accountIDs[index])
				}
			}
		})
	}
}

func TestDeployExplicitDatabaseNameDoesNotReuseDifferentCachedID(t *testing.T) {
	wrangler := &workflowWrangler{}
	service := testWorkflowService(wrangler)
	service.LoadState = func() (DeploymentState, error) {
		return DeploymentState{Schema: deploymentStateSchema, AccountID: "account", DatabaseName: "old-db", DatabaseID: "old-id"}, nil
	}
	opts := Options{WorkerDir: t.TempDir(), DatabaseName: "new-db"}
	_, err := service.Deploy(context.Background(), opts, Hooks{}, "")
	if err == nil {
		t.Fatal("missing explicit database was accepted from unrelated cached state")
	}
}

func TestDeployIgnoresDeploymentStateFromAnotherAccount(t *testing.T) {
	wrangler := &workflowWrangler{}
	service := testWorkflowService(wrangler)
	service.LoadState = func() (DeploymentState, error) {
		return DeploymentState{Schema: deploymentStateSchema, AccountID: "other-account", WorkerName: "other-worker", DatabaseName: "other-db", DatabaseID: "other-id", BucketName: "other-bucket"}, nil
	}
	if _, err := service.Deploy(context.Background(), Options{WorkerDir: t.TempDir()}, Hooks{}, ""); err != nil {
		t.Fatal(err)
	}
	if wrangler.config.WorkerName != "mimir" || wrangler.config.DatabaseName != "mimir" || wrangler.config.DatabaseID != "database-uuid" || wrangler.config.BucketName != "mimir-logs" {
		t.Fatalf("cross-account state reused: %#v", wrangler.config)
	}
}

func TestDeployRevalidatesStaleCachedDatabaseID(t *testing.T) {
	wrangler := &workflowWrangler{databaseList: `[{"uuid":"replacement-id","name":"custom-db"}]`}
	service := testWorkflowService(wrangler)
	service.LoadState = func() (DeploymentState, error) {
		return DeploymentState{Schema: deploymentStateSchema, AccountID: "account", WorkerName: "custom-worker", DatabaseName: "custom-db", DatabaseID: "stale-id", BucketName: "custom-logs"}, nil
	}
	var saved DeploymentState
	service.SaveState = func(state DeploymentState) error { saved = state; return nil }
	if _, err := service.Deploy(context.Background(), Options{WorkerDir: t.TempDir()}, Hooks{}, ""); err != nil {
		t.Fatal(err)
	}
	if wrangler.config.DatabaseID != "replacement-id" || saved.DatabaseID != "replacement-id" || saved.AccountID != "account" {
		t.Fatalf("config=%#v saved=%#v", wrangler.config, saved)
	}
}

func assertD1Targets(t *testing.T, calls [][]string, databaseName string) {
	t.Helper()
	found := 0
	for _, args := range calls {
		if len(args) < 4 || args[0] != "d1" || (args[1] != "execute" && args[1] != "migrations") {
			continue
		}
		found++
		target := args[2]
		if args[1] == "migrations" {
			target = args[3]
		}
		if target != databaseName {
			t.Fatalf("D1 command used %q instead of %q: %s", target, databaseName, strings.Join(args, " "))
		}
	}
	if found == 0 {
		t.Fatal("no D1 operations recorded")
	}
}
