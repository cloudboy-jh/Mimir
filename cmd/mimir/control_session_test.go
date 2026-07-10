package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseControlTOMLRoundTrip(t *testing.T) {
	t.Parallel()
	src := `
machine = "therig"

[sessions]
enabled = true
repo = "https://github.com/cloudboy-jh/mimir-sessions.git"
path = "~/.mimir/sessions"
default_harness = "hermes"

[code]
prefer_mcp = true
auto_index_if_stale = false

[log]
path = "~/.mimir/mimir.log"
level = "info"
`
	cfg, err := parseControlTOML(src)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Machine != "therig" {
		t.Fatalf("machine: %q", cfg.Machine)
	}
	if !cfg.Sessions.Enabled || cfg.Sessions.Repo == "" {
		t.Fatalf("sessions: %+v", cfg.Sessions)
	}
	if cfg.Code.AutoIndexIfStale {
		t.Fatal("expected auto_index_if_stale false")
	}
	out := encodeControlTOML(cfg)
	cfg2, err := parseControlTOML(out)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Machine != cfg.Machine || cfg2.Sessions.Repo != cfg.Sessions.Repo {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", cfg, cfg2)
	}
}

func TestControlInitCreatesFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envMimirHome, home)

	cfg, err := controlInit("therig")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Machine != "therig" {
		t.Fatalf("machine %q", cfg.Machine)
	}
	if !pathExists(filepath.Join(home, configFile)) {
		t.Fatal("config missing")
	}
	if !pathExists(filepath.Join(home, defaultLog)) {
		t.Fatal("log missing")
	}
	// idempotent
	cfg2, err := controlInit("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Machine != "therig" {
		t.Fatalf("idempotent machine %q", cfg2.Machine)
	}
}

func TestReceiptMark(t *testing.T) {
	t.Parallel()
	r := Receipt{Plane: "control", Verb: "init", Subject: "machine=therig", Meaning: "sessions off · code mcp optional", Status: "ok"}
	s := r.String()
	if !strings.HasPrefix(s, "◆ mimir  control.init") {
		t.Fatalf("weird receipt:\n%s", s)
	}
}

func TestSessionFileNameAndRender(t *testing.T) {
	t.Parallel()
	name := sessionFileName("therig", "hermes", "gittrix-v2")
	if name != "sessions/therig-hermes-gittrix-v2.md" {
		t.Fatalf("name %s", name)
	}
	doc := renderSessionDoc(SessionMeta{
		SessionID: "gittrix-v2",
		Machine:   "therig",
		Harness:   "hermes",
		Project:   "gittrix",
		Goal:      "split adapters",
	})
	if !strings.Contains(doc, "session_id: gittrix-v2") {
		t.Fatal(doc)
	}
	if !strings.Contains(doc, "## Current Goal\nsplit adapters") {
		t.Fatal(doc)
	}
}

func TestValidateSessionRemoteRefusesMonorepo(t *testing.T) {
	t.Parallel()
	if err := validateSessionRemote("https://github.com/x/gittrix.git"); err == nil {
		t.Fatal("expected refuse")
	}
	if err := validateSessionRemote("https://github.com/x/mimir-sessions.git"); err != nil {
		t.Fatal(err)
	}
}

func TestSessionPushLocalNoPush(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envMimirHome, home)

	if _, err := controlInit("therig"); err != nil {
		t.Fatal(err)
	}
	// prepare sessions dir as non-remote local write only
	sess := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sess, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadControlConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Sessions.Enabled = true
	cfg.Sessions.Path = sess // abs path ok for expand? expandHomePath leaves abs as-is
	if err := saveControlConfig(cfg); err != nil {
		t.Fatal(err)
	}

	res, err := sessionPush(t.Context(), sessionPushOptions{
		ID:      "gittrix-v2",
		Harness: "hermes",
		Project: "gittrix",
		Goal:    "test",
		NoPush:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pathExists(res.AbsPath) {
		t.Fatal("session file missing")
	}
	if res.Receipt.Status != "ok" {
		t.Fatalf("receipt %+v", res.Receipt)
	}
}

func TestAppendLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envMimirHome, home)
	cfg, err := controlInit("box")
	if err != nil {
		t.Fatal(err)
	}
	if err := appendLog(cfg, "session.push", "therig-hermes-x sha=abc", "ok"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, defaultLog))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "session.push") {
		t.Fatalf("log: %s", data)
	}
}
