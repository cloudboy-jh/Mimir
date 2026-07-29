package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

func TestAccessChecklistScopesToDashboard(t *testing.T) {
	checklist := accessChecklist("https://mimir.example.workers.dev")
	for _, destination := range []string{"mimir.example.workers.dev/dashboard/auth", "mimir.example.workers.dev/dashboard/api/*", "mimir.example.workers.dev/dashboard/log-objects/*"} {
		if !strings.Contains(checklist, destination) {
			t.Fatalf("checklist is missing %s:\n%s", destination, checklist)
		}
	}
	if strings.Contains(checklist, "leave the path blank") || strings.Contains(checklist, "Bypass") || strings.Contains(checklist, "wrangler deploy") {
		t.Fatalf("checklist carries a broken manual flow:\n%s", checklist)
	}
}

func TestAccessHelpIncludesEmail(t *testing.T) {
	var output bytes.Buffer
	if err := usage(&output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "--token <api-token>") || !strings.Contains(output.String(), "--email") || !strings.Contains(output.String(), "<address>") {
		t.Fatalf("access help does not include --email:\n%s", output.String())
	}
}

func TestAccessJSONWithoutTokenReturnsManualAction(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(mimirapi.Pointer{URL: "https://mimir.example.workers.dev", Token: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := cmdAccess(context.Background(), []string{"--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		State        string   `json:"state"`
		Action       string   `json:"action"`
		Destinations []string `json:"destinations"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != "manual" || result.Action == "" || len(result.Destinations) != 3 {
		t.Fatalf("result = %#v", result)
	}
	if strings.Contains(output.String(), "Manual steps") || strings.Contains(output.String(), "Recommended:") {
		t.Fatalf("JSON mode printed human checklist: %s", output.String())
	}
}
