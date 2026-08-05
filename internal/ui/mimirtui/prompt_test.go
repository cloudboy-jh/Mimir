package mimirtui

import (
	"strings"
	"testing"
)

func TestSystemPromptDefinesGroundedMemoryAssistant(t *testing.T) {
	for _, requirement := range []string{
		"private memory interface",
		"Use Mimir tools before making factual claims",
		"not a general-purpose coding agent",
		"prepare a scoped handoff",
		"<mimir_ui_context>",
	} {
		if !strings.Contains(SystemPrompt, requirement) {
			t.Fatalf("system prompt missing %q", requirement)
		}
	}
}
