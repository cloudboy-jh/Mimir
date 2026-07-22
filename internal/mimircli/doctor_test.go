package mimircli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRunDoctorHealthyOpenCodeIntegration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
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
			fmt.Fprint(w, `{"sessions":0,"log":0}`)
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
	if _, err := installCurrentHarnessIntegrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	report := runDoctor(context.Background())
	if !report.OK {
		t.Fatalf("doctor report: %#v", report)
	}
}

func TestRunDoctorReportsMissingConnection(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	report := runDoctor(context.Background())
	if report.OK || len(report.Checks) != 1 || report.Checks[0].Name != "connection" {
		t.Fatalf("doctor report: %#v", report)
	}
}
