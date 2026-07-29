package deployment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const CloudflareAPIBase = "https://api.cloudflare.com/client/v4"
const DashboardAccessAppName = "mimir-dashboard"

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type AccessClient struct {
	Base       string
	Token      string
	HTTPClient HTTPDoer
}

type AccessApp struct {
	UID               string   `json:"uid"`
	Aud               string   `json:"aud"`
	Name              string   `json:"name"`
	Domain            string   `json:"domain"`
	SelfHostedDomains []string `json:"self_hosted_domains"`
}

type AccessPolicy struct {
	UID      string           `json:"uid"`
	Name     string           `json:"name"`
	Decision string           `json:"decision"`
	Include  []map[string]any `json:"include"`
	Exclude  []map[string]any `json:"exclude"`
	Require  []map[string]any `json:"require"`
}

type AccessOutcome struct {
	State      string `json:"state"`
	TeamDomain string `json:"team_domain,omitempty"`
	Aud        string `json:"aud,omitempty"`
	Policy     string `json:"policy,omitempty"`
}

func (api AccessClient) call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, api.Base+path, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+api.Token)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	client := api.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
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

func (api AccessClient) AuthDomain(ctx context.Context, accountID string) (string, error) {
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
	domain := strings.TrimRight(org.AuthDomain, "/")
	if !strings.HasPrefix(domain, "https://") && !strings.HasPrefix(domain, "http://") {
		domain = "https://" + domain
	}
	return domain, nil
}

func (api AccessClient) ListApps(ctx context.Context, accountID string) ([]AccessApp, error) {
	result, err := api.call(ctx, "GET", "/accounts/"+url.PathEscape(accountID)+"/access/apps", nil)
	if err != nil {
		return nil, err
	}
	var apps []AccessApp
	if err := json.Unmarshal(result, &apps); err != nil {
		return nil, fmt.Errorf("Cloudflare API access apps: invalid response")
	}
	return apps, nil
}

func DashboardAccessDomains(host string) []string {
	return []string{host + "/dashboard/auth", host + "/dashboard/api/*", host + "/dashboard/log-objects/*"}
}

func (api AccessClient) EnsureApp(ctx context.Context, accountID, host string) (AccessApp, error) {
	desired := DashboardAccessDomains(host)
	apps, err := api.ListApps(ctx, accountID)
	if err != nil {
		return AccessApp{}, err
	}
	body := map[string]any{"name": DashboardAccessAppName, "domain": desired[0], "type": "self_hosted", "session_duration": "24h", "app_launcher_visible": false, "self_hosted_domains": desired}
	for _, app := range apps {
		primary := strings.TrimRight(app.Domain, "/")
		if primary != host && primary != desired[0] && primary != host+"/dashboard" {
			continue
		}
		if sameStrings(app.SelfHostedDomains, desired) {
			return app, nil
		}
		result, err := api.call(ctx, "PUT", "/accounts/"+url.PathEscape(accountID)+"/access/apps/"+url.PathEscape(app.UID), body)
		if err != nil {
			return AccessApp{}, err
		}
		var updated AccessApp
		if err := json.Unmarshal(result, &updated); err != nil || updated.UID == "" {
			return AccessApp{}, fmt.Errorf("Cloudflare API update access app: invalid response")
		}
		return updated, nil
	}
	result, err := api.call(ctx, "POST", "/accounts/"+url.PathEscape(accountID)+"/access/apps", body)
	if err != nil {
		return AccessApp{}, err
	}
	var app AccessApp
	if err := json.Unmarshal(result, &app); err != nil || app.UID == "" {
		return AccessApp{}, fmt.Errorf("Cloudflare API create access app: invalid response")
	}
	return app, nil
}

func (api AccessClient) EnsureEmailPolicy(ctx context.Context, accountID, appUID, email string) (string, error) {
	result, err := api.call(ctx, "GET", "/accounts/"+url.PathEscape(accountID)+"/access/apps/"+url.PathEscape(appUID)+"/policies", nil)
	if err != nil {
		return "", err
	}
	var policies []AccessPolicy
	if err := json.Unmarshal(result, &policies); err != nil {
		return "", fmt.Errorf("Cloudflare API access policies: invalid response")
	}
	if len(policies) == 1 && exactAllowEmailPolicy(policies[0], email) {
		return "existing", nil
	}
	if len(policies) > 0 {
		return "action-required", fmt.Errorf("dashboard Access policy action required: remove conflicting, permissive, or bypass policies and leave only an exact Allow policy for %s", email)
	}
	if _, err := api.call(ctx, "POST", "/accounts/"+url.PathEscape(accountID)+"/access/apps/"+url.PathEscape(appUID)+"/policies", map[string]any{"name": DashboardAccessAppName + "-allow", "decision": "allow", "precedence": 1, "include": []map[string]any{{"email": map[string]string{"email": email}}}}); err != nil {
		return "", err
	}
	return "created", nil
}

func exactAllowEmailPolicy(policy AccessPolicy, email string) bool {
	if policy.Name != DashboardAccessAppName+"-allow" || policy.Decision != "allow" || len(policy.Include) != 1 || len(policy.Exclude) != 0 || len(policy.Require) != 0 {
		return false
	}
	selector := policy.Include[0]
	if len(selector) != 1 {
		return false
	}
	emailSelector, ok := selector["email"].(map[string]any)
	if !ok || len(emailSelector) != 1 {
		return false
	}
	value, ok := emailSelector["email"].(string)
	return ok && strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(email))
}

func ConfigureDashboardAccess(ctx context.Context, api AccessClient, accountID, workerURL, email string) (AccessOutcome, error) {
	teamDomain, err := api.AuthDomain(ctx, accountID)
	if err != nil {
		return AccessOutcome{}, err
	}
	host := strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(workerURL, "/"), "https://"), "http://")
	app, err := api.EnsureApp(ctx, accountID, host)
	if err != nil {
		return AccessOutcome{}, err
	}
	outcome := AccessOutcome{State: "action-required", TeamDomain: teamDomain, Aud: app.Aud, Policy: "action-required"}
	email = strings.TrimSpace(email)
	if email == "" {
		return outcome, nil
	}
	outcome.Policy, err = api.EnsureEmailPolicy(ctx, accountID, app.UID, email)
	if err != nil {
		if outcome.Policy == "action-required" {
			return outcome, nil
		}
		return outcome, err
	}
	outcome.State = "configured"
	return outcome, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
