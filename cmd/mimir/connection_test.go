package main

import (
	"strings"
	"testing"
)

func TestConnectionManifestContainsNoCredential(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	manifest, err := currentConnectionManifest("https://mimir.example.workers.dev")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.OpenAIBaseURL != "https://mimir.example.workers.dev/v1" || manifest.AnthropicBaseURL != "https://mimir.example.workers.dev" {
		t.Fatalf("manifest %#v", manifest)
	}
	if !strings.HasSuffix(manifest.CredentialFile, "/token") {
		t.Fatalf("credential file %q", manifest.CredentialFile)
	}
}

func TestValidateDeploymentURL(t *testing.T) {
	for _, valid := range []string{"https://mimir.example.workers.dev", "http://127.0.0.1:8787"} {
		if err := validateDeploymentURL(valid); err != nil {
			t.Fatalf("%s: %v", valid, err)
		}
	}
	for _, invalid := range []string{"http://example.com", "https://user:pass@example.com", "not-a-url"} {
		if err := validateDeploymentURL(invalid); err == nil {
			t.Fatalf("expected %s to be rejected", invalid)
		}
	}
}
