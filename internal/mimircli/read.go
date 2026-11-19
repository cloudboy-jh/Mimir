package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
)

// Read commands call the Worker query API (/api/*). `--json` prints the
// Worker JSON unchanged; human output is a compact static table or block.

type readFlags struct {
	values     map[string]string
	positional []string
	json       bool
}

// parseReadFlags accepts `--name value`, `--name=value` and `--json` for the
// allowed names; everything else is positional.
func parseReadFlags(args []string, usage string, allowed ...string) (readFlags, error) {
	flags := readFlags{values: map[string]string{}}
	known := map[string]bool{}
	for _, name := range allowed {
		known[name] = true
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--json" {
			flags.json = true
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			flags.positional = append(flags.positional, arg)
			continue
		}
		name, value, inline := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if !known[name] {
			return flags, fmt.Errorf("%s", usage)
		}
		if !inline {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return flags, fmt.Errorf("--%s requires a value", name)
			}
			value = args[i+1]
			i++
		}
		flags.values[name] = value
	}
	return flags, nil
}

// query maps CLI flag names to API parameter names (flag "git-status" →
// "git_status", "by" → "group_by").
func (f readFlags) query(rename map[string]string) string {
	query := url.Values{}
	for name, value := range f.values {
		if renamed, ok := rename[name]; ok {
			name = renamed
		}
		query.Set(strings.ReplaceAll(name, "-", "_"), value)
	}
	if len(query) == 0 {
		return ""
	}
	return "?" + query.Encode()
}

func readJSON(ctx context.Context, out io.Writer, path string, jsonOutput bool, target any) (bool, error) {
	data, err := apiRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	if jsonOutput {
		return false, printRemoteData(out, data)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return false, fmt.Errorf("decoding Mimir response: %w", err)
	}
	return true, nil
}

type sessionRow struct {
	ID           string  `json:"id"`
	DisplayTitle string  `json:"display_title"`
	State        string  `json:"state"`
	Harness      *string `json:"harness"`
	Repo         *string `json:"repo"`
	StartedAt    string  `json:"started_at"`
	ActivityAt   string  `json:"activity_at"`
	ModelPrimary *string `json:"model_primary"`
	RequestCount int     `json:"request_count"`
	TokensIn     int     `json:"tokens_in"`
	TokensOut    int     `json:"tokens_out"`
	Children     int     `json:"child_session_count"`
	GitStatus    string  `json:"git_status"`
	CommitCount  int     `json:"commit_count"`
	Goal         *string `json:"goal"`
	Device       *struct {
		Name string `json:"name"`
	} `json:"device"`
	Capture struct {
		Status string `json:"status"`
	} `json:"capture"`
}

type receiptView struct {
	SessionID    string  `json:"session_id"`
	Line         string  `json:"line"`
	DashboardURL *string `json:"dashboard_url"`
}

const sessionsUsage = "usage: mimir sessions [--q text] [--repo name] [--harness id] [--device id] [--state active|inactive] [--git-status none|committed|pushed] [--from date] [--to date] [--limit n] [--cursor c] [--json]"

func cmdSessions(ctx context.Context, args []string, out io.Writer) error {
	flags, err := parseReadFlags(args, sessionsUsage, "q", "repo", "harness", "device", "state", "git-status", "from", "to", "limit", "cursor")
	if err != nil {
		return err
	}
	if len(flags.positional) != 0 {
		return fmt.Errorf("%s", sessionsUsage)
	}
	var page struct {
		Sessions   []sessionRow `json:"sessions"`
		NextCursor *string      `json:"next_cursor"`
	}
	if human, err := readJSON(ctx, out, "/api/sessions"+flags.query(nil), flags.json, &page); !human || err != nil {
		return err
	}
	if len(page.Sessions) == 0 {
		_, err := fmt.Fprintln(out, "No sessions matched.")
		return err
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "ID\tACTIVITY\tSTATE\tGIT\tREQ\tREPO\tHARNESS\tTITLE")
	for _, session := range page.Sessions {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n", session.ID, shortTime(session.ActivityAt), session.State, session.GitStatus,
			session.RequestCount, text(session.Repo), text(session.Harness), clip(session.DisplayTitle, 60))
	}
	if err := table.Flush(); err != nil {
		return err
	}
	return printNextCursor(out, "mimir sessions", page.NextCursor)
}

func cmdSession(ctx context.Context, args []string, out io.Writer) error {
	const usage = "usage: mimir session <id> [--json] | mimir session status <id> [--json] | mimir session end <id> [--json]"
	args, jsonOutput := stripJSONFlag(args)
	switch {
	case len(args) == 1 && !strings.HasPrefix(args[0], "-") && args[0] != "status" && args[0] != "end":
		return printSessionDetail(ctx, out, args[0], jsonOutput)
	case len(args) == 2 && args[0] == "status" && !strings.HasPrefix(args[1], "-"):
		return printReceipt(ctx, out, args[1], jsonOutput, "")
	case len(args) == 2 && args[0] == "end" && !strings.HasPrefix(args[1], "-"):
		if _, err := remoteRequest(ctx, http.MethodPost, "/sessions/"+url.PathEscape(args[1])+"/end", map[string]any{}); err != nil {
			return err
		}
		return printReceipt(ctx, out, args[1], jsonOutput, "Session ended · ")
	}
	return fmt.Errorf("%s", usage)
}

func printReceipt(ctx context.Context, out io.Writer, id string, jsonOutput bool, prefix string) error {
	var receipt receiptView
	if human, err := readJSON(ctx, out, "/api/sessions/"+url.PathEscape(id)+"/receipt", jsonOutput, &receipt); !human || err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, prefix+receipt.Line); err != nil {
		return err
	}
	if receipt.DashboardURL != nil && *receipt.DashboardURL != "" {
		_, err := fmt.Fprintln(out, "Dashboard: "+*receipt.DashboardURL)
		return err
	}
	return nil
}

type commitRow struct {
	SessionID     string  `json:"session_id"`
	CommitSHA     string  `json:"commit_sha"`
	Subject       *string `json:"subject"`
	Ref           *string `json:"ref"`
	RepositoryURL *string `json:"repository_url"`
	RepositoryKey *string `json:"repository_key"`
	CommittedAt   *string `json:"committed_at"`
	AcceptedAt    string  `json:"accepted_at"`
	Pushed        bool    `json:"pushed"`
	Additions     int     `json:"patch_additions"`
	Deletions     int     `json:"patch_deletions"`
	Files         int     `json:"patch_files"`
	Sessions      []struct {
		ID           string `json:"id"`
		DisplayTitle string `json:"display_title"`
	} `json:"sessions"`
}

func printSessionDetail(ctx context.Context, out io.Writer, id string, jsonOutput bool) error {
	var detail struct {
		Session sessionRow  `json:"session"`
		Receipt receiptView `json:"receipt"`
		Summary *struct {
			Status       string   `json:"status"`
			Goal         *string  `json:"goal"`
			Actions      []string `json:"actions"`
			Result       *string  `json:"result"`
			Verification []string `json:"verification"`
			Unresolved   []string `json:"unresolved"`
		} `json:"summary"`
		Files []struct {
			Path  string `json:"path"`
			Count int    `json:"count"`
		} `json:"files"`
		Errors []struct {
			Signature string `json:"signature"`
			Count     int    `json:"count"`
		} `json:"errors"`
		Commits    []commitRow  `json:"commits"`
		Supporting []sessionRow `json:"supporting_sessions"`
	}
	if human, err := readJSON(ctx, out, "/api/sessions/"+url.PathEscape(id), jsonOutput, &detail); !human || err != nil {
		return err
	}
	s := detail.Session
	var b strings.Builder
	fmt.Fprintln(&b, s.DisplayTitle)
	meta := []string{s.ID, s.State}
	for _, value := range []*string{s.Harness, s.Repo, s.ModelPrimary} {
		if value != nil && *value != "" {
			meta = append(meta, *value)
		}
	}
	if s.Device != nil && s.Device.Name != "" {
		meta = append(meta, s.Device.Name)
	}
	fmt.Fprintln(&b, strings.Join(meta, " · "))
	fmt.Fprintf(&b, "Started %s · active %s · %d requests · %d in / %d out tokens · %d sub-sessions\n",
		shortTime(s.StartedAt), shortTime(s.ActivityAt), s.RequestCount, s.TokensIn, s.TokensOut, s.Children)
	fmt.Fprintln(&b, detail.Receipt.Line)
	if detail.Receipt.DashboardURL != nil && *detail.Receipt.DashboardURL != "" {
		fmt.Fprintln(&b, "Dashboard: "+*detail.Receipt.DashboardURL)
	}
	if summary := detail.Summary; summary != nil && summary.Status == "ready" {
		if summary.Goal != nil && *summary.Goal != "" {
			fmt.Fprintln(&b, "\nGoal: "+*summary.Goal)
		}
		writeList(&b, "Actions", summary.Actions)
		if summary.Result != nil && *summary.Result != "" {
			fmt.Fprintln(&b, "Result: "+*summary.Result)
		}
		writeList(&b, "Verification", summary.Verification)
		writeList(&b, "Unresolved", summary.Unresolved)
	} else if summary != nil {
		fmt.Fprintln(&b, "\nSummary: "+summary.Status)
	}
	if len(detail.Commits) > 0 {
		fmt.Fprintln(&b, "\nCommits:")
		for _, commit := range detail.Commits {
			fmt.Fprintf(&b, "  %s  %-9s %s\n", shortSHA(commit.CommitSHA), pushedLabel(commit.Pushed), text(commit.Subject))
		}
	}
	if len(detail.Files) > 0 {
		fmt.Fprintln(&b, "\nFiles:")
		for i, file := range detail.Files {
			if i == 10 {
				fmt.Fprintf(&b, "  … %d more\n", len(detail.Files)-10)
				break
			}
			fmt.Fprintf(&b, "  %s (%d)\n", file.Path, file.Count)
		}
	}
	if len(detail.Errors) > 0 {
		fmt.Fprintln(&b, "\nErrors:")
		for _, failure := range detail.Errors {
			fmt.Fprintf(&b, "  %s (%d)\n", clip(failure.Signature, 100), failure.Count)
		}
	}
	if len(detail.Supporting) > 0 {
		fmt.Fprintln(&b, "\nSupporting sessions:")
		for _, child := range detail.Supporting {
			fmt.Fprintf(&b, "  %s  %s\n", child.ID, clip(child.DisplayTitle, 70))
		}
	}
	_, err := io.WriteString(out, b.String())
	return err
}

func writeList(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintln(b, label+":")
	for _, item := range items {
		fmt.Fprintln(b, "  - "+item)
	}
}

const turnsUsage = "usage: mimir turns <id> [--limit n] [--cursor c] [--order asc|desc] [--json]"

func cmdTurns(ctx context.Context, args []string, out io.Writer) error {
	flags, err := parseReadFlags(args, turnsUsage, "limit", "cursor", "order")
	if err != nil {
		return err
	}
	if len(flags.positional) != 1 {
		return fmt.Errorf("%s", turnsUsage)
	}
	id := flags.positional[0]
	var page struct {
		Turns []struct {
			Timestamp   string  `json:"ts"`
			Model       *string `json:"model"`
			RequestKind string  `json:"request_kind"`
			User        *string `json:"user"`
			Assistant   *string `json:"assistant"`
			Tools       []struct {
				Name  string `json:"name"`
				Error bool   `json:"error"`
			} `json:"tools"`
			Truncated bool `json:"truncated"`
		} `json:"turns"`
		NextCursor *string `json:"next_cursor"`
	}
	if human, err := readJSON(ctx, out, "/api/sessions/"+url.PathEscape(id)+"/turns"+flags.query(nil), flags.json, &page); !human || err != nil {
		return err
	}
	if len(page.Turns) == 0 {
		_, err := fmt.Fprintln(out, "No turns.")
		return err
	}
	var b strings.Builder
	for i, turn := range page.Turns {
		if i > 0 {
			fmt.Fprintln(&b)
		}
		header := "── " + shortTime(turn.Timestamp)
		if turn.Model != nil && *turn.Model != "" {
			header += " · " + *turn.Model
		}
		if turn.RequestKind != "" && turn.RequestKind != "primary" {
			header += " · " + turn.RequestKind
		}
		if turn.Truncated {
			header += " · truncated"
		}
		fmt.Fprintln(&b, header)
		if turn.User != nil && strings.TrimSpace(*turn.User) != "" {
			fmt.Fprintln(&b, "User: "+clip(strings.TrimSpace(*turn.User), 1200))
		}
		if len(turn.Tools) > 0 {
			names := make([]string, 0, len(turn.Tools))
			for _, tool := range turn.Tools {
				if tool.Error {
					names = append(names, tool.Name+" (error)")
				} else {
					names = append(names, tool.Name)
				}
			}
			fmt.Fprintln(&b, "Tools: "+strings.Join(names, ", "))
		}
		if turn.Assistant != nil && strings.TrimSpace(*turn.Assistant) != "" {
			fmt.Fprintln(&b, "Assistant: "+clip(strings.TrimSpace(*turn.Assistant), 1200))
		}
	}
	if _, err := io.WriteString(out, b.String()); err != nil {
		return err
	}
	return printNextCursor(out, "mimir turns "+id, page.NextCursor)
}

const searchUsage = "usage: mimir search <query> [--repo name] [--from date] [--to date] [--limit n] [--json]"

func cmdSearch(ctx context.Context, args []string, out io.Writer) error {
	flags, err := parseReadFlags(args, searchUsage, "repo", "from", "to", "limit")
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(flags.positional, " "))
	if query == "" {
		return fmt.Errorf("%s", searchUsage)
	}
	flags.values["q"] = query
	var response struct {
		Results []struct {
			Session sessionRow `json:"session"`
			Hits    []struct {
				Timestamp string `json:"ts"`
				Snippet   string `json:"snippet"`
			} `json:"hits"`
		} `json:"results"`
	}
	if human, err := readJSON(ctx, out, "/api/search"+flags.query(nil), flags.json, &response); !human || err != nil {
		return err
	}
	if len(response.Results) == 0 {
		_, err := fmt.Fprintln(out, "No matches.")
		return err
	}
	var b strings.Builder
	for i, result := range response.Results {
		if i > 0 {
			fmt.Fprintln(&b)
		}
		s := result.Session
		fmt.Fprintf(&b, "%s  %s\n", s.ID, clip(s.DisplayTitle, 80))
		fmt.Fprintf(&b, "  %s · %s · %s · git %s\n", shortTime(s.ActivityAt), text(s.Repo), text(s.Harness), s.GitStatus)
		for _, hit := range result.Hits {
			fmt.Fprintf(&b, "  %s  %s\n", shortTime(hit.Timestamp), clip(oneLine(hit.Snippet), 160))
		}
	}
	_, err = io.WriteString(out, b.String())
	return err
}

const statsUsage = "usage: mimir stats --by <repo|harness|device|model|day> [--from date] [--to date] [--tz zone] [--json]"

func cmdStats(ctx context.Context, args []string, out io.Writer) error {
	flags, err := parseReadFlags(args, statsUsage, "by", "from", "to", "tz")
	if err != nil {
		return err
	}
	switch flags.values["by"] {
	case "repo", "harness", "device", "model", "day":
	default:
		return fmt.Errorf("%s", statsUsage)
	}
	if len(flags.positional) != 0 {
		return fmt.Errorf("%s", statsUsage)
	}
	if _, ok := flags.values["tz"]; !ok {
		if zone := localTimeZone(); zone != "" {
			flags.values["tz"] = zone
		}
	}
	var response struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Groups []struct {
			Key       string `json:"key"`
			Sessions  int    `json:"sessions"`
			Requests  int    `json:"requests"`
			TokensIn  int    `json:"tokens_in"`
			TokensOut int    `json:"tokens_out"`
			Commits   int    `json:"commits"`
		} `json:"groups"`
	}
	if human, err := readJSON(ctx, out, "/api/stats"+flags.query(map[string]string{"by": "group_by"}), flags.json, &response); !human || err != nil {
		return err
	}
	fmt.Fprintf(out, "%s → %s\n", shortTime(response.From), shortTime(response.To))
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(table, "%s\tSESSIONS\tREQUESTS\tTOKENS IN\tTOKENS OUT\tCOMMITS\n", strings.ToUpper(flags.values["by"]))
	for _, group := range response.Groups {
		fmt.Fprintf(table, "%s\t%d\t%d\t%d\t%d\t%d\n", group.Key, group.Sessions, group.Requests, group.TokensIn, group.TokensOut, group.Commits)
	}
	return table.Flush()
}

const dayUsage = "usage: mimir day [YYYY-MM-DD] [--tz zone] [--json]"

func cmdDay(ctx context.Context, args []string, out io.Writer) error {
	flags, err := parseReadFlags(args, dayUsage, "tz")
	if err != nil {
		return err
	}
	if len(flags.positional) > 1 {
		return fmt.Errorf("%s", dayUsage)
	}
	zone, ok := flags.values["tz"]
	if !ok {
		zone = localTimeZone()
	}
	location := time.UTC
	if zone != "" {
		if location, err = time.LoadLocation(zone); err != nil {
			return fmt.Errorf("invalid --tz %q", zone)
		}
	}
	date := time.Now().In(location).Format(time.DateOnly)
	if len(flags.positional) == 1 {
		date = flags.positional[0]
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			return fmt.Errorf("invalid date %q: want YYYY-MM-DD", date)
		}
	}
	path := "/api/days/" + date
	if zone != "" {
		path += "?" + url.Values{"tz": {zone}}.Encode()
	}
	var day struct {
		Date     string `json:"date"`
		TimeZone string `json:"time_zone"`
		Totals   struct {
			Sessions int `json:"sessions"`
			Requests int `json:"requests"`
			Commits  int `json:"commits"`
			Active   int `json:"active"`
		} `json:"totals"`
		Workstreams []struct {
			Label      string   `json:"label"`
			SessionIDs []string `json:"session_ids"`
			Requests   int      `json:"requests"`
			Git        struct {
				Committed int `json:"committed"`
				Pushed    int `json:"pushed"`
			} `json:"git"`
		} `json:"workstreams"`
		Sessions []struct {
			ID           string  `json:"id"`
			DisplayTitle *string `json:"display_title"`
			State        string  `json:"state"`
			GitStatus    string  `json:"git_status"`
			FirstAt      string  `json:"first_at"`
			Requests     int     `json:"requests"`
		} `json:"sessions"`
	}
	if human, err := readJSON(ctx, out, path, flags.json, &day); !human || err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) · %d sessions · %d requests · %d commits · %d active\n", day.Date, day.TimeZone, day.Totals.Sessions, day.Totals.Requests, day.Totals.Commits, day.Totals.Active)
	byID := map[string]int{}
	for i, session := range day.Sessions {
		byID[session.ID] = i
	}
	dayLocation, err := time.LoadLocation(day.TimeZone)
	if err != nil {
		dayLocation = location
	}
	for _, stream := range day.Workstreams {
		fmt.Fprintf(&b, "\n%s · %d sessions · %d requests · %d committed · %d pushed\n", stream.Label, len(stream.SessionIDs), stream.Requests, stream.Git.Committed, stream.Git.Pushed)
		for _, id := range stream.SessionIDs {
			index, ok := byID[id]
			if !ok {
				continue
			}
			session := day.Sessions[index]
			at := session.FirstAt
			if parsed, err := time.Parse(time.RFC3339Nano, at); err == nil {
				at = parsed.In(dayLocation).Format("15:04")
			}
			title := session.ID
			if session.DisplayTitle != nil && *session.DisplayTitle != "" {
				title = *session.DisplayTitle
			}
			fmt.Fprintf(&b, "  %s  %-8s %-9s %s  %s\n", at, session.State, session.GitStatus, session.ID, clip(title, 60))
		}
	}
	_, err = io.WriteString(out, b.String())
	return err
}

func cmdDevices(ctx context.Context, args []string, out io.Writer) error {
	if !onlyJSONFlag(args) {
		return fmt.Errorf("usage: mimir devices [--json]")
	}
	var response struct {
		Devices []struct {
			ID         string   `json:"id"`
			Name       string   `json:"name"`
			Platform   string   `json:"platform"`
			Arch       string   `json:"arch"`
			LastSeenAt *string  `json:"last_seen_at"`
			RevokedAt  *string  `json:"revoked_at"`
			Sessions   int      `json:"session_count"`
			Harnesses  []string `json:"harnesses"`
			Tailnet    *struct {
				DNSName *string `json:"dns_name"`
				Online  *bool   `json:"online"`
			} `json:"tailnet"`
		} `json:"devices"`
	}
	if human, err := readJSON(ctx, out, "/api/devices", len(args) == 1, &response); !human || err != nil {
		return err
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "ID\tNAME\tPLATFORM\tLAST SEEN\tSESSIONS\tTAILNET\tSTATUS")
	for _, device := range response.Devices {
		tail := "-"
		if device.Tailnet != nil {
			tail = text(device.Tailnet.DNSName)
			if device.Tailnet.Online != nil {
				if *device.Tailnet.Online {
					tail += " (online)"
				} else {
					tail += " (offline)"
				}
			}
		}
		status := "active"
		if device.RevokedAt != nil {
			status = "revoked"
		}
		fmt.Fprintf(table, "%s\t%s\t%s/%s\t%s\t%d\t%s\t%s\n", device.ID, device.Name, device.Platform, device.Arch, shortTime(text(device.LastSeenAt)), device.Sessions, tail, status)
	}
	return table.Flush()
}

const commitsUsage = "usage: mimir commits [--repo name] [--ref ref] [--q text] [--session id] [--from date] [--to date] [--limit n] [--cursor c] [--json]"

func cmdCommits(ctx context.Context, args []string, out io.Writer) error {
	flags, err := parseReadFlags(args, commitsUsage, "repo", "ref", "q", "session", "from", "to", "limit", "cursor")
	if err != nil {
		return err
	}
	if len(flags.positional) != 0 {
		return fmt.Errorf("%s", commitsUsage)
	}
	var page struct {
		Commits    []commitRow `json:"commits"`
		NextCursor *string     `json:"next_cursor"`
	}
	if human, err := readJSON(ctx, out, "/api/commits"+flags.query(nil), flags.json, &page); !human || err != nil {
		return err
	}
	if len(page.Commits) == 0 {
		_, err := fmt.Fprintln(out, "No commits matched.")
		return err
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "SHA\tWHEN\tGIT\tREPO\tREF\tCHANGE\tSUBJECT\tSESSION")
	for _, commit := range page.Commits {
		when := commit.AcceptedAt
		if commit.CommittedAt != nil {
			when = *commit.CommittedAt
		}
		session := commit.SessionID
		if len(commit.Sessions) > 0 {
			session = commit.Sessions[0].ID
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t+%d/-%d\t%s\t%s\n", shortSHA(commit.CommitSHA), shortTime(when), pushedLabel(commit.Pushed),
			repoLabel(commit), text(commit.Ref), commit.Additions, commit.Deletions, clip(text(commit.Subject), 60), session)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	return printNextCursor(out, "mimir commits", page.NextCursor)
}

func repoLabel(commit commitRow) string {
	for _, value := range []*string{commit.RepositoryURL, commit.RepositoryKey} {
		if value != nil && *value != "" {
			return strings.TrimSuffix(filepath.Base(strings.TrimRight(*value, "/")), ".git")
		}
	}
	return "-"
}

func printNextCursor(out io.Writer, command string, cursor *string) error {
	if cursor == nil || *cursor == "" {
		return nil
	}
	_, err := fmt.Fprintf(out, "More: %s --cursor %s\n", command, *cursor)
	return err
}

func pushedLabel(pushed bool) string {
	if pushed {
		return "pushed"
	}
	return "committed"
}

func shortSHA(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// shortTime renders an RFC3339 timestamp in local time; other values pass through.
func shortTime(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		if value == "" {
			return "-"
		}
		return value
	}
	return parsed.Local().Format("2006-01-02 15:04")
}

func text(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "-"
	}
	return *value
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func clip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

// localTimeZone returns the local IANA zone name when it can be determined
// (TZ, or the /etc/localtime link); the Worker defaults to UTC otherwise.
func localTimeZone() string {
	if zone := strings.TrimPrefix(strings.TrimSpace(os.Getenv("TZ")), ":"); zone != "" {
		if _, err := time.LoadLocation(zone); err == nil {
			return zone
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, zone, ok := strings.Cut(target, "zoneinfo/"); ok {
			if _, err := time.LoadLocation(zone); err == nil {
				return zone
			}
		}
	}
	return ""
}
