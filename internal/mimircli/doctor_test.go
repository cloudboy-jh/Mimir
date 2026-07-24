package mimircli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRunDoctorHealthyHermesIntegration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HERMES_HOME", filepath.Join(home, ".hermes"))
	t.Setenv("OPENROUTER_API_KEY", "test-openrouter-key")
	t.Setenv(envMimirHome, t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whoami":
			if r.Header.Get("Authorization") != "Bearer machine-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"service":"mimir","api_version":1,"capabilities":["hermes_authorization","session_events","session_lifecycle"],"sessions":0,"log":0}`)
		case "/integrations/hermes/authorize":
			if r.Header.Get("Authorization") != "Bearer machine-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"authorized":true}`)
		case "/v1/hermes/models", "/v1/hermes/key", "/v1/hermes/credits":
			if r.Header.Get("Authorization") != "Bearer test-openrouter-key" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"data":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := savePointer(Pointer{URL: server.URL, Token: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	artifacts, err := syncManagedArtifacts(true, "install")
	if err != nil {
		t.Fatal(err)
	}
	recordCurrentExecutableInReceipt(t)
	oldRunHermesPluginCommand := runHermesPluginCommand
	runHermesPluginCommand = func(context.Context, string, ...string) error { return nil }
	oldListHermesPlugins := listHermesPlugins
	listHermesPlugins = func(context.Context, string) (string, error) { return "enabled user 1.0.0 mimir\n", nil }
	oldFindOpenCode := findOpenCode
	findOpenCode = func() (string, error) { return "opencode-test", nil }
	t.Cleanup(func() {
		runHermesPluginCommand = oldRunHermesPluginCommand
		listHermesPlugins = oldListHermesPlugins
		findOpenCode = oldFindOpenCode
	})
	if _, err := installCurrentHarnessIntegrations(context.Background(), artifacts); err != nil {
		t.Fatal(err)
	}
	manifest, err := currentConnectionManifest(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = manifest
	report := runDoctor(context.Background())
	if !report.OK {
		t.Fatalf("doctor report: %#v", report)
	}
}

func TestRunDoctorReportsMissingConnection(t *testing.T) {
	paths := isolatedInstallation(t, false)
	report := runDoctor(context.Background())
	if report.OK || len(report.Checks) < 2 || report.Checks[len(report.Checks)-1].Name != "connection" || report.Checks[0].Name == "connection" {
		t.Fatalf("doctor report: %#v", report)
	}
	for _, path := range []string{paths.Receipt, paths.Log} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("doctor created %s", path)
		}
	}
}

func TestValidateWorkerIdentityRejectsStaleWorker(t *testing.T) {
	if err := validateWorkerIdentity([]byte(`{"sessions":0,"log":0}`)); err == nil {
		t.Fatal("legacy Worker was accepted")
	}
	if err := validateWorkerIdentity([]byte(`{"service":"mimir","api_version":1,"capabilities":["session_events"]}`)); err == nil {
		t.Fatal("Worker missing required capabilities was accepted")
	}
}
