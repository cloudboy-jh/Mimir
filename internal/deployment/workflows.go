package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type ProvisionResult struct {
	URL, Token string
	Access     AccessOutcome
}

func (s *Service) Provision(ctx context.Context, opts Options, hooks Hooks) (ProvisionResult, error) {
	dir, err := s.prepare(ctx, opts.WorkerDir, true)
	if err != nil {
		return ProvisionResult{}, err
	}
	hooks.step("Worker prepared")
	if err := s.ensureAuth(ctx, dir, hooks, opts.Noninteractive); err != nil {
		return ProvisionResult{}, err
	}
	hooks.step("Cloudflare authenticated")
	if opts.DatabaseID == "" {
		output, err := s.Wrangler.Run(ctx, dir, nil, "d1", "list", "--json")
		if err != nil {
			return ProvisionResult{}, err
		}
		opts.DatabaseID = listedDatabaseID(output, opts.DatabaseName)
		if opts.DatabaseID == "" {
			output, err = s.Wrangler.Run(ctx, dir, nil, "d1", "create", opts.DatabaseName)
			if err != nil {
				return ProvisionResult{}, err
			}
			opts.DatabaseID = databaseID(output)
		}
		if opts.DatabaseID == "" {
			return ProvisionResult{}, fmt.Errorf("could not read the D1 database ID; retry with --database-id")
		}
	}
	hooks.step("Database ready")
	if _, err := s.Wrangler.Run(ctx, dir, nil, "r2", "bucket", "create", opts.BucketName); err != nil && !alreadyExists(err.Error()) {
		return ProvisionResult{}, err
	}
	hooks.step("Archive ready")
	if _, err := s.configure(ctx, dir, opts); err != nil {
		return ProvisionResult{}, err
	}
	if _, err := s.Wrangler.Run(ctx, dir, nil, "d1", "migrations", "apply", opts.DatabaseName, "--remote"); err != nil {
		return ProvisionResult{}, err
	}
	hooks.step("Schema current")
	secretOutput, secretErr := s.Wrangler.Run(ctx, dir, nil, "secret", "list", "--format", "json")
	secretReady := secretErr == nil && listedSecret(secretOutput, "OPENROUTER_API_KEY")
	key := strings.TrimSpace(opts.OpenRouterKey)
	if key == "" && !secretReady {
		if opts.Noninteractive || hooks.PromptOpenRouterKey == nil {
			return ProvisionResult{}, StateError{State: "openrouter_key_required", Message: "set OPENROUTER_API_KEY and rerun setup"}
		}
		key, err = hooks.PromptOpenRouterKey()
		if err != nil {
			return ProvisionResult{}, err
		}
	}
	if key == "" && !secretReady {
		return ProvisionResult{}, fmt.Errorf("OpenRouter API key is required")
	}
	token, err := randomToken()
	if err != nil {
		return ProvisionResult{}, err
	}
	if err := s.registerMachineToken(ctx, dir, opts.DatabaseName, token); err != nil {
		return ProvisionResult{}, err
	}
	hooks.step("Machine registered")
	if key != "" {
		if _, err := s.Wrangler.Run(ctx, dir, secretReader(key), "secret", "put", "OPENROUTER_API_KEY"); err != nil {
			return ProvisionResult{}, err
		}
	}
	hooks.step("OpenRouter connected")
	output, err := s.Wrangler.Run(ctx, dir, nil, "deploy")
	if err != nil {
		return ProvisionResult{}, err
	}
	hooks.step("Worker deployed")
	url := workerURL(output)
	if url == "" {
		return ProvisionResult{}, fmt.Errorf("deployment succeeded but its workers.dev URL was not found; reconnect with mimir setup --url <url> --token <token>")
	}
	if hooks.Verify != nil {
		if err := hooks.Verify(ctx, url, token); err != nil {
			return ProvisionResult{}, fmt.Errorf("Worker deployed but whoami verification failed: %w", err)
		}
	}
	if hooks.Connected != nil {
		if err := hooks.Connected(ctx, url, token); err != nil {
			return ProvisionResult{}, err
		}
	}
	if err := s.storeDeploymentURL(ctx, dir, opts.DatabaseName, url); err != nil {
		return ProvisionResult{}, err
	}
	access := AccessOutcome{State: "manual"}
	accessToken := ""
	if hooks.PromptAccessToken != nil {
		accessToken, err = hooks.PromptAccessToken()
		if err != nil {
			return ProvisionResult{}, err
		}
	}
	if accessToken != "" {
		identity, err := s.ReadIdentity(ctx, dir)
		if err != nil {
			return ProvisionResult{}, err
		}
		if len(identity.Accounts) == 0 {
			return ProvisionResult{}, fmt.Errorf("no Cloudflare account found; run wrangler login")
		}
		client := s.Access
		client.Token = accessToken
		access, err = ConfigureDashboardAccess(ctx, client, identity.Accounts[0].ID, url, opts.AccessEmail)
		if err != nil {
			return ProvisionResult{}, fmt.Errorf("configuring dashboard Access: %w", err)
		}
		if access.State != "configured" {
			return ProvisionResult{URL: strings.TrimRight(url, "/"), Token: token, Access: access}, nil
		}
		if err := s.persistAccessVars(dir, access.Aud, access.TeamDomain); err != nil {
			return ProvisionResult{}, err
		}
		if _, err := s.Wrangler.Run(ctx, dir, nil, "deploy"); err != nil {
			return ProvisionResult{}, fmt.Errorf("applying dashboard Access configuration: %w", err)
		}
		hooks.step("Dashboard Access configured")
	}
	return ProvisionResult{URL: strings.TrimRight(url, "/"), Token: token, Access: access}, nil
}

type DeployResult struct{ URL string }

func (s *Service) Deploy(ctx context.Context, opts Options, hooks Hooks, fallbackURL string) (DeployResult, error) {
	dir, err := s.prepare(ctx, opts.WorkerDir, true)
	if err != nil {
		return DeployResult{}, err
	}
	hooks.step("Worker prepared")
	if err := s.ensureAuth(ctx, dir, hooks, opts.Noninteractive); err != nil {
		return DeployResult{}, err
	}
	hooks.step("Cloudflare authenticated")
	opts, err = s.configure(ctx, dir, opts)
	if err != nil {
		return DeployResult{}, err
	}
	hooks.step("Database configured")
	if _, err := s.Wrangler.Run(ctx, dir, nil, "d1", "migrations", "apply", opts.DatabaseName, "--remote"); err != nil {
		return DeployResult{}, fmt.Errorf("applying database migrations: %w", err)
	}
	hooks.step("Schema current")
	output, err := s.Wrangler.Run(ctx, dir, nil, "deploy")
	if err != nil {
		return DeployResult{}, err
	}
	hooks.step("Worker deployed")
	url := workerURL(output)
	if url == "" {
		url = fallbackURL
	}
	if url != "" {
		if err := s.storeDeploymentURL(ctx, dir, opts.DatabaseName, url); err != nil {
			return DeployResult{}, err
		}
	}
	return DeployResult{URL: strings.TrimRight(url, "/")}, nil
}

type Identity struct {
	LoggedIn bool   `json:"loggedIn"`
	AuthType string `json:"authType"`
	Email    string `json:"email"`
	Accounts []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"accounts"`
}

func (s *Service) ReadIdentity(ctx context.Context, dir string) (Identity, error) {
	output, err := s.Wrangler.Run(ctx, dir, nil, "whoami", "--json")
	if err != nil {
		return Identity{}, fmt.Errorf("reading Cloudflare user: %w", err)
	}
	var identity Identity
	if err := json.Unmarshal([]byte(output), &identity); err != nil {
		return Identity{}, fmt.Errorf("reading Cloudflare user: %w", err)
	}
	if !identity.LoggedIn {
		return Identity{}, fmt.Errorf("Cloudflare user is not logged in")
	}
	return identity, nil
}

type LoginResult struct {
	Identity Identity
	URL      string
	Token    string
}

func (s *Service) Login(ctx context.Context, opts Options, hooks Hooks, explicitURL string) (LoginResult, error) {
	dir, err := s.prepare(ctx, opts.WorkerDir, false)
	if err != nil {
		return LoginResult{}, err
	}
	hooks.step("Worker prepared")
	identity, err := s.ReadIdentity(ctx, dir)
	if err != nil {
		if opts.Noninteractive {
			return LoginResult{}, StateError{State: "cloudflare_auth_required", Message: "run wrangler login in an interactive terminal"}
		}
		if hooks.Login != nil {
			err = hooks.Login(ctx, dir)
		} else {
			err = s.Wrangler.Interactive(ctx, dir, hooks.Streams, "login")
		}
		if err != nil {
			return LoginResult{}, fmt.Errorf("Cloudflare login failed: %w", err)
		}
		identity, err = s.ReadIdentity(ctx, dir)
		if err != nil {
			return LoginResult{}, fmt.Errorf("Cloudflare login could not be verified: %w", err)
		}
	}
	hooks.step("Cloudflare authenticated")
	opts, err = s.configure(ctx, dir, opts)
	if err != nil {
		if state, ok := err.(StateError); ok && state.State == "deployment_missing" {
			state.Message = "no Mimir deployment found in this Cloudflare account"
			return LoginResult{}, state
		}
		return LoginResult{}, err
	}
	hooks.step("Deployment found")
	url := strings.TrimRight(explicitURL, "/")
	if url == "" {
		url, err = s.deploymentURL(ctx, dir, opts.DatabaseName)
		if err != nil {
			return LoginResult{}, err
		}
	}
	if url == "" {
		return LoginResult{}, StateError{State: "deployment_url_missing", Message: "run mimir deploy, then rerun mimir login"}
	}
	token, err := randomToken()
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.registerMachineToken(ctx, dir, opts.DatabaseName, token); err != nil {
		return LoginResult{}, err
	}
	hooks.step("Machine registered")
	if hooks.Verify != nil {
		if err := hooks.Verify(ctx, url, token); err != nil {
			return LoginResult{}, err
		}
	}
	hooks.step("Connection verified")
	return LoginResult{Identity: identity, URL: url, Token: token}, nil
}

type AccessOptions struct {
	Options
	URL, Token, Email, Aud, TeamDomain string
}

func (s *Service) ConfigureAccess(ctx context.Context, opts AccessOptions, hooks Hooks) (AccessOutcome, error) {
	dir, err := s.prepare(ctx, opts.WorkerDir, false)
	if err != nil {
		return AccessOutcome{}, err
	}
	if err := s.ensureAuth(ctx, dir, hooks, opts.Noninteractive); err != nil {
		return AccessOutcome{}, err
	}
	if _, err := s.configure(ctx, dir, opts.Options); err != nil {
		return AccessOutcome{}, err
	}
	outcome := AccessOutcome{State: "configured", Aud: opts.Aud, TeamDomain: opts.TeamDomain, Policy: "skipped"}
	if outcome.Aud == "" {
		if opts.Token == "" {
			return AccessOutcome{State: "manual"}, nil
		}
		identity, err := s.ReadIdentity(ctx, dir)
		if err != nil {
			return AccessOutcome{}, err
		}
		if len(identity.Accounts) == 0 {
			return AccessOutcome{}, fmt.Errorf("no Cloudflare account found; run wrangler login")
		}
		client := s.Access
		client.Token = opts.Token
		outcome, err = ConfigureDashboardAccess(ctx, client, identity.Accounts[0].ID, opts.URL, opts.Email)
		if err != nil {
			return outcome, err
		}
	}
	if outcome.State != "configured" {
		return outcome, nil
	}
	// Persist before deploy for both API-managed and manually supplied Access values.
	if err := s.persistAccessVars(dir, outcome.Aud, outcome.TeamDomain); err != nil {
		return AccessOutcome{}, err
	}
	if _, err := s.Wrangler.Run(ctx, dir, nil, "deploy"); err != nil {
		return AccessOutcome{}, fmt.Errorf("applying dashboard Access configuration: %w", err)
	}
	return outcome, nil
}

func (s *Service) persistAccessVars(dir, aud, teamDomain string) error {
	return s.Wrangler.UpdateVars(filepath.Join(dir, "wrangler.jsonc"), map[string]string{
		"DASHBOARD_ACCESS_AUD": aud, "DASHBOARD_ACCESS_TEAM_DOMAIN": teamDomain,
	})
}
