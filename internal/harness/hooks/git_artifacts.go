package hooks

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/sessionimport"
)

var commitCommand = regexp.MustCompile(`(?:^|[;&|]\s*)git\s+(?:(?:-C\s+(?:"[^"]+"|'[^']+'|\S+)|-c\s+(?:"[^"]+"|'[^']+'|\S+)|--git-dir(?:=\S+|\s+\S+)|--work-tree(?:=\S+|\s+\S+))\s+)*commit(?:\s|$)`)
var gitDirectory = regexp.MustCompile(`(?:^|\s)-C\s+("[^"]+"|'[^']+'|\S+)`)
var leadingDirectory = regexp.MustCompile(`^\s*cd\s+("[^"]+"|'[^']+'|[^\s;&|]+)\s*(?:&&|;)\s*$`)
// Git prints both "[branch SHA] title" and "[branch (root-commit) SHA] title".
var commitConfirmation = regexp.MustCompile(`(?m)^\[[^\]\r\n]{1,200}?\s+(?:\(root-commit\)\s+)?([0-9a-fA-F]{7,40})\](?:\s|$)`)
var commitHint = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
var failedShellExit = regexp.MustCompile(`(?m)(?:^Exit code:|^Process exited with code)\s*[1-9][0-9]*\b`)

func isCommitHook(harness, event string) bool {
	return (harness == "cursor" && event == "afterShellExecution") ||
		(harness != "cursor" && event == "PostToolUse")
}

func (s Service) normalizeCommit(ctx context.Context, harness, session, repo, cwd string, body map[string]any) normalizedInput {
	command, output := "", ""
	switch harness {
	case "claude-code", "codex":
		if stringValue(body["tool_name"]) != "Bash" && !(harness == "claude-code" && stringValue(body["tool_name"]) == "PowerShell") {
			return normalizedInput{}
		}
		input, _ := body["tool_input"].(map[string]any)
		command = stringValue(input["command"])
		if directory := firstString(input, "working_directory", "workdir", "cwd"); directory != "" {
			cwd = resolveToolCWD(cwd, directory)
		}
		if failedResult(body["tool_response"]) || failedShellExit.MatchString(commandOutput(body["tool_response"])) {
			return normalizedInput{}
		}
		output = commandOutput(body["tool_response"])
	case "cursor":
		command = stringValue(body["command"])
		output = commandOutput(body["output"])
		if failedResult(body) || failedResult(body["output"]) || failedShellExit.MatchString(output) {
			return normalizedInput{}
		}
		hasToolCWD := false
		if input, ok := body["tool_input"].(map[string]any); ok {
			if directory := firstString(input, "working_directory", "workdir", "cwd"); directory != "" {
				hasToolCWD = true
				cwd = resolveToolCWD(cwd, directory)
				if cwd == "" {
					return normalizedInput{}
				}
			}
		}
		if stringValue(body["cwd"]) == "" && !hasToolCWD {
			if roots, ok := body["workspace_roots"].([]any); ok && len(roots) > 1 {
				return normalizedInput{}
			}
		}
	}
	location := commitCommand.FindString(command)
	if cwd == "" || location == "" || strings.Contains(location, "--git-dir") || strings.Contains(location, "--work-tree") {
		return normalizedInput{}
	}
	// Only a single explicit directory change before Git can be attributed.
	// Otherwise the hook cwd may name a different repository than Git used.
	prefix := command[:strings.Index(command, location)+strings.Index(location, "git ")]
	if prefix != "" {
		if matches := leadingDirectory.FindStringSubmatch(prefix); len(matches) == 2 {
			cwd = resolveToolCWD(cwd, strings.Trim(matches[1], `"'`))
		} else if strings.TrimSpace(prefix) != "" {
			return normalizedInput{}
		}
	}
	if matches := gitDirectory.FindStringSubmatch(location); len(matches) == 2 {
		cwd = resolveToolCWD(cwd, strings.Trim(matches[1], `"'`))
	}
	if cwd == "" {
		return normalizedInput{}
	}
	match := commitConfirmation.FindStringSubmatch(output)
	if len(match) != 2 {
		return normalizedInput{}
	}
	hint := strings.ToLower(match[1])
	if !commitHint.MatchString(hint) {
		return normalizedInput{}
	}
	// Resolve only the SHA emitted by Git; never treat HEAD or an unrelated
	// commit as evidence of this tool invocation.
	sha := hint
	if len(hint) < 40 {
		cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", hint+"^{commit}")
		cmd.Dir = cwd
		resolved, err := cmd.Output()
		if err != nil {
			return normalizedInput{}
		}
		sha = strings.TrimSpace(string(resolved))
		if len(sha) != 40 || !strings.HasPrefix(sha, hint) {
			return normalizedInput{}
		}
	}
	collector := s.Collector
	if collector == nil {
		collector = sessionimport.CheckoutArtifactCollector{MaxPatchBytes: 4 << 20}
	}
	artifact, err := collector.CollectCommit(ctx, cwd, sha)
	if err != nil || artifact.CommitSHA != sha {
		return normalizedInput{}
	}
	repo = filepath.Base(filepath.Clean(cwd))
	return normalizedInput{deliveries: []Delivery{{
		Kind: "git-artifacts", Harness: harness, SessionID: session, Repo: repo,
		Body: map[string]any{"version": 1, "commits": []sessionimport.GitArtifact{artifact}},
	}}}
}

func resolveToolCWD(cwd, directory string) string {
	if filepath.IsAbs(directory) {
		return filepath.Clean(directory)
	}
	if cwd == "" {
		return ""
	}
	return filepath.Join(cwd, directory)
}

func commandOutput(value any) string {
	switch result := value.(type) {
	case string:
		// Cursor tool_output is a JSON-encoded result; Codex can send plain text.
		var decoded map[string]any
		if len(result) < MaxInputBytes && strings.HasPrefix(strings.TrimSpace(result), "{") && json.Unmarshal([]byte(result), &decoded) == nil {
			return commandOutput(decoded)
		}
		return result
	case map[string]any:
		for _, key := range []string{"stdout", "output", "content"} {
			if text := commandOutput(result[key]); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range result {
			if block, ok := item.(map[string]any); ok {
				if text := stringValue(block["text"]); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func failedResult(value any) bool {
	result, ok := value.(map[string]any)
	if !ok {
		if text, ok := value.(string); ok && strings.HasPrefix(strings.TrimSpace(text), "{") {
			var decoded map[string]any
			if len(text) < MaxInputBytes && json.Unmarshal([]byte(text), &decoded) == nil {
				return failedResult(decoded)
			}
		}
		return false
	}
	for _, key := range []string{"exitCode", "exit_code", "exit_code_int"} {
		if code, exists := result[key]; exists && code != nil {
			n, err := strconv.ParseFloat(stringifyNumber(code), 64)
			if err != nil || n != 0 {
				return true
			}
		}
	}
	return result["success"] == false || result["is_error"] == true
}

func stringifyNumber(value any) string {
	switch n := value.(type) {
	case json.Number:
		return n.String()
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case int:
		return strconv.Itoa(n)
	case string:
		return n
	}
	return "invalid"
}
