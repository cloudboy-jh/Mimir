package mimircli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunDoctorHealthyOpenCodeIntegration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(envMimirHome, t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/whoami" || r.Header.Get("Authorization") != "Bearer machine-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"sessions":0,"log":0}`)
	}))
	defer server.Close()
	if err := savePointer(Pointer{URL: server.URL, Token: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	if err := installCurrentOpenCodeIntegration(); err != nil {
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
