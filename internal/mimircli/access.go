package mimircli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	cliui "github.com/cloudboy-jh/mimir/internal/ui"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

const dashboardAccessAppName = deployment.DashboardAccessAppName

const accessTokenHint = `Dashboard Access automation uses a Cloudflare API token with exactly:
  Account → Access: Apps and Policies → Edit
  Account → Access: Organizations, Identity Providers, and Groups → Read
Create one at https://dash.cloudflare.com/profile/api-tokens (account-scoped, no zones).
`

func accessChecklist(workerURL string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(workerURL, "/"), "https://"), "http://")
	domains := deployment.DashboardAccessDomains(host)
	return fmt.Sprintf(`Dashboard Access is not configured (no Cloudflare API token given).
Recommended: mimir access --token <cloudflare-api-token>  (does all of this)

Manual steps in Zero Trust → Access → Applications:
  1. Add a self-hosted application
       Name: %s
       Destinations: %s and %s
       (both are required; Access paths are exact matches)
  2. Add an Allow policy for your email

Machine API routes (/v1, /sessions, ...) stay outside Access; the Worker
authenticates them with bearer tokens.`, dashboardAccessAppName, domains[0], domains[1])
}

func renderAccessChecklist(out io.Writer, workerURL string) string {
	render := cliui.New(out)
	return render.Callout(bentotui.ToneWarn, "Dashboard Access needs configuration", accessChecklist(workerURL))
}

func renderAccessTokenHint(out io.Writer) string {
	render := cliui.New(out)
	return render.Callout(bentotui.ToneInfo, "Cloudflare API token", strings.TrimSpace(accessTokenHint))
}

func cmdAccess(ctx context.Context, args []string, ioctx IO) error {
	var token, email, aud, teamDomain string
	jsonOut := false
	for i := 0; i < len(args); i++ {
		if args[i] == "--json" {
			jsonOut = true
			continue
		}
		if i+1 >= len(args) {
			return fmt.Errorf("%s requires a value", args[i])
		}
		switch args[i] {
		case "--token":
			token = args[i+1]
		case "--email":
			email = args[i+1]
		case "--aud":
			aud = args[i+1]
		case "--team-domain":
			teamDomain = args[i+1]
		default:
			return fmt.Errorf("unknown access option %q", args[i])
		}
		i++
	}
	if (aud == "") != (teamDomain == "") {
		return fmt.Errorf("--aud and --team-domain must be used together")
	}
	pointer, err := loadPointer()
	if err != nil {
		return fmt.Errorf("Mimir is not connected; run mimir setup or mimir login first")
	}
	url := strings.TrimRight(pointer.URL, "/")
	if aud == "" {
		if token == "" {
			token = strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
		}
		if token == "" && !jsonOut {
			fmt.Fprintln(ioctx.Out, renderAccessTokenHint(ioctx.Out))
			token, err = promptSecret(ioctx, "Cloudflare API token (Enter to print manual steps): ")
			if err != nil {
				return err
			}
		}
		if token == "" {
			if jsonOut {
				host := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
				result := map[string]any{
					"state": "manual", "worker_url": url,
					"application_name": dashboardAccessAppName,
					"destinations":     deployment.DashboardAccessDomains(host),
					"action":           "configure a Cloudflare Access self-hosted application and an Allow policy, or rerun with --token",
				}
				return writeSetupResult(ioctx.Out, true, result, "")
			}
			_, err := fmt.Fprintln(ioctx.Out, renderAccessChecklist(ioctx.Out, url))
			return err
		}
		if email == "" {
			email = strings.TrimSpace(os.Getenv("MIMIR_ACCESS_EMAIL"))
		}
		if email == "" && !jsonOut {
			email, err = promptValue(ioctx, "Email allowed into the dashboard: ")
			if err != nil {
				return err
			}
		}
	}
	opts := deployment.AccessOptions{Options: deployment.DefaultOptions(), URL: url, Token: token, Email: email, Aud: aud, TeamDomain: teamDomain}
	opts.Noninteractive = jsonOut
	outcome, err := deployment.NewService(httpClient).ConfigureAccess(ctx, opts, deployment.Hooks{
		Streams: deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err},
		Login: func(ctx context.Context, dir string) error {
			cloudflareLoginNotice(ioctx.Out)
			return deployment.Wrangler{}.Interactive(ctx, dir, deployment.Streams{In: ioctx.In, Out: ioctx.Out, Err: ioctx.Err}, "login")
		},
	})
	if err != nil {
		return err
	}
	if outcome.State != "configured" {
		if jsonOut {
			return writeSetupResult(ioctx.Out, true, map[string]any{
				"state": outcome.State, "policy": outcome.Policy, "aud": outcome.Aud, "team_domain": outcome.TeamDomain,
				"action": "provide --email for an exact dashboard Allow policy, then rerun mimir access",
			}, "")
		}
		_, err := fmt.Fprintln(ioctx.Out, renderAccessChecklist(ioctx.Out, url))
		return err
	}
	result := map[string]any{"state": "configured", "aud": outcome.Aud, "team_domain": outcome.TeamDomain}
	render := cliui.New(ioctx.Out)
	return writeSetupResult(ioctx.Out, jsonOut, result, render.Card("Dashboard Access configured", bentotui.Field{Label: "Worker", Value: url}, bentotui.Field{Label: "Status", Value: bentotui.Badge(render.Theme, render.Color, "READY", bentotui.VariantSuccess)}))
}

func promptValue(ioctx IO, label string) (string, error) {
	if _, err := fmt.Fprint(ioctx.Out, cliui.New(ioctx.Out).Prompt(label)); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(ioctx.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
