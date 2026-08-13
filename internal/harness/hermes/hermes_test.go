package hermes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/harness"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

const testInstallationID = "0123456789abcdef0123456789abcdef"

func testManifest() harness.ConnectionManifest {
	return harness.ConnectionManifest{OpenAIBaseURL: "https://mimir.example.workers.dev/v1"}
}

func TestInstallPreservesEnvAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".env")
	original := "OPENROUTER_API_KEY=original-key\nOTHER=value\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := testManifest()
	if err := Install(home, manifest.OpenAIBaseURL, testInstallationID); err != nil {
		t.Fatal(err)
	}
	first := mustReadFile(t, path)
	text := string(first)
	for _, want := range []string{"OPENROUTER_API_KEY=original-key", "OTHER=value", "OPENROUTER_BASE_URL=\"https://mimir.example.workers.dev/v1/hermes/" + testInstallationID + "\""} {
		if !strings.Contains(text, want) {
			t.Fatalf("Hermes .env missing %q:\n%s", want, text)
		}
	}
	if strings.LastIndex(text, managedEnd) < strings.LastIndex(text, "OPENROUTER_API_KEY=original-key") {
		t.Fatal("managed values must be last so they override prior dotenv assignments")
	}
	if err := Install(home, manifest.OpenAIBaseURL, testInstallationID); err != nil {
		t.Fatal(err)
	}
	if second := mustReadFile(t, path); !bytes.Equal(first, second) {
		t.Fatal("idempotent Hermes install changed .env bytes")
	}
	if ok, detail := IntegrationMatches(home, manifest.OpenAIBaseURL, testInstallationID); !ok {
		t.Fatalf("integration mismatch: %s", detail)
	}
	if key, err := OpenRouterKey(home); err != nil || key != "original-key" {
		t.Fatalf("key=%q err=%v", key, err)
	}
}

func TestEnableUsesHermesCLI(t *testing.T) {
	var gotHome string
	var gotArgs []string
	service := New()
	service.RunPluginCommand = func(_ context.Context, home string, args ...string) error {
		gotHome, gotArgs = home, append([]string(nil), args...)
		return nil
	}
	home := t.TempDir()
	if err := service.Enable(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	if gotHome != home || strings.Join(gotArgs, " ") != "enable mimir" {
		t.Fatalf("Hermes plugin command home=%q args=%q", gotHome, gotArgs)
	}
}

func TestEnableRemovesLegacyMCPConfigAfterCLI(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	input := "before: true\r\nmcp_servers:\r\n  mimir:\r\n    command: mimir.exe\r\n    args:\r\n      - serve\r\n  keep:\r\n    command: keep.exe\r\nafter: true\r\n"
	if err := os.WriteFile(path, []byte(input), 0o640); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.RunPluginCommand = func(_ context.Context, _ string, _ ...string) error {
		if got := string(mustReadFile(t, path)); got != input {
			t.Fatalf("legacy config was removed before Hermes CLI succeeded: %q", got)
		}
		return nil
	}
	if err := service.Enable(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	got := string(mustReadFile(t, path))
	want := "before: true\r\nmcp_servers:\r\n  keep:\r\n    command: keep.exe\r\nafter: true\r\n"
	if got != want {
		t.Fatalf("config = %q, want %q", got, want)
	}
}

func TestEnablePreservesLegacyConfigWhenCLIEnableFails(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	input := []byte("mcp_servers:\n  mimir:\n    command: mimir\n    args:\n      - serve\n")
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.RunPluginCommand = func(context.Context, string, ...string) error { return fmt.Errorf("failed") }
	if err := service.Enable(context.Background(), home); err == nil {
		t.Fatal("expected enable failure")
	}
	if got := mustReadFile(t, path); !bytes.Equal(got, input) {
		t.Fatalf("legacy config changed after failed enable: %q", got)
	}
}

func TestRemoveLegacyMCPBlockPreservesInlineSiblingAndComments(t *testing.T) {
	input := []byte("mcp_servers:\n  mimir:\n    command: mimir\n    args:\n      - serve\n  # keep this comment\n  keep: {command: keep}\nafter: true\n")
	want := "mcp_servers:\n  # keep this comment\n  keep: {command: keep}\nafter: true\n"
	got, changed := removeLegacyMCPBlock(input)
	if !changed || string(got) != want {
		t.Fatalf("changed=%v config=%q, want %q", changed, got, want)
	}
}

func TestRemoveLegacyMCPBlockRemovesEmptyParent(t *testing.T) {
	input := []byte("before: true\nmcp_servers:\n  mimir:\n    command: mimir\n    args:\n      - serve\nafter: true\n")
	got, changed := removeLegacyMCPBlock(input)
	if !changed || string(got) != "before: true\nafter: true\n" {
		t.Fatalf("changed=%v config=%q", changed, got)
	}
}

func TestRemoveLegacyMCPBlockAcceptsInlineServeArgs(t *testing.T) {
	input := []byte("mcp_servers:\n  mimir:\n    command: mimir\n    args: [\"serve\"]\nafter: true\n")
	got, changed := removeLegacyMCPBlock(input)
	if !changed || string(got) != "after: true\n" {
		t.Fatalf("changed=%v config=%q", changed, got)
	}
}

func TestRemoveLegacyMCPBlockPreservesAmbiguousConfig(t *testing.T) {
	for name, input := range map[string]string{
		"absent":     "other: true\n",
		"duplicate":  "mcp_servers:\n  mimir: {}\nmcp_servers:\n  keep: {}\n",
		"flow":       "mcp_servers: {mimir: {command: mimir}}\n",
		"modified":   "mcp_servers:\n  mimir:\n    command: custom-mimir.exe\n    args:\n      - serve\n",
		"extra args": "mcp_servers:\n  mimir:\n    command: mimir.exe\n    args:\n      - serve\n      - --custom\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, changed := removeLegacyMCPBlock([]byte(input))
			if changed || string(got) != input {
				t.Fatalf("changed=%v config=%q", changed, got)
			}
		})
	}
}

func TestRemoveLegacyMCPBlockPreservesFollowingTopLevelComment(t *testing.T) {
	input := []byte("mcp_servers:\n  mimir:\n    command: mimir\n    args:\n      - serve\n# explanation for next setting\nnext: true\n")
	want := "# explanation for next setting\nnext: true\n"
	got, changed := removeLegacyMCPBlock(input)
	if !changed || string(got) != want {
		t.Fatalf("changed=%v config=%q, want %q", changed, got, want)
	}
}

func TestCleanupLegacySkillRequiresExactKnownTree(t *testing.T) {
	for name, extra := range map[string]bool{"known": false, "extra": true} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			root := filepath.Join(home, "skills", "mimir")
			if err := os.MkdirAll(filepath.Join(root, "references"), 0o700); err != nil {
				t.Fatal(err)
			}
			skill := []byte("legacy skill")
			formats := []byte("legacy formats")
			oldSkill := legacySkillHashes["SKILL.md"]
			oldFormats := legacySkillHashes["references/formats.md"]
			legacySkillHashes["SKILL.md"] = map[string]bool{hashTestBytes(skill): true}
			legacySkillHashes["references/formats.md"] = map[string]bool{hashTestBytes(formats): true}
			t.Cleanup(func() {
				legacySkillHashes["SKILL.md"] = oldSkill
				legacySkillHashes["references/formats.md"] = oldFormats
			})
			if err := os.WriteFile(filepath.Join(root, "SKILL.md"), skill, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "references", "formats.md"), formats, 0o600); err != nil {
				t.Fatal(err)
			}
			if extra {
				if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := cleanupLegacySkill(root); err != nil {
				t.Fatal(err)
			}
			_, err := os.Stat(root)
			if extra && err != nil {
				t.Fatal("modified legacy skill was removed")
			}
			if !extra && !os.IsNotExist(err) {
				t.Fatal("known legacy skill remains")
			}
		})
	}
}

func hashTestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func TestAuthorizeReportsStaleWorker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}))
	defer server.Close()
	err := New().Authorize(context.Background(), mimirapi.Pointer{URL: server.URL, Token: "machine"}, "openrouter")
	if err == nil || !strings.Contains(err.Error(), "run mimir deploy") {
		t.Fatalf("error = %v", err)
	}
}

func TestAuthorizeSendsDigestOnly(t *testing.T) {
	token := "openrouter-secret"
	wantHash := sha256.Sum256([]byte(token))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/integrations/hermes/authorize" || r.Header.Get("Authorization") != "Bearer machine" {
			t.Fatalf("request = %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["token_hash"] != hex.EncodeToString(wantHash[:]) || strings.Contains(fmt.Sprint(body), token) {
			t.Fatalf("authorization body = %#v", body)
		}
		fmt.Fprint(w, `{"authorized":true}`)
	}))
	defer server.Close()
	if err := New().Authorize(context.Background(), mimirapi.Pointer{URL: server.URL, Token: "machine"}, token); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurePreservesAuthorizationOrdering(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("OPENROUTER_API_KEY=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var order []string
	service := New()
	service.Discover = func() (string, bool, error) { order = append(order, "discover"); return home, true, nil }
	service.RunPluginCommand = func(context.Context, string, ...string) error { order = append(order, "enable"); return nil }
	service.Request = func(_ context.Context, _ mimirapi.Pointer, _, _ string, body any) ([]byte, error) {
		order = append(order, "authorize")
		encoded, _ := json.Marshal(body)
		if strings.Contains(string(encoded), "secret") {
			t.Fatalf("raw credential sent: %s", encoded)
		}
		return nil, nil
	}
	if installed, err := service.Configure(context.Background(), mimirapi.Pointer{URL: "https://mimir.test", Token: "machine"}, testManifest(), testInstallationID); err != nil || !installed {
		t.Fatalf("installed=%v err=%v", installed, err)
	}
	order = append(order, "installed")
	if strings.Join(order, ",") != "discover,enable,authorize,installed" {
		t.Fatalf("order = %v", order)
	}
}

func TestInstallRefreshesManagedValues(t *testing.T) {
	home := t.TempDir()
	if err := Install(home, "https://mimir.example/v1", testInstallationID); err != nil {
		t.Fatal(err)
	}
	if err := Install(home, "https://new.example/v1", testInstallationID); err != nil {
		t.Fatal(err)
	}
	text := string(mustReadFile(t, filepath.Join(home, ".env")))
	if strings.Count(text, managedStart) != 1 || !strings.Contains(text, "https://new.example/v1/hermes/"+testInstallationID) {
		t.Fatalf("managed block was not refreshed:\n%s", text)
	}
}

func TestInstallUsesScopedInstallationRoute(t *testing.T) {
	home := t.TempDir()
	if err := Install(home, "https://mimir.example/v1", testInstallationID); err != nil {
		t.Fatal(err)
	}
	data := string(mustReadFile(t, filepath.Join(home, ".env")))
	want := `OPENROUTER_BASE_URL="https://mimir.example/v1/hermes/` + testInstallationID + `"`
	if !strings.Contains(data, want) {
		t.Fatalf("Hermes installer URL missing %q: %s", want, data)
	}
}

func TestUpsertEnvReplacesLegacyManagedRoute(t *testing.T) {
	legacy := []byte(managedStart + "\n" + `OPENROUTER_BASE_URL="https://mimir.example/v1/hermes"` + "\n" + managedEnd + "\n")
	updated, err := UpsertEnv(legacy, "https://mimir.example/v1/hermes/"+testInstallationID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(updated), managedStart) != 1 || strings.Contains(string(updated), `/v1/hermes"`) || !strings.Contains(string(updated), "/v1/hermes/"+testInstallationID) {
		t.Fatalf("legacy route was not replaced: %s", updated)
	}
	if _, status := RemoveManagedEnv(legacy); status != "removed" {
		t.Fatalf("legacy managed route removal status = %q", status)
	}
}

func TestRemoveManagedEnvPreservesContentAndUnsafeBlocks(t *testing.T) {
	original := []byte("OPENROUTER_API_KEY=keep-me\nOTHER=value\n")
	updated, err := UpsertEnv(original, "https://mimir.test/v1/hermes")
	if err != nil {
		t.Fatal(err)
	}
	cleaned, status := RemoveManagedEnv(updated)
	if status != "removed" || !bytes.Equal(cleaned, original) {
		t.Fatalf("status=%q cleaned=%q", status, cleaned)
	}
	for name, input := range map[string]string{
		"modified":  managedStart + "\nOPENROUTER_BASE_URL=https://changed.example/hermes\n" + managedEnd + "\n",
		"malformed": managedStart + "\nOPENROUTER_BASE_URL=\"https://mimir.test/hermes\"\n",
		"duplicate": managedStart + "\nOPENROUTER_BASE_URL=\"https://mimir.test/hermes\"\n" + managedEnd + "\n" + managedStart + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, gotStatus := RemoveManagedEnv([]byte(input))
			if gotStatus != "preserved" || string(got) != input {
				t.Fatalf("status=%q updated=%q", gotStatus, got)
			}
		})
	}
}

func TestUninstallReportsRemovalAndAbsence(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".env")
	original := []byte("OPENROUTER_API_KEY=keep\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Install(home, testManifest().OpenAIBaseURL, testInstallationID); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.Discover = func() (string, bool, error) { return home, true, nil }
	if result := service.Uninstall(); result.State != "removed" || !result.RestartRequired {
		t.Fatalf("result = %#v", result)
	}
	if got := mustReadFile(t, path); !bytes.Equal(got, original) {
		t.Fatalf(".env = %q, want %q", got, original)
	}
	if result := service.Uninstall(); result.State != "absent" {
		t.Fatalf("second result = %#v", result)
	}
}

func TestParseDotenvCredentialSyntax(t *testing.T) {
	for name, input := range map[string]string{
		"single quoted": "OPENROUTER_API_KEY='sk-or-single'\n",
		"interpolated":  "OPENROUTER_KEY=sk-or-expanded\nOPENROUTER_API_KEY=${OPENROUTER_KEY}\n",
		"comment":       "OPENROUTER_API_KEY=sk-or-commented # account key\n",
	} {
		t.Run(name, func(t *testing.T) {
			values, err := ParseDotenv([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"single quoted": "sk-or-single", "interpolated": "sk-or-expanded", "comment": "sk-or-commented"}[name]
			if values["OPENROUTER_API_KEY"] != want {
				t.Fatalf("key=%q want=%q", values["OPENROUTER_API_KEY"], want)
			}
		})
	}
}

func TestDiscoverHomeUsesActiveProfileAndExplicitEnvironment(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "profiles", "coder")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "active_profile"), []byte("coder\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, found, err := ResolveProfileHome(root); err != nil || !found || got != profile {
		t.Fatalf("home=%q found=%v err=%v", got, found, err)
	}
	t.Setenv("HERMES_HOME", profile)
	if got, found, err := DiscoverHome(); err != nil || !found || got != profile {
		t.Fatalf("home=%q found=%v err=%v", got, found, err)
	}
}

func TestInstallRejectsSymlinkedEnv(t *testing.T) {
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
	if err := Install(home, testManifest().OpenAIBaseURL, testInstallationID); err == nil || !strings.Contains(err.Error(), "symlinked") {
		t.Fatalf("error %v", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
