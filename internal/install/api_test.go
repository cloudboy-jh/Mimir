package install

import (
	"regexp"
	"testing"
)

func TestEmbeddedWorkerIdentity(t *testing.T) {
	identity, err := EmbeddedWorkerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if identity.Version == "" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(identity.SHA256) {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestArtifactsReadyOwnsReadinessPolicy(t *testing.T) {
	report := ArtifactReport{Artifacts: []ArtifactResult{
		{Harness: "opencode", Source: "plugins/opencode/mimir.ts", Status: ArtifactCurrent},
		{Harness: "opencode", Source: "skills/mimir-use/SKILL.md", Status: ArtifactUpdated},
		{Harness: "cursor", Source: "plugins/cursor/hooks.json", Status: ArtifactInstalled},
		{Harness: "cursor", Source: MCPSource("cursor"), Status: ArtifactConflict},
		{Harness: "hermes", Source: "plugins/hermes/plugin.yaml", Status: ArtifactConflict},
	}}
	if !ArtifactsReady(report, "opencode") || !ArtifactsReady(report, "cursor") {
		t.Fatal("usable harness artifacts were not ready")
	}
	if ArtifactsReady(report, "hermes") || ArtifactsReady(report, "pi") {
		t.Fatal("conflicting or missing harness artifacts were ready")
	}
}
