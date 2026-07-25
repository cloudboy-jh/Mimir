package deployment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessAPIEnsureAppIsIdempotent(t *testing.T) {
	created := 0
	apps := []AccessApp{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/access/apps"):
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": apps})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/access/apps"):
			created++
			app := AccessApp{UID: "uid-1", Aud: "aud-1", Name: DashboardAccessAppName, Domain: "mimir.example.workers.dev/dashboard", SelfHostedDomains: DashboardAccessDomains("mimir.example.workers.dev")}
			apps = append(apps, app)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": app})
		default:
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	api := AccessClient{Base: server.URL, Token: "cf-token"}
	for i := 0; i < 2; i++ {
		app, err := api.EnsureApp(context.Background(), "acc-1", "mimir.example.workers.dev")
		if err != nil || app.Aud != "aud-1" {
			t.Fatalf("app %+v error %v", app, err)
		}
	}
	if created != 1 {
		t.Fatalf("created %d times", created)
	}
}

func TestAccessAPIEnsureAppCorrectsBareHostApp(t *testing.T) {
	updated := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []AccessApp{{UID: "uid-1", Aud: "aud-1", Name: DashboardAccessAppName, Domain: "mimir.example.workers.dev"}}})
		case http.MethodPut:
			updated++
			app := AccessApp{UID: "uid-1", Aud: "aud-1", Name: DashboardAccessAppName, Domain: "mimir.example.workers.dev/dashboard", SelfHostedDomains: DashboardAccessDomains("mimir.example.workers.dev")}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": app})
		default:
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	app, err := (AccessClient{Base: server.URL, Token: "cf-token"}).EnsureApp(context.Background(), "acc-1", "mimir.example.workers.dev")
	if err != nil || updated != 1 || app.Aud != "aud-1" {
		t.Fatalf("updated=%d app=%+v error=%v", updated, app, err)
	}
}

func TestAccessAPIEnsureAppDoesNotRewriteSameNameOnAnotherDomain(t *testing.T) {
	created, updated := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []AccessApp{{UID: "other", Name: DashboardAccessAppName, Domain: "other.example/dashboard", SelfHostedDomains: DashboardAccessDomains("other.example")}}})
		case http.MethodPost:
			created++
			app := AccessApp{UID: "intended", Aud: "aud-1", Name: DashboardAccessAppName, Domain: "mimir.example/dashboard", SelfHostedDomains: DashboardAccessDomains("mimir.example")}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": app})
		case http.MethodPut:
			updated++
		default:
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	app, err := (AccessClient{Base: server.URL, Token: "cf-token"}).EnsureApp(context.Background(), "acc-1", "mimir.example")
	if err != nil || app.UID != "intended" || created != 1 || updated != 0 {
		t.Fatalf("app=%+v created=%d updated=%d error=%v", app, created, updated, err)
	}
}

func TestAccessAPIEnsureEmailPolicy(t *testing.T) {
	policies := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": policies})
		case http.MethodPost:
			policies = append(policies, map[string]any{"uid": "policy-1", "name": DashboardAccessAppName + "-allow", "decision": "allow", "include": []map[string]any{{"email": map[string]any{"email": "user@example.com"}}}})
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": policies[0]})
		default:
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	api := AccessClient{Base: server.URL, Token: "cf-token"}
	if state, err := api.EnsureEmailPolicy(context.Background(), "acc-1", "uid-1", "user@example.com"); err != nil || state != "created" {
		t.Fatalf("state=%q error=%v", state, err)
	}
	if state, err := api.EnsureEmailPolicy(context.Background(), "acc-1", "uid-1", "user@example.com"); err != nil || state != "existing" {
		t.Fatalf("state=%q error=%v", state, err)
	}
}

func TestAccessAPIEnsureEmailPolicyRefusesConflictingPolicies(t *testing.T) {
	for _, policy := range []map[string]any{
		{"uid": "bypass", "name": "bypass", "decision": "bypass", "include": []map[string]any{{"everyone": map[string]any{}}}},
		{"uid": "permissive", "name": DashboardAccessAppName + "-allow", "decision": "allow", "include": []map[string]any{{"everyone": map[string]any{}}}},
		{"uid": "wrong-email", "name": DashboardAccessAppName + "-allow", "decision": "allow", "include": []map[string]any{{"email": map[string]any{"email": "other@example.com"}}}},
	} {
		policy := policy
		t.Run(policy["uid"].(string), func(t *testing.T) {
			posted := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posted = true
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]any{policy}})
			}))
			defer server.Close()
			state, err := (AccessClient{Base: server.URL, Token: "cf-token"}).EnsureEmailPolicy(context.Background(), "acc-1", "uid-1", "user@example.com")
			if err == nil || state != "action-required" || !strings.Contains(err.Error(), "action required") || posted {
				t.Fatalf("state=%q error=%v posted=%v", state, err, posted)
			}
		})
	}
}

func TestAccessAPIEnsureEmailPolicyRefusesAdditionalPolicy(t *testing.T) {
	policies := []map[string]any{
		{"uid": "exact", "name": DashboardAccessAppName + "-allow", "decision": "allow", "include": []map[string]any{{"email": map[string]any{"email": "user@example.com"}}}},
		{"uid": "extra", "name": "bypass", "decision": "bypass", "include": []map[string]any{{"everyone": map[string]any{}}}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": policies})
	}))
	defer server.Close()
	state, err := (AccessClient{Base: server.URL, Token: "cf-token"}).EnsureEmailPolicy(context.Background(), "acc-1", "uid-1", "user@example.com")
	if err == nil || state != "action-required" {
		t.Fatalf("state=%q error=%v", state, err)
	}
}

func TestConfigureDashboardAccessReportsPolicyActionRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any
		switch {
		case strings.HasSuffix(r.URL.Path, "/access/organizations"):
			result = map[string]any{"auth_domain": "team.cloudflareaccess.com"}
		case strings.HasSuffix(r.URL.Path, "/access/apps"):
			result = []AccessApp{{UID: "uid-1", Aud: "aud-1", Name: DashboardAccessAppName, Domain: "mimir.example/dashboard", SelfHostedDomains: DashboardAccessDomains("mimir.example")}}
		case strings.HasSuffix(r.URL.Path, "/policies"):
			result = []map[string]any{{"uid": "bypass", "name": "bypass", "decision": "bypass", "include": []map[string]any{{"everyone": map[string]any{}}}}}
		default:
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer server.Close()
	outcome, err := ConfigureDashboardAccess(context.Background(), AccessClient{Base: server.URL, Token: "cf-token"}, "acc-1", "https://mimir.example", "user@example.com")
	if err != nil || outcome.State != "action-required" || outcome.Policy != "action-required" {
		t.Fatalf("outcome=%#v error=%v", outcome, err)
	}
}

func TestConfigureDashboardAccessRequiresEmailPolicy(t *testing.T) {
	policyRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any
		switch {
		case strings.HasSuffix(r.URL.Path, "/access/organizations"):
			result = map[string]any{"auth_domain": "team.cloudflareaccess.com"}
		case strings.HasSuffix(r.URL.Path, "/access/apps"):
			result = []AccessApp{{UID: "uid-1", Aud: "aud-1", Name: DashboardAccessAppName, Domain: "mimir.example/dashboard", SelfHostedDomains: DashboardAccessDomains("mimir.example")}}
		case strings.HasSuffix(r.URL.Path, "/policies"):
			policyRequests++
			result = []any{}
		default:
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer server.Close()
	outcome, err := ConfigureDashboardAccess(context.Background(), AccessClient{Base: server.URL, Token: "cf-token"}, "acc-1", "https://mimir.example", "")
	if err != nil || outcome.State != "action-required" || outcome.Policy != "action-required" || policyRequests != 0 {
		t.Fatalf("outcome=%#v policyRequests=%d error=%v", outcome, policyRequests, err)
	}
}

func TestAccessAPIAuthDomainAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer cf-token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": "not authorized"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{"auth_domain": "team.cloudflareaccess.com"}})
	}))
	defer server.Close()
	domain, err := (AccessClient{Base: server.URL, Token: "cf-token"}).AuthDomain(context.Background(), "acc-1")
	if err != nil || domain != "https://team.cloudflareaccess.com" {
		t.Fatalf("domain %q error %v", domain, err)
	}
	if _, err := (AccessClient{Base: server.URL, Token: "bad"}).AuthDomain(context.Background(), "acc-1"); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("error %v", err)
	}
}
