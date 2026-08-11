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
	config        Config
	hideDeployURL bool
}

func (w *workflowWrangler) Run(_ context.Context, _ string, _ io.Reader, args ...string) (string, error) {
	w.calls = append(w.calls, slices.Clone(args))
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
	return service
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
