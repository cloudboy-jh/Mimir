package mimircli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/sessions"
	receiptui "github.com/cloudboy-jh/mimir/internal/ui/receipts"
)

func cmdList(ctx context.Context, args []string, out io.Writer) error {
	return cmdListIO(ctx, args, nil, out)
}

func cmdListIO(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	repo, outcome, limit, jsonOutput := "", "", 20, false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			jsonOutput = true
		case args[i] == "--repo" && i+1 < len(args):
			repo = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--repo="):
			repo = strings.TrimPrefix(args[i], "--repo=")
		case args[i] == "--outcome" && i+1 < len(args):
			outcome = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--outcome="):
			outcome = strings.TrimPrefix(args[i], "--outcome=")
		case args[i] == "--limit" && i+1 < len(args):
			value, err := strconv.Atoi(args[i+1])
			if err != nil || value <= 0 {
				return fmt.Errorf("invalid --limit value")
			}
			limit = value
			i++
		case strings.HasPrefix(args[i], "--limit="):
			value, err := strconv.Atoi(strings.TrimPrefix(args[i], "--limit="))
			if err != nil || value <= 0 {
				return fmt.Errorf("invalid --limit value")
			}
			limit = value
		default:
			return fmt.Errorf("usage: mimir list [--repo name] [--outcome landed|discarded|abandoned|unresolved] [--limit 20] [--json]")
		}
	}
	if outcome != "" && !canonicalOutcome(outcome) {
		return fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", outcome)
	}
	if jsonOutput {
		data, err := currentSessionService().FetchReceiptsJSON(ctx, repo, outcome, limit)
		if err != nil {
			return err
		}
		return printRemoteData(out, data)
	}
	receipts, err := currentSessionService().FetchReceipts(ctx, repo, outcome)
	if err != nil {
		return err
	}
	return renderReceipts(out, receipts, limit)
}

func renderReceipts(out io.Writer, receipts []sessions.Receipt, limit int) error {
	return receiptui.Render(out, receipts, limit)
}
