package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type sessionInitOptions struct {
	// Repo overrides discovery. Prefer empty so gh path runs.
	Repo string
	// LoginOverride skips gh user lookup for tests.
	LoginOverride string
	// SkipGH for pure path/repo clone tests.
	SkipGH bool
	// Create allows calling gh repo create when missing (default true when not SkipGH).
	NoCreate bool
}

type sessionInitResult struct {
	Repo    string
	Path    string
	Created bool
	Cloned  bool
	Pulled  bool
	Receipt Receipt
}

func sessionInit(ctx context.Context, opts sessionInitOptions) (sessionInitResult, error) {
	// Ensure control plane exists first.
	cfg, err := controlInit("")
	if err != nil {
		return sessionInitResult{}, err
	}

	dir, err := sessionsAbsPath(cfg)
	if err != nil {
		return sessionInitResult{}, err
	}

	// 1. Config already has a remote
	if opts.Repo == "" && strings.TrimSpace(cfg.Sessions.Repo) != "" {
		opts.Repo = strings.TrimSpace(cfg.Sessions.Repo)
	}

	// 2. Discover via gh if still empty
	created := false
	if opts.Repo == "" && !opts.SkipGH {
		login := opts.LoginOverride
		if login == "" {
			login, err = ghLogin(ctx)
			if err != nil {
				r := failReceipt("session", "init", "no gh auth · sessions disabled")
				_ = appendLog(cfg, "session.init", "no-gh-auth", "fail")
				return sessionInitResult{Receipt: r}, fmt.Errorf("no git provider auth: %w", err)
			}
		}
		exists, vis, err := ghRepoExists(ctx, login, "mimir-sessions")
		if err != nil {
			return sessionInitResult{}, err
		}
		if exists {
			if err := refuseBadSessionTarget(login+"/mimir-sessions", vis, true); err != nil {
				r := failReceipt("session", "init", err.Error())
				_ = appendLog(cfg, "session.init", "refuse "+err.Error(), "fail")
				return sessionInitResult{Receipt: r}, err
			}
			opts.Repo = fmt.Sprintf("https://github.com/%s/mimir-sessions.git", login)
		} else if !opts.NoCreate {
			if err := ghRepoCreate(ctx, login, "mimir-sessions"); err != nil {
				r := failReceipt("session", "init", "gh repo create failed")
				_ = appendLog(cfg, "session.init", "create-fail", "fail")
				return sessionInitResult{Receipt: r}, err
			}
			created = true
			opts.Repo = fmt.Sprintf("https://github.com/%s/mimir-sessions.git", login)
			_ = appendLog(cfg, "session.init", fmt.Sprintf("create %s/mimir-sessions private", login), "ok")
		} else {
			r := failReceipt("session", "init", "mimir-sessions missing and create disabled")
			return sessionInitResult{Receipt: r}, fmt.Errorf("repo %s/mimir-sessions does not exist", login)
		}
	}

	if opts.Repo == "" {
		r := failReceipt("session", "init", "no sessions remote")
		return sessionInitResult{Receipt: r}, fmt.Errorf("no sessions remote; sign into gh or set sessions.repo")
	}

	// Refuse public application monorepos if we can inspect GitHub URL
	if err := validateSessionRemote(opts.Repo); err != nil {
		r := failReceipt("session", "init", err.Error())
		_ = appendLog(cfg, "session.init", "refuse "+err.Error(), "fail")
		return sessionInitResult{Receipt: r}, err
	}

	cloned, pulled := false, false
	if pathExists(filepath.Join(dir, ".git")) {
		// already a repo - set remote if needed, pull
		if rem, err := runGit(ctx, dir, "config", "--get", "remote.origin.url"); err != nil || rem == "" {
			_, _ = runGit(ctx, dir, "remote", "add", "origin", opts.Repo)
		}
		if _, err := runGit(ctx, dir, "pull", "--ff-only"); err == nil {
			pulled = true
		}
	} else if pathExists(dir) {
		// directory exists; try clone into temp then move, or initialize
		entries, _ := os.ReadDir(dir)
		if len(entries) == 0 {
			if err := cloneSessions(ctx, opts.Repo, dir); err != nil {
				return sessionInitResult{}, err
			}
			cloned = true
		} else {
			return sessionInitResult{}, fmt.Errorf("sessions path not empty and not a git repo: %s", dir)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return sessionInitResult{}, err
		}
		if err := cloneSessions(ctx, opts.Repo, dir); err != nil {
			// empty remote might fail clone; init empty repo as fallback
			if err2 := os.MkdirAll(dir, 0o755); err2 != nil {
				return sessionInitResult{}, err
			}
			if _, err2 := runGit(ctx, dir, "init"); err2 != nil {
				return sessionInitResult{}, err
			}
			_, _ = runGit(ctx, dir, "remote", "add", "origin", opts.Repo)
			// optional README so first push is easy
			readme := filepath.Join(dir, "README.md")
			_ = os.WriteFile(readme, []byte("# mimir-sessions\n\nAgent session sync for Mimir.\n"), 0o644)
			_, _ = runGit(ctx, dir, "add", "README.md")
			_, _ = runGit(ctx, dir, "commit", "-m", "init mimir-sessions")
			cloned = true
			_ = appendLog(cfg, "session.init", "init-empty-local", "ok")
		} else {
			cloned = true
		}
	}

	if cloned {
		_ = appendLog(cfg, "session.clone", "path="+dir, "ok")
	}

	cfg.Sessions.Enabled = true
	cfg.Sessions.Repo = opts.Repo
	cfg.Sessions.Path = displaySessionsPathMust()
	if err := saveControlConfig(cfg); err != nil {
		return sessionInitResult{}, err
	}

	action := "bind"
	if created {
		action = "create"
	} else if cloned {
		action = "clone"
	} else if pulled {
		action = "pull"
	}
	_ = appendLog(cfg, "session.init", fmt.Sprintf("%s %s", action, opts.Repo), "ok")

	return sessionInitResult{
		Repo:    opts.Repo,
		Path:    dir,
		Created: created,
		Cloned:  cloned,
		Pulled:  pulled,
		Receipt: Receipt{
			Plane:   "session",
			Verb:    "init",
			Subject: opts.Repo,
			Meaning: actMeaning(created, cloned, pulled),
			Status:  "ok",
		},
	}, nil
}

func displaySessionsPathMust() string {
	return filepath.ToSlash(filepath.Join("~", ".mimir", sessionsDir))
}

func actMeaning(created, cloned, pulled bool) string {
	switch {
	case created:
		return "private repo created · cloned"
	case cloned:
		return "cloned sessions remote"
	case pulled:
		return "pulled existing sessions"
	default:
		return "sessions bound"
	}
}

func cloneSessions(ctx context.Context, repo, dir string) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	// git clone <repo> <dir>
	cmd := exec.CommandContext(ctx, "git", "clone", repo, dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func validateSessionRemote(repo string) error {
	lower := strings.ToLower(repo)
	// crude refuse: if name is a well-known app monorepo slug
	bad := []string{"glib-code", "gittrix", "opencode", "hermes"}
	for _, b := range bad {
		if strings.Contains(lower, "/"+b+".git") || strings.Contains(lower, "/"+b+"/") || strings.HasSuffix(lower, "/"+b) {
			return fmt.Errorf("refuse application monorepo as sessions remote: %s", b)
		}
	}
	if strings.Contains(lower, "mimir-sessions") {
		return nil
	}
	// allow non-default private session repos the user already owns, but flag monorepo-ish paths
	return nil
}

func refuseBadSessionTarget(name, visibility string, mustBePrivate bool) error {
	if mustBePrivate && strings.EqualFold(visibility, "public") {
		return fmt.Errorf("refuse public repo for sessions: %s", name)
	}
	return nil
}

func ghLogin(ctx context.Context) (string, error) {
	// Prefer gh api user for accurate login
	out, err := runCmd(ctx, "", "gh", "api", "user", "--jq", ".login")
	if err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	// fallback: gh auth status parsing is brittle; try api without jq
	out, err = runCmd(ctx, "", "gh", "api", "user")
	if err != nil {
		return "", fmt.Errorf("gh not authenticated")
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal([]byte(out), &u); err != nil || u.Login == "" {
		return "", fmt.Errorf("gh user lookup failed")
	}
	return u.Login, nil
}

func ghRepoExists(ctx context.Context, login, name string) (exists bool, visibility string, err error) {
	out, err := runCmd(ctx, "", "gh", "api", fmt.Sprintf("repos/%s/%s", login, name))
	if err != nil {
		// 404 → missing
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") {
			return false, "", nil
		}
		// gh returns exit 1 with message
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "not found") || strings.Contains(low, "404") {
			return false, "", nil
		}
		return false, "", err
	}
	var r struct {
		Private    bool   `json:"private"`
		Visibility string `json:"visibility"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return true, "unknown", nil
	}
	vis := r.Visibility
	if vis == "" {
		if r.Private {
			vis = "private"
		} else {
			vis = "public"
		}
	}
	return true, vis, nil
}

func ghRepoCreate(ctx context.Context, login, name string) error {
	// --private --confirm (older gh) or --private (newer)
	_, err := runCmd(ctx, "", "gh", "repo", "create", login+"/"+name,
		"--private",
		"--description", "Mimir agent session sync",
		"--confirm",
	)
	if err != nil {
		// retry without --confirm for newer gh
		_, err2 := runCmd(ctx, "", "gh", "repo", "create", login+"/"+name,
			"--private",
			"--description", "Mimir agent session sync",
		)
		return err2
	}
	return nil
}

func runCmd(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), text, err)
	}
	return text, nil
}
