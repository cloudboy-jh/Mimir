package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const cloudflareAPIBase = "https://api.cloudflare.com/client/v4"
const dashboardAccessAppName = "mimir-dashboard"

type accessOutcome struct {
	State      string `json:"state"` // configured | manual
	TeamDomain string `json:"team_domain,omitempty"`
	Aud        string `json:"aud,omitempty"`
	Policy     string `json:"policy,omitempty"` // created | existing | skipped
}

// accessAPI is a minimal stdlib Cloudflare API client for Access resources.
type accessAPI struct {
	base  string
	token string
}

func (api accessAPI) call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, api.base+path, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+api.token)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("Cloudflare API %s %s: invalid response", method, path)
	}
	if !envelope.Success {
		message := res.Status
		if len(envelope.Errors) > 0 {
			message = envelope.Errors[0].Message
		}
		return nil, fmt.Errorf("Cloudflare API %s %s: %s", method, path, message)
	}
	return envelope.Result, nil
}

func (api accessAPI) authDomain(ctx context.Context, accountID string) (string, error) {
	result, err := api.call(ctx, "GET", "/accounts/"+url.PathEscape(accountID)+"/access/organizations", nil)
	if err != nil {
		return "", err
	}
	var org struct {
		AuthDomain string `json:"auth_domain"`
	}
	if err := json.Unmarshal(result, &org); err != nil || org.AuthDomain == "" {
		return "", fmt.Errorf("Cloudflare Access team domain not found; is Zero Trust enabled on this account?")
	}
	return strings.TrimRight(org.AuthDomain, "/"), nil
}

type accessApp struct {
	UID    string `json:"uid"`
	Aud    string `json:"aud"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

func (api accessAPI) listApps(ctx context.Context, accountID string) ([]accessApp, error) {
	result, err := api.call(ctx, "GET", "/accounts/"+url.PathEscape(accountID)+"/access/apps", nil)
	if err != nil {
		return nil, err
	}
	var apps []accessApp
	if err := json.Unmarshal(result, &apps); err != nil {
		return nil, fmt.Errorf("Cloudflare API access apps: invalid response")
	}
	return apps, nil
}

func (api accessAPI) ensureApp(ctx context.Context, accountID, domain string) (accessApp, error) {
	apps, err := api.listApps(ctx, accountID)
	if err != nil {
		return accessApp{}, err
	}
	for _, app := range apps {
		if app.Name == dashboardAccessAppName || strings.TrimRight(app.Domain, "/") == strings.TrimRight(domain, "/") {
			return app, nil
		}
	}
	result, err := api.call(ctx, "POST", "/accounts/"+url.PathEscape(accountID)+"/access/apps", map[string]any{
		"name":                 dashboardAccessAppName,
		"domain":               domain,
		"type":                 "self_hosted",
		"session_duration":     "24h",
		"app_launcher_visible": false,
	})
	if err != nil {
		return accessApp{}, err
	}
	var app accessApp
	if err := json.Unmarshal(result, &app); err != nil || app.UID == "" {
		return accessApp{}, fmt.Errorf("Cloudflare API create access app: invalid response")
	}
	return app, nil
}

func (api accessAPI) ensureEmailPolicy(ctx context.Context, accountID, appUID, email string) (string, error) {
	result, err := api.call(ctx, "GET", "/accounts/"+url.PathEscape(accountID)+"/access/apps/"+url.PathEscape(appUID)+"/policies", nil)
	if err != nil {
		return "", err
	}
	var policies []struct {
		UID string `json:"uid"`
	}
	if err := json.Unmarshal(result, &policies); err != nil {
		return "", fmt.Errorf("Cloudflare API access policies: invalid response")
	}
	if len(policies) > 0 {
		return "existing", nil
	}
	if _, err := api.call(ctx, "POST", "/accounts/"+url.PathEscape(accountID)+"/access/apps/"+url.PathEscape(appUID)+"/policies", map[string]any{
		"name":       dashboardAccessAppName + "-allow",
		"decision":   "allow",
		"precedence": 1,
		"include":    []map[string]any{{"email": map[string]string{"email": email}}},
	}); err != nil {
		return "", err
	}
	return "created", nil
}

// setupDashboardAccess standardizes the Access application protecting the
// dashboard API. It only runs when CLOUDFLARE_API_TOKEN is set; otherwise it
// returns a manual outcome and setup prints the checklist.
func setupDashboardAccess(ctx context.Context, dir string, opts setupOptions, workerURL string) (accessOutcome, error) {
	token := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
	if token == "" {
		return accessOutcome{State: "manual"}, nil
	}
	identity, err := readCloudflareIdentity(ctx, dir)
	if err != nil {
		return accessOutcome{}, err
	}
	if len(identity.Accounts) == 0 {
		return accessOutcome{}, fmt.Errorf("no Cloudflare account found; run wrangler login")
	}
	accountID := identity.Accounts[0].ID
	api := accessAPI{base: cloudflareAPIBase, token: token}
	teamDomain, err := api.authDomain(ctx, accountID)
	if err != nil {
		return accessOutcome{}, err
	}
	host := strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(workerURL, "/"), "https://"), "http://")
	app, err := api.ensureApp(ctx, accountID, host+"/dashboard")
	if err != nil {
		return accessOutcome{}, err
	}
	outcome := accessOutcome{State: "configured", TeamDomain: teamDomain, Aud: app.Aud, Policy: "skipped"}
	email := strings.TrimSpace(opts.AccessEmail)
	if email == "" {
		email = strings.TrimSpace(os.Getenv("MIMIR_ACCESS_EMAIL"))
	}
	if email != "" {
		outcome.Policy, err = api.ensureEmailPolicy(ctx, accountID, app.UID, email)
		if err != nil {
			return accessOutcome{}, err
		}
	}
	if err := updateWranglerVars(filepath.Join(dir, "wrangler.jsonc"), map[string]string{
		"DASHBOARD_ACCESS_AUD":         app.Aud,
		"DASHBOARD_ACCESS_TEAM_DOMAIN": teamDomain,
	}); err != nil {
		return accessOutcome{}, err
	}
	return outcome, nil
}

func accessChecklist(workerURL string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(workerURL, "/"), "https://"), "http://")
	return fmt.Sprintf(`Dashboard Access not configured (CLOUDFLARE_API_TOKEN not set). Manual steps:
  1. Zero Trust → Access → Applications → Add an application → Self-hosted
     Application name: %s
     Application domain: %s/dashboard
  2. Add an Allow policy for your email
  3. Copy the application's AUD tag from its Overview page
  4. Add to worker/wrangler.jsonc vars:
       "DASHBOARD_ACCESS_AUD": "<aud>",
       "DASHBOARD_ACCESS_TEAM_DOMAIN": "https://<team>.cloudflareaccess.com"
  5. wrangler deploy`, dashboardAccessAppName, host)
}
