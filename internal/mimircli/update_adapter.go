package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

var runLifecycleUpdate = func(ctx context.Context, check bool) (lifecyclepkg.UpdateReport, error) {
	configureInstall()
	return lifecycleService().Update(ctx, check)
}

func cmdUpdate(ctx context.Context, args []string, out io.Writer) error {
	check, jsonOutput := false, false
	for _, arg := range args {
		switch arg {
		case "--check":
			check = true
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("usage: mimir update [--check] [--json]")
		}
	}
	report, err := runLifecycleUpdate(ctx, check)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	message := ""
	switch report.Binary.Status {
	case "current":
		message = fmt.Sprintf("mimir %s is up to date", report.Binary.Current)
	case "available":
		message = fmt.Sprintf("mimir %s available (current %s)", report.Binary.Latest, report.Binary.Current)
	case "updated":
		message = fmt.Sprintf("updated mimir %s → %s", report.Binary.Current, report.Binary.Latest)
	}
	message += "\n" + installpkg.ArtifactSummary(report.Artifacts)
	if summary := integrationSummary(report.Integrations); strings.TrimSpace(summary) != "" {
		message += "\n" + summary
	}
	_, err = fmt.Fprintln(out, message)
	return err
}
