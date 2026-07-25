package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

type requesterFunc func(context.Context, string, string, any) ([]byte, error)

func (f requesterFunc) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	return f(ctx, method, path, body)
}

func TestRunReportsArtifactsBeforeMissingConnection(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, nil }))
	service.CheckArtifacts = func() (install.ArtifactReport, error) {
		return install.ArtifactReport{Artifacts: []install.ArtifactResult{{Source: "skills/mimir-use/SKILL.md", Path: "missing", Status: install.ArtifactMissing}}}, nil
	}
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	report := service.Run(context.Background())
	if report.OK || len(report.Checks) != 2 || report.Checks[0].Name != "managed-artifact skills/mimir-use/SKILL.md" || report.Checks[1].Name != "connection" {
		t.Fatalf("report %#v", report)
	}
}

func TestValidateWorkerIdentityRejectsStaleWorker(t *testing.T) {
	if err := ValidateWorkerIdentity([]byte(`{"sessions":0,"log":0}`)); err == nil {
		t.Fatal("legacy Worker was accepted")
	}
	if err := ValidateWorkerIdentity([]byte(`{"service":"mimir","api_version":1,"capabilities":["session_events"]}`)); err == nil {
		t.Fatal("Worker missing required capabilities was accepted")
	}
}
