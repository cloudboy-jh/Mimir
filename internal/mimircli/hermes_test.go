package mimircli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallHermesIntegrationPreservesEnvAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".env")
	original := "OPENROUTER_API_KEY=original-key\nOTHER=value\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := testConnectionManifest(home)
	if err := installHermesIntegration(home, manifest); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(first)
	for _, want := range []string{
		"OPENROUTER_API_KEY=original-key",
		"OTHER=value",
		"OPENROUTER_BASE_URL=\"https://mimir.example.workers.dev/v1/hermes\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Hermes .env missing %q:\n%s", want, text)
		}
	}
	if strings.LastIndex(text, hermesManagedEnd) < strings.LastIndex(text, "OPENROUTER_API_KEY=original-key") {
		t.Fatal("managed values must be last so they override prior dotenv assignments")
	}
	if err := installHermesIntegration(home, manifest); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("idempotent Hermes install changed .env bytes")
	}
	if ok, detail := hermesIntegrationMatches(home, manifest); !ok {
		t.Fatalf("integration mismatch: %s", detail)
	}
	if key, err := hermesOpenRouterKey(home); err != nil || key != "original-key" {
		t.Fatalf("key=%q err=%v", key, err)
	}
}

func TestInstallHermesIntegrationRefreshesManagedValues(t *testing.T) {
	home := t.TempDir()
	manifest := testConnectionManifest(home)
	if err := installHermesIntegration(home, manifest); err != nil {
		t.Fatal(err)
	}
	manifest.OpenAIBaseURL = "https://new.example.workers.dev/v1"
	if err := installHermesIntegration(home, manifest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, hermesManagedStart) != 1 {
		t.Fatalf("duplicate managed block:\n%s", text)
	}
	if !strings.Contains(text, "https://new.example.workers.dev/v1/hermes") {
		t.Fatalf("managed block was not refreshed:\n%s", text)
	}
}

func TestUpsertHermesEnvRejectsMalformedManagedBlock(t *testing.T) {
	_, err := upsertHermesEnv([]byte(hermesManagedStart+"\nOPENROUTER_BASE_URL=x\n"), "https://mimir.test/v1/hermes")
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("error %v", err)
	}
}

func TestParseHermesDotenvCredentialSyntax(t *testing.T) {
	for name, input := range map[string]string{
		"single quoted":  "OPENROUTER_API_KEY='sk-or-single'\n",
		"interpolated":   "OPENROUTER_KEY=sk-or-expanded\nOPENROUTER_API_KEY=${OPENROUTER_KEY}\n",
		"inline comment": "OPENROUTER_API_KEY=sk-or-commented # account key\n",
	} {
		t.Run(name, func(t *testing.T) {
			values, err := parseHermesDotenv([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"single quoted": "sk-or-single", "interpolated": "sk-or-expanded", "inline comment": "sk-or-commented"}[name]
			if values["OPENROUTER_API_KEY"] != want {
				t.Fatalf("key=%q want=%q", values["OPENROUTER_API_KEY"], want)
			}
		})
	}
}

func TestDiscoverHermesHomeUsesActiveProfile(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "profiles", "coder")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "active_profile"), []byte("coder\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, found, err := resolveHermesProfileHome(root)
	if err != nil || !found || got != filepath.Join(root, "profiles", "coder") {
		t.Fatalf("home=%q found=%v err=%v", got, found, err)
	}
}

func TestInstallHermesIntegrationRejectsSymlinkedEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated Windows privileges")
	}
	home := t.TempDir()
	target := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(target, []byte("OPENROUTER_API_KEY=key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, ".env")); err != nil {
		t.Fatal(err)
	}
	if err := installHermesIntegration(home, testConnectionManifest(home)); err == nil || !strings.Contains(err.Error(), "symlinked") {
		t.Fatalf("error %v", err)
	}
}

func TestDiscoverHermesHomeUsesExplicitEnvironment(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profile")
	t.Setenv("HERMES_HOME", home)
	got, found, err := discoverHermesHome()
	if err != nil || !found || got != home {
		t.Fatalf("home=%q found=%v err=%v", got, found, err)
	}
}
