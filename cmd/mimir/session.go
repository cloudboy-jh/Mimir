package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,80}$`)

type SessionMeta struct {
	SessionID string
	Machine   string
	Harness   string
	Project   string
	Timestamp string
	Status    string
	Goal      string
	Body      string
}

func sessionFileName(machine, harness, id string) string {
	return fmt.Sprintf("sessions/%s-%s-%s.md", machine, harness, id)
}

func validateSessionID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("session id required")
	}
	if !sessionIDRe.MatchString(id) {
		return fmt.Errorf("invalid session id %q (use alnum, dash, underscore, dot)", id)
	}
	return nil
}

func renderSessionDoc(m SessionMeta) string {
	if m.Timestamp == "" {
		m.Timestamp = time.Now().Format(time.RFC3339)
	}
	if m.Status == "" {
		m.Status = "active"
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "session_id: %s\n", m.SessionID)
	fmt.Fprintf(&b, "machine: %s\n", m.Machine)
	fmt.Fprintf(&b, "harness: %s\n", m.Harness)
	if m.Project != "" {
		fmt.Fprintf(&b, "project: %s\n", m.Project)
	}
	fmt.Fprintf(&b, "timestamp: %s\n", m.Timestamp)
	fmt.Fprintf(&b, "status: %s\n", m.Status)
	b.WriteString("---\n\n")
	title := m.SessionID
	if m.Project != "" {
		title = m.Project + " / " + m.SessionID
	}
	fmt.Fprintf(&b, "# Session: %s\n\n", title)
	b.WriteString("## Current Goal\n")
	if m.Goal != "" {
		fmt.Fprintf(&b, "%s\n\n", m.Goal)
	} else {
		b.WriteString("\n")
	}
	b.WriteString("## State Variables\n\n")
	b.WriteString("## Progress\n")
	b.WriteString("- [ ] \n\n")
	b.WriteString("## Context Brief\n")
	if m.Body != "" {
		b.WriteString(strings.TrimRight(m.Body, "\n"))
		b.WriteString("\n")
	} else {
		b.WriteString("\n")
	}
	return b.String()
}

type sessionPushOptions struct {
	ID      string
	Harness string
	Project string
	Goal    string
	Body    string
	NoPush  bool // write file only (tests)
}

type sessionPushResult struct {
	FileName string
	AbsPath  string
	SHA      string
	Receipt  Receipt
}

func sessionPush(ctx context.Context, opts sessionPushOptions) (sessionPushResult, error) {
	cfg, err := loadControlConfig()
	if err != nil {
		return sessionPushResult{}, err
	}
	if err := validateSessionID(opts.ID); err != nil {
		return sessionPushResult{}, err
	}
	if cfg.Machine == "" {
		cfg.Machine = deriveMachine()
	}
	harness := opts.Harness
	if harness == "" {
		harness = cfg.Sessions.DefaultHarness
	}
	if harness == "" {
		harness = "agent"
	}
	if !cfg.Sessions.Enabled {
		r := failReceipt("session", "push", "sessions disabled")
		_ = appendLog(cfg, "session.push", opts.ID+" disabled", "fail")
		return sessionPushResult{Receipt: r}, fmt.Errorf("sessions disabled; run mimir session init")
	}
	dir, err := sessionsAbsPath(cfg)
	if err != nil {
		return sessionPushResult{}, err
	}
	if !pathExists(dir) {
		r := failReceipt("session", "push", "sessions path missing")
		return sessionPushResult{Receipt: r}, fmt.Errorf("sessions path missing: %s", dir)
	}

	name := sessionFileName(cfg.Machine, harness, opts.ID)
	abs := filepath.Join(dir, name)
	
	// Ensure the parent sessions/ subdirectory physically exists
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return sessionPushResult{}, err
	}
	
	doc := renderSessionDoc(SessionMeta{
		SessionID: opts.ID,
		Machine:   cfg.Machine,
		Harness:   harness,
		Project:   opts.Project,
		Goal:      opts.Goal,
		Body:      opts.Body,
	})
	if err := os.WriteFile(abs, []byte(doc), 0o644); err != nil {
		return sessionPushResult{}, err
	}

	sha := ""
	if !opts.NoPush {
		if _, err := runGit(ctx, dir, "rev-parse", "--is-inside-work-tree"); err != nil {
			r := failReceipt("session", "push", "sessions path is not a git repo")
			_ = appendLog(cfg, "session.push", name+" not-a-repo", "fail")
			return sessionPushResult{Receipt: r}, fmt.Errorf("sessions path is not a git repo: %s", dir)
		}
		if _, err := runGit(ctx, dir, "add", name); err != nil {
			return sessionPushResult{}, err
		}
		// commit only if something staged
		status, _ := runGit(ctx, dir, "status", "--porcelain", name)
		if strings.TrimSpace(status) != "" {
			msg := fmt.Sprintf("session: %s", name)
			if _, err := runGit(ctx, dir, "commit", "-m", msg); err != nil {
				return sessionPushResult{}, err
			}
		}
		sha, _ = runGit(ctx, dir, "rev-parse", "--short", "HEAD")
		// push if remote exists
		if rem, err := runGit(ctx, dir, "remote"); err == nil && strings.TrimSpace(rem) != "" {
			if _, err := runGit(ctx, dir, "push"); err != nil {
				r := failReceipt("session", "push", "git push failed")
				_ = appendLog(cfg, "session.push", name+" push-fail", "fail")
				return sessionPushResult{FileName: name, AbsPath: abs, SHA: sha, Receipt: r}, err
			}
		}
	}

	short := sha
	if len(short) > 7 {
		short = short[:7]
	}
	_ = appendLog(cfg, "session.push", fmt.Sprintf("%s sha=%s", name, short), "ok")
	receipt := Receipt{
		Plane:   "session",
		Verb:    "push",
		Subject: strings.TrimSuffix(name, ".md"),
		Status:  "ok",
		Metric:  short,
	}
	if opts.Goal != "" {
		receipt.Meaning = "goal: " + opts.Goal
	}
	return sessionPushResult{FileName: name, AbsPath: abs, SHA: sha, Receipt: receipt}, nil
}

type sessionPullResult struct {
	Updated bool
	SHA     string
	Files   []string
	Receipt Receipt
}

func sessionPull(ctx context.Context, id string) (sessionPullResult, error) {
	cfg, err := loadControlConfig()
	if err != nil {
		return sessionPullResult{}, err
	}
	if !cfg.Sessions.Enabled {
		r := failReceipt("session", "pull", "sessions disabled")
		return sessionPullResult{Receipt: r}, fmt.Errorf("sessions disabled; run mimir session init")
	}
	dir, err := sessionsAbsPath(cfg)
	if err != nil {
		return sessionPullResult{}, err
	}
	if !pathExists(dir) {
		r := failReceipt("session", "pull", "sessions path missing")
		return sessionPullResult{Receipt: r}, fmt.Errorf("sessions path missing: %s", dir)
	}
	if _, err := runGit(ctx, dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		r := failReceipt("session", "pull", "sessions path is not a git repo")
		return sessionPullResult{Receipt: r}, fmt.Errorf("sessions path is not a git repo")
	}

	before, _ := runGit(ctx, dir, "rev-parse", "HEAD")
	if rem, err := runGit(ctx, dir, "remote"); err == nil && strings.TrimSpace(rem) != "" {
		if _, err := runGit(ctx, dir, "pull", "--ff-only"); err != nil {
			// allow pull to fail if offline; still list local
			_ = appendLog(cfg, "session.pull", "pull-warn "+err.Error(), "warn")
		}
	}
	after, _ := runGit(ctx, dir, "rev-parse", "HEAD")
	sha, _ := runGit(ctx, dir, "rev-parse", "--short", "HEAD")
	files, err := listSessionFiles(dir, id)
	if err != nil {
		return sessionPullResult{}, err
	}
	subject := "sessions"
	if id != "" {
		subject = id
	} else if len(files) > 0 {
		subject = strings.TrimSuffix(filepath.Base(files[0]), ".md")
	}
	_ = appendLog(cfg, "session.pull", fmt.Sprintf("%s files=%d sha=%s", subject, len(files), sha), "ok")
	meaning := "ok · restored"
	if before != after {
		meaning = "updated · restored"
	}
	return sessionPullResult{
		Updated: before != after,
		SHA:     sha,
		Files:   files,
		Receipt: Receipt{
			Plane:   "session",
			Verb:    "pull",
			Subject: subject,
			Meaning: meaning,
			Status:  "ok",
			Metric:  sha,
		},
	}, nil
}

func listSessionFiles(dir, id string) ([]string, error) {
	sessionSubdir := filepath.Join(dir, "sessions")
	_ = os.MkdirAll(sessionSubdir, 0o755)
	entries, err := os.ReadDir(sessionSubdir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if id != "" && !strings.Contains(name, id) {
			continue
		}
		out = append(out, filepath.Join(sessionSubdir, name))
	}
	sort.Strings(out)
	return out, nil
}

func sessionList(cfg ControlConfig) ([]string, error) {
	dir, err := sessionsAbsPath(cfg)
	if err != nil {
		return nil, err
	}
	if !pathExists(dir) {
		return nil, nil
	}
	files, err := listSessionFiles(dir, "")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	return names, nil
}
