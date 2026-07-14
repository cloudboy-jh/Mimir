package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDeploymentURL(t *testing.T) {
	got, err := parseDeploymentURL(`[{"results":[{"value":"https://mimir.example.workers.dev"}]}]`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://mimir.example.workers.dev" {
		t.Fatalf("URL %q", got)
	}
}

func TestSQLQuote(t *testing.T) {
	if got := sqlQuote("jack's machine"); got != "jack''s machine" {
		t.Fatalf("SQL quote %q", got)
	}
}

func TestReadCloudflareIdentity(t *testing.T) {
	dir := t.TempDir()
	wrangler := filepath.Join(dir, "node_modules", ".bin")
	if err := os.MkdirAll(wrangler, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s' '{\"loggedIn\":true,\"authType\":\"OAuth Token\",\"email\":\"user@example.com\",\"accounts\":[{\"id\":\"abc\",\"name\":\"Example Account\"}]}'\n"
	if err := os.WriteFile(filepath.Join(wrangler, "wrangler"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	identity, err := readCloudflareIdentity(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Email != "user@example.com" || len(identity.Accounts) != 1 || identity.Accounts[0].Name != "Example Account" {
		t.Fatalf("identity %#v", identity)
	}
}

func TestLoginSummaryShowsUserAndConnection(t *testing.T) {
	var identity cloudflareIdentity
	identity.LoggedIn = true
	identity.AuthType = "OAuth Token"
	identity.Email = "user@example.com"
	identity.Accounts = append(identity.Accounts, struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{ID: "abc", Name: "Example Account"})

	summary := loginSummary(identity, "https://mimir.example.workers.dev/", false)
	for _, want := range []string{"◆ Cloudflare", "Email:    user@example.com", "Account:  Example Account", "Auth:     OAuth Token", "◆ Connection", "Worker:   https://mimir.example.workers.dev", "Status:   ✓ connected"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "┌") || strings.Contains(summary, "Mimir Login") {
		t.Fatalf("summary contains redundant title box:\n%s", summary)
	}
	if strings.Contains(summary, "\x1b[") {
		t.Fatalf("plain summary contains ANSI escapes:\n%s", summary)
	}
}

func TestLoginSummaryUsesMimirPalette(t *testing.T) {
	identity := cloudflareIdentity{LoggedIn: true, AuthType: "OAuth Token", Email: "user@example.com"}
	summary := loginSummary(identity, "https://mimir.example.workers.dev", true)
	for _, color := range []string{mimirMint, mimirGreen, mimirMutedGreen} {
		if !strings.Contains(summary, "38;2;"+color+"m") {
			t.Fatalf("summary missing palette color %s", color)
		}
	}
}
