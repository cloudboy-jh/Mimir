package mimircli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessAPIEnsureAppIsIdempotent(t *testing.T) {
	created := 0
	apps := []accessApp{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer cf-token" {
			t.Fatalf("authorization %q", r.Header.Get("authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/acc-1/access/apps":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": apps})
		case r.Method == http.MethodPost && r.URL.Path == "/accounts/acc-1/access/apps":
			created++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["domain"] != "mimir.example.workers.dev/dashboard" || body["type"] != "self_hosted" {
				t.Fatalf("app body %v", body)
			}
			domains, _ := body["self_hosted_domains"].([]any)
			if len(domains) != 2 || domains[1] != "mimir.example.workers.dev/dashboard/*" {
				t.Fatalf("app destinations %v", domains)
			}
			app := accessApp{UID: "uid-1", Aud: "aud-1", Name: dashboardAccessAppName, Domain: "mimir.example.workers.dev/dashboard", SelfHostedDomains: []string{"mimir.example.workers.dev/dashboard", "mimir.example.workers.dev/dashboard/*"}}
			apps = append(apps, app)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": app})
		default:
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	api := accessAPI{base: server.URL, token: "cf-token"}
	app, err := api.ensureApp(context.Background(), "acc-1", "mimir.example.workers.dev")
	if err != nil {
		t.Fatal(err)
	}
	if app.Aud != "aud-1" || created != 1 {
		t.Fatalf("app %+v created=%d", app, created)
	}
	app, err = api.ensureApp(context.Background(), "acc-1", "mimir.example.workers.dev")
	if err != nil {
		t.Fatal(err)
	}
	if app.Aud != "aud-1" || created != 1 {
		t.Fatalf("idempotent app %+v created=%d", app, created)
	}
}

func TestAccessAPIEnsureAppCorrectsBareHostApp(t *testing.T) {
	updated := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/acc-1/access/apps":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": []accessApp{{UID: "uid-1", Aud: "aud-1", Name: dashboardAccessAppName, Domain: "mimir.example.workers.dev"}}})
		case r.Method == http.MethodPut && r.URL.Path == "/accounts/acc-1/access/apps/uid-1":
			updated++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			domains, _ := body["self_hosted_domains"].([]any)
			if body["domain"] != "mimir.example.workers.dev/dashboard" || len(domains) != 2 {
				t.Fatalf("update body %v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": accessApp{UID: "uid-1", Aud: "aud-1", Name: dashboardAccessAppName, Domain: "mimir.example.workers.dev/dashboard", SelfHostedDomains: []string{"mimir.example.workers.dev/dashboard", "mimir.example.workers.dev/dashboard/*"}}})
		default:
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	api := accessAPI{base: server.URL, token: "cf-token"}
	app, err := api.ensureApp(context.Background(), "acc-1", "mimir.example.workers.dev")
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 || app.Aud != "aud-1" {
		t.Fatalf("bare-host app was not corrected: updated=%d app=%+v", updated, app)
	}
}

func TestAccessAPIEnsureEmailPolicy(t *testing.T) {
	policies := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/policies"):
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": policies})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/policies"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["decision"] != "allow" {
				t.Fatalf("policy body %v", body)
			}
			policies = append(policies, map[string]any{"uid": "pol-1"})
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": map[string]any{"uid": "pol-1"}})
		default:
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	api := accessAPI{base: server.URL, token: "cf-token"}
	state, err := api.ensureEmailPolicy(context.Background(), "acc-1", "uid-1", "user@example.com")
	if err != nil || state != "created" {
		t.Fatalf("policy %s %v", state, err)
	}
	state, err = api.ensureEmailPolicy(context.Background(), "acc-1", "uid-1", "user@example.com")
	if err != nil || state != "existing" {
		t.Fatalf("existing policy %s %v", state, err)
	}
}

func TestAccessAPIAuthDomain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/acc-1/access/organizations" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": map[string]any{"auth_domain": "team.cloudflareaccess.com"}})
	}))
	defer server.Close()
	api := accessAPI{base: server.URL, token: "cf-token"}
	domain, err := api.authDomain(context.Background(), "acc-1")
	if err != nil || domain != "https://team.cloudflareaccess.com" {
		t.Fatalf("scheme-less auth domain %q %v", domain, err)
	}
}

func TestAccessAPIErrorSurfaced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": "not authorized"}}, "result": nil})
	}))
	defer server.Close()
	api := accessAPI{base: server.URL, token: "bad"}
	if _, err := api.authDomain(context.Background(), "acc-1"); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("error %v", err)
	}
}

func TestAccessChecklistScopesToDashboard(t *testing.T) {
	checklist := accessChecklist("https://mimir.example.workers.dev")
	if !strings.Contains(checklist, "mimir.example.workers.dev/dashboard and mimir.example.workers.dev/dashboard/*") {
		t.Fatalf("checklist is missing the exact destinations:\n%s", checklist)
	}
	if strings.Contains(checklist, "leave the path blank") || strings.Contains(checklist, "Bypass") || strings.Contains(checklist, "wrangler deploy") {
		t.Fatalf("checklist still carries a broken manual flow:\n%s", checklist)
	}
}

func TestUpdateWranglerVars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrangler.jsonc")
	initial := "{\n  // worker name\n  \"name\": \"mimir\",\n  \"vars\": {\"KEEP\": \"1\"},\n}\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateWranglerVars(path, map[string]string{"DASHBOARD_ACCESS_AUD": "aud-1", "DASHBOARD_ACCESS_TEAM_DOMAIN": "https://team.cloudflareaccess.com"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("rewritten config invalid: %v", err)
	}
	vars, ok := config["vars"].(map[string]any)
	if !ok {
		t.Fatalf("vars %v", config["vars"])
	}
	if vars["KEEP"] != "1" || vars["DASHBOARD_ACCESS_AUD"] != "aud-1" || config["name"] != "mimir" {
		t.Fatalf("config %v", config)
	}
}
