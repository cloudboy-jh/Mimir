package mimircli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardOpensOneTimeURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("dashboard should not make an API request")
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
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
	if !strings.Contains(opened, "/dashboard") {
		t.Fatalf("opened %q", opened)
	}
	if strings.Contains(opened, "test-token") {
		t.Fatalf("machine token leaked in URL %q", opened)
	}
}
