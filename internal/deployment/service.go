package deployment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudboy-jh/mimir/internal/install"
)

type WorkerInstaller interface {
	WorkerDir(string) (string, error)
	MaterializeWorker(string) (string, error)
	EnsureWorkerDependencies(context.Context, string) error
	EnsureDashboardDependencies(context.Context, string) error
	BuildDashboard(context.Context, string) error
}

type installService struct{}

func (installService) WorkerDir(explicit string) (string, error) { return install.WorkerDir(explicit) }
func (installService) MaterializeWorker(source string) (string, error) {
	return install.MaterializeWorker(source)
}
func (installService) EnsureWorkerDependencies(ctx context.Context, dir string) error {
	return install.EnsureWorkerDependencies(ctx, dir)
}
func (installService) EnsureDashboardDependencies(ctx context.Context, dir string) error {
	return install.EnsureDashboardDependencies(ctx, dir)
}
func (installService) BuildDashboard(ctx context.Context, dir string) error {
	return install.BuildDashboard(ctx, dir)
}

type Service struct {
	Installer WorkerInstaller
	Wrangler  WranglerClient
	Access    AccessClient
	Now       func() time.Time
	Hostname  func() (string, error)
	LoadState func() (DeploymentState, error)
	SaveState func(DeploymentState) error
}

func NewService(httpClient HTTPDoer) *Service {
	return &Service{
		Installer: installService{}, Wrangler: Wrangler{},
		Access: AccessClient{Base: CloudflareAPIBase, HTTPClient: httpClient},
		Now:    time.Now, Hostname: os.Hostname, LoadState: LoadState, SaveState: SaveState,
	}
}

func (s *Service) resolveOptions(opts Options) Options {
	var state DeploymentState
	var err error
	if s.LoadState != nil {
		state, err = s.LoadState()
	}
	if err == nil {
		if opts.WorkerName == "" {
			opts.WorkerName = state.WorkerName
		}
		if opts.DatabaseName == "" {
			opts.DatabaseName = state.DatabaseName
		}
		if opts.DatabaseID == "" && state.DatabaseName == opts.DatabaseName {
			opts.DatabaseID = state.DatabaseID
		}
		if opts.BucketName == "" {
			opts.BucketName = state.BucketName
		}
	}
	defaults := DefaultOptions()
	if opts.WorkerName == "" {
		opts.WorkerName = defaults.WorkerName
	}
	if opts.DatabaseName == "" {
		opts.DatabaseName = defaults.DatabaseName
	}
	if opts.BucketName == "" {
		opts.BucketName = defaults.BucketName
	}
	return opts
}

func (s *Service) saveResolvedState(opts Options, url string) error {
	if s.SaveState == nil {
		return nil
	}
	return s.SaveState(DeploymentState{
		WorkerName: opts.WorkerName, DatabaseName: opts.DatabaseName, DatabaseID: opts.DatabaseID,
		BucketName: opts.BucketName, URL: strings.TrimRight(url, "/"), VerifiedAt: s.Now().UTC().Format(time.RFC3339),
	})
}

type Options struct {
	WorkerDir, WorkerName, DatabaseName, DatabaseID, BucketName string
	OpenRouterKey, AccessEmail                                  string
	Noninteractive                                              bool
}

func DefaultOptions() Options {
	return Options{WorkerName: "mimir", DatabaseName: "mimir", BucketName: "mimir-logs"}
}

type StateError struct {
	State   string `json:"state"`
	Message string `json:"message"`
}

func (e StateError) Error() string {
	data, _ := json.Marshal(e)
	return string(data)
}

type Hooks struct {
	Streams             Streams
	Step                func(string)
	Login               func(context.Context, string) error
	PromptOpenRouterKey func() (string, error)
	PromptAccessToken   func() (string, error)
	Verify              func(context.Context, string, string) error
	Connected           func(context.Context, string, string) error
}

func (h Hooks) step(message string) {
	if h.Step != nil {
		h.Step(message)
	}
}

type preparation int

const (
	prepareLogin preparation = iota
	prepareDeployment
)

func (s *Service) prepare(ctx context.Context, explicit string, mode preparation) (string, error) {
	dir, err := s.Installer.WorkerDir(explicit)
	if err != nil {
		return "", err
	}
	dir, err = s.Installer.MaterializeWorker(dir)
	if err != nil {
		return "", err
	}
	if mode == prepareLogin {
		return dir, nil
	}
	if err := s.Installer.EnsureWorkerDependencies(ctx, dir); err != nil {
		return "", fmt.Errorf("installing Worker dependencies: %w", err)
	}
	if explicit != "" {
		if err := s.Installer.EnsureDashboardDependencies(ctx, dir); err != nil {
			return "", fmt.Errorf("installing dashboard dependencies: %w", err)
		}
		if err := s.Installer.BuildDashboard(ctx, dir); err != nil {
			return "", fmt.Errorf("building dashboard: %w", err)
		}
	}
	return dir, nil
}

func (s *Service) ensureAuth(ctx context.Context, dir string, hooks Hooks, noninteractive bool) error {
	if _, err := s.Wrangler.Run(ctx, dir, nil, "whoami"); err == nil {
		return nil
	}
	if noninteractive {
		return StateError{State: "cloudflare_auth_required", Message: "run wrangler login in an interactive terminal"}
	}
	if hooks.Login != nil {
		if err := hooks.Login(ctx, dir); err != nil {
			return fmt.Errorf("Cloudflare login failed: %w", err)
		}
	} else if err := s.Wrangler.Interactive(ctx, dir, hooks.Streams, "login"); err != nil {
		return fmt.Errorf("Cloudflare login failed: %w", err)
	}
	if _, err := s.Wrangler.Run(ctx, dir, nil, "whoami"); err != nil {
		return fmt.Errorf("Cloudflare login could not be verified: %w", err)
	}
	return nil
}

func (s *Service) configure(ctx context.Context, dir string, opts Options) (Options, error) {
	if opts.DatabaseID == "" {
		output, err := s.Wrangler.Run(ctx, dir, nil, "d1", "list", "--json")
		if err != nil {
			return opts, err
		}
		opts.DatabaseID = listedDatabaseID(output, opts.DatabaseName)
	}
	if opts.DatabaseID == "" {
		return opts, StateError{State: "deployment_missing", Message: "no Mimir D1 database found; run mimir setup first"}
	}
	err := s.Wrangler.UpdateConfig(filepath.Join(dir, "wrangler.jsonc"), Config{
		WorkerName: opts.WorkerName, DatabaseName: opts.DatabaseName,
		DatabaseID: opts.DatabaseID, BucketName: opts.BucketName,
	})
	if err != nil {
		return opts, err
	}
	identity, err := install.EmbeddedWorkerIdentity()
	if err != nil {
		return opts, fmt.Errorf("reading embedded Worker identity: %w", err)
	}
	if opts.WorkerDir != "" {
		identity.Version, identity.SHA256 = "development", ""
	}
	if err := s.Wrangler.UpdateVars(filepath.Join(dir, "wrangler.jsonc"), map[string]string{
		"MIMIR_BUNDLE_VERSION": identity.Version,
		"MIMIR_BUNDLE_SHA256":  identity.SHA256,
	}); err != nil {
		return opts, fmt.Errorf("writing Worker identity: %w", err)
	}
	return opts, nil
}

func (s *Service) registerMachineToken(ctx context.Context, dir, databaseName, token string) error {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	label, err := s.Hostname()
	if err != nil || strings.TrimSpace(label) == "" {
		label = "machine"
	}
	sql := fmt.Sprintf("INSERT INTO access_tokens(token_hash, label, created_at) VALUES('%s', '%s', '%s') ON CONFLICT(token_hash) DO UPDATE SET revoked_at = NULL", sqlQuote(hash), sqlQuote(label), s.Now().UTC().Format(time.RFC3339))
	if _, err := s.Wrangler.Run(ctx, dir, nil, "d1", "execute", databaseName, "--remote", "--command", sql); err != nil {
		return fmt.Errorf("registering this machine: %w", err)
	}
	return nil
}

func (s *Service) storeDeploymentURL(ctx context.Context, dir, databaseName, url string) error {
	sql := fmt.Sprintf("INSERT INTO config(key, value) VALUES('deployment.url', '%s') ON CONFLICT(key) DO UPDATE SET value = excluded.value", sqlQuote(strings.TrimRight(url, "/")))
	_, err := s.Wrangler.Run(ctx, dir, nil, "d1", "execute", databaseName, "--remote", "--command", sql)
	return err
}

func (s *Service) deploymentURL(ctx context.Context, dir, databaseName string) (string, error) {
	output, err := s.Wrangler.Run(ctx, dir, nil, "d1", "execute", databaseName, "--remote", "--command", "SELECT value FROM config WHERE key = 'deployment.url'", "--json")
	if err != nil {
		return "", err
	}
	return parseDeploymentURL(output)
}

func parseDeploymentURL(output string) (string, error) {
	var result []struct {
		Results []struct {
			Value string `json:"value"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return "", err
	}
	if len(result) == 0 || len(result[0].Results) == 0 {
		return "", nil
	}
	return strings.TrimRight(result[0].Results[0].Value, "/"), nil
}

func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func sqlQuote(value string) string { return strings.ReplaceAll(value, "'", "''") }

func secretReader(value string) io.Reader { return strings.NewReader(value) }
