package hooks

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/sessionimport"
)

var commitCommand = regexp.MustCompile(`(?:^|[;&|]\s*)git\s+commit(?:\s|$)`)
var commitConfirmation = regexp.MustCompile(`(?m)^\[[^\]\r\n]{1,200}\s+([0-9a-fA-F]{7,40})\](?:\s|$)`)
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
		if failedResult(body["tool_response"]) || failedShellExit.MatchString(commandOutput(body["tool_response"])) {
			return normalizedInput{}
		}
		output = commandOutput(body["tool_response"])
	case "cursor":
		command = stringValue(body["command"])
		output = commandOutput(body["output"])
		if failedResult(body) {
			return normalizedInput{}
		}
		hasToolCWD := false
		if input, ok := body["tool_input"].(map[string]any); ok {
			if directory := firstString(input, "working_directory", "workdir", "cwd"); directory != "" {
				hasToolCWD = true
				if filepath.IsAbs(directory) {
					cwd = directory
				} else if cwd != "" {
					cwd = filepath.Join(cwd, directory)
				} else {
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
	if cwd == "" || !commitCommand.MatchString(command) {
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
		if code, exists := result[key]; exists && code != nil && code != float64(0) && code != json.Number("0") && code != 0 {
			return true
		}
	}
	return result["success"] == false || result["is_error"] == true
}
