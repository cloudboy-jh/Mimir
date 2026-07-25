package opencode

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness"
)

const (
	pluginSource           = "plugins/opencode/mimir.ts"
	setupSkillSourcePrefix = "skills/mimir-setup/"
	useSkillSourcePrefix   = "skills/mimir-use/"
)

func ArtifactSourcePrefixes() []string {
	return []string{pluginSource, setupSkillSourcePrefix, useSkillSourcePrefix}
}

type Service struct {
	LookPath func(string) (string, error)
}

func New() Service { return Service{LookPath: exec.LookPath} }

func (s Service) Configure(command []string) (harness.IntegrationState, error) {
	lookPath := s.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if _, err := lookPath("opencode"); err != nil {
		return harness.IntegrationState{State: "skipped", Detail: "OpenCode is not installed"}, nil
	}
	if err := ValidateCommand(command); err != nil {
		return harness.IntegrationState{State: "failed", Scope: "mcp", Detail: err.Error()}, err
	}
	return harness.IntegrationState{State: "installed", Scope: "mcp", RestartRequired: true, Detail: "managed OpenCode plugin injects Mimir MCP: " + strings.Join(command, " ")}, nil
}

func ValidateCommand(command []string) error {
	if len(command) != 2 || command[1] != "serve" || strings.TrimSpace(command[0]) == "" {
		return fmt.Errorf("Mimir connection manifest has a malformed MCP command")
	}
	info, err := os.Lstat(command[0])
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Mimir MCP executable does not exist: %s", command[0])
	}
	return nil
}
