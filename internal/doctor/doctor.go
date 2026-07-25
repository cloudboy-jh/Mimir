package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	openintegration "github.com/cloudboy-jh/mimir/internal/harness/opencode"
	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

type Requester interface {
	Request(context.Context, string, string, any) ([]byte, error)
}

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Repair string `json:"repair,omitempty"`
}

type Report struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

type Service struct {
	API            Requester
	CheckArtifacts func() (install.ArtifactReport, error)
	LoadPointer    func() (mimirapi.Pointer, error)
	Lifecycle      lifecycle.Service
	FindOpenCode   func() (string, error)
}

func New(api Requester) Service {
	return Service{
		API: api, CheckArtifacts: install.CheckArtifacts, LoadPointer: mimirapi.LoadPointer,
		Lifecycle: lifecycle.New(), FindOpenCode: func() (string, error) { return exec.LookPath("opencode") },
	}
}

func (s Service) Run(ctx context.Context) Report {
	report := Report{OK: true}
	add := func(name, status, detail, repair string) {
		report.Checks = append(report.Checks, Check{Name: name, Status: status, Detail: detail, Repair: repair})
		if status == "failed" {
			report.OK = false
		}
	}
	artifacts, artifactErr := s.CheckArtifacts()
	if artifactErr != nil {
		add("managed-artifacts", "failed", artifactErr.Error(), "mimir install or mimir update")
	} else {
		for _, artifact := range artifacts.Artifacts {
			status, repair := "ok", ""
			if artifact.Status != install.ArtifactCurrent {
				status = "failed"
				switch artifact.Status {
				case install.ArtifactOutdated, install.ArtifactMissing:
					repair = "mimir install"
				case install.ArtifactConflict, install.ArtifactModified:
					repair = "review the preserved Mimir file; remove or restore it, then run mimir install"
				default:
					repair = "mimir install or mimir update"
				}
			}
			add("managed-artifact "+artifact.Source, status, string(artifact.Status)+" · "+artifact.Path, repair)
		}
	}
	pointer, err := s.LoadPointer()
	if err != nil {
		add("connection", "failed", err.Error(), "mimir login")
		return report
	}
	data, err := s.API.Request(ctx, "GET", "/whoami", nil)
	if err != nil {
		add("worker", "failed", err.Error(), "mimir login")
	} else if err := ValidateWorkerIdentity(data); err != nil {
		add("worker", "failed", err.Error(), "mimir deploy")
	} else {
		add("worker", "ok", pointer.URL, "")
	}
	manifest, err := s.Lifecycle.Manifest(pointer.URL)
	if err != nil {
		add("connection.manifest", "failed", err.Error(), "mimir login")
		return report
	}
	if _, err := s.FindOpenCode(); err != nil {
		add("opencode.mcp", "skipped", "OpenCode is not installed", "")
	} else if err := openintegration.ValidateCommand(manifest.MCPCommand); err != nil {
		add("opencode.mcp", "failed", err.Error(), "mimir install")
	} else {
		add("opencode.mcp", "ok", "managed plugin injects "+strings.Join(manifest.MCPCommand, " ")+" at startup", "")
	}
	for _, check := range s.Lifecycle.Hermes.Doctor(ctx, pointer, manifest) {
		add(check.Name, check.Status, check.Detail, check.Repair)
	}
	return report
}

type workerIdentity struct {
	Service      string   `json:"service"`
	APIVersion   int      `json:"api_version"`
	Capabilities []string `json:"capabilities"`
}

func ValidateWorkerIdentity(data []byte) error {
	var identity workerIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return fmt.Errorf("invalid /whoami response: %w", err)
	}
	if identity.Service != "mimir" || identity.APIVersion < 1 {
		return fmt.Errorf("deployed Worker predates the versioned machine API")
	}
	capabilities := make(map[string]bool, len(identity.Capabilities))
	for _, capability := range identity.Capabilities {
		capabilities[capability] = true
	}
	for _, required := range []string{"hermes_authorization", "session_events", "session_lifecycle"} {
		if !capabilities[required] {
			return fmt.Errorf("deployed Worker lacks required capability %s", required)
		}
	}
	return nil
}
