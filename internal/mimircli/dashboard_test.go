package mimircli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

func TestDashboardOpensDeploymentURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("dashboard should not make an API request")
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	oldOpenBrowser := openBrowser
	t.Cleanup(func() { openBrowser = oldOpenBrowser })
	var opened string
	openBrowser = func(_ context.Context, target string) error { opened = target; return nil }
	var output bytes.Buffer
	if err := dashboard(context.Background(), IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if opened != server.URL+"/dashboard/" {
		t.Fatalf("opened %q", opened)
	}
	if output.String() != "Dashboard: "+server.URL+"/dashboard/\n" {
		t.Fatalf("output %q", output.String())
	}
	openBrowser = func(context.Context, string) error { return errors.New("no browser") }
	output.Reset()
	if err := dashboard(context.Background(), IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Open "+server.URL+"/dashboard/\n" {
		t.Fatalf("fallback output %q", output.String())
	}
}
