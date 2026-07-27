package install

import (
	"path/filepath"
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
	root := t.TempDir()
	report := ArtifactReport{Artifacts: []ArtifactResult{
		{Path: filepath.Join(root, "plugins", "mimir.ts"), Source: "plugins/opencode/mimir.ts", Status: ArtifactCurrent},
		{Path: filepath.Join(root, "skills", "mimir-use", "SKILL.md"), Source: "skills/mimir-use/SKILL.md", Status: ArtifactUpdated},
		{Path: filepath.Join(t.TempDir(), "plugin.yaml"), Source: "plugins/hermes/plugin.yaml", Status: ArtifactConflict},
	}}
	if !ArtifactsReady(report, root, "plugins/opencode/mimir.ts", "skills/mimir-use/") {
		t.Fatal("current provider artifacts were not ready")
	}
	if ArtifactsReady(report, "", "plugins/hermes/") {
		t.Fatal("conflicting provider artifact was ready")
	}
	if ArtifactsReady(report, root, "plugins/hermes/") {
		t.Fatal("missing provider artifacts were ready")
	}
}
