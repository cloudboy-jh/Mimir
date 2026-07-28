package mimircli

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/sessions"
	"github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
	receiptui "github.com/cloudboy-jh/mimir/internal/ui/receipts"
	sessionui "github.com/cloudboy-jh/mimir/internal/ui/sessions"
)

func cmdList(ctx context.Context, args []string, out io.Writer) error {
	return cmdListIO(ctx, args, nil, out)
}

func cmdListIO(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	repo, outcome, limit, jsonOutput := "", "", 20, false
	interactive := "auto"
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			jsonOutput = true
		case args[i] == "--interactive":
			interactive = "always"
		case args[i] == "--no-interactive":
			interactive = "never"
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
			return fmt.Errorf("usage: mimir list [--repo name] [--outcome landed|discarded|abandoned|unresolved] [--limit 20] [--interactive|--no-interactive] [--json]")
		}
	}
	if outcome != "" && !canonicalOutcome(outcome) {
		return fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", outcome)
	}
	if jsonOutput {
		if interactive == "always" {
			return fmt.Errorf("--json and --interactive cannot be used together")
		}
		data, err := currentSessionService().FetchReceiptsJSON(ctx, repo, outcome, limit)
		if err != nil {
			return err
		}
		return printRemoteData(out, data)
	}
	interactiveTerminal := appframe.Interactive(in, out)
	if interactive == "always" && !interactiveTerminal {
		return fmt.Errorf("interactive list requires terminal input and output")
	}
	receipts, err := currentSessionService().FetchReceipts(ctx, repo, outcome)
	if err != nil {
		return err
	}
	if interactive == "always" || (interactive == "auto" && interactiveTerminal) {
		input, inputOK := in.(*os.File)
		output, outputOK := out.(*os.File)
		if !inputOK || !outputOK {
			return renderReceipts(out, receipts, limit)
		}
		return runSessionBrowser(ctx, input, output, receipts, repo, outcome, limit)
	}
	return renderReceipts(out, receipts, limit)
}

func runSessionBrowser(ctx context.Context, in, out *os.File, receipts []sessions.Receipt, repo, outcome string, limit int) error {
	filters := make([]string, 0, 2)
	if repo != "" {
		filters = append(filters, "repo="+repo)
	}
	if outcome != "" {
		filters = append(filters, "outcome="+outcome)
	}
	pointer, _ := loadPointer()
	toItems := func(values []sessions.Receipt) []sessionui.BrowserSession {
		return sessionui.Items(values, pointer.URL, limit)
	}
	browser := sessionui.NewSessionBrowser(sessionui.SessionBrowserOptions{
		Out: out, Items: toItems(receipts), Filters: strings.Join(filters, " · "),
		Refresh: func(ctx context.Context) ([]sessionui.BrowserSession, error) {
			values, err := currentSessionService().FetchReceipts(ctx, repo, outcome)
			if err != nil {
				return nil, err
			}
			return toItems(values), nil
		},
		Open: openBrowser,
		Copy: func(value string) error {
			_, err := fmt.Fprintf(out, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(value)))
			return err
		},
	})
	return bentotui.Run(ctx, in, out, browser)
}

func renderReceipts(out io.Writer, receipts []sessions.Receipt, limit int) error {
	return receiptui.Render(out, receipts, limit)
}
