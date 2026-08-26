package sessionimport

import (
	"context"
	"encoding/json"
	"time"
)

const (
	DefaultMaxSessions      = 1000
	DefaultMaxCommandBytes  = 64 << 20
	DefaultMaxFiles         = 1000
	DefaultMaxFileBytes     = 64 << 20
	DefaultMaxLineBytes     = 4 << 20
	DefaultMaxLines         = 100000
	DefaultMaxMessages      = 128
	DefaultMaxExchangeBytes = 512 << 10
	DefaultMaxGitArtifacts  = 15
	DefaultMaxGitPatchBytes = 256 << 10
)

type Options struct {
	Sources   []string  `json:"sources,omitempty"`
	SourceIDs []string  `json:"source_ids,omitempty"`
	Since     time.Time `json:"since,omitempty"`
	Before    time.Time `json:"before,omitempty"`
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type ToolActivity struct {
	Name   string         `json:"name"`
	Input  map[string]any `json:"input"`
	Status string         `json:"status"`
	Output string         `json:"output,omitempty"`
}

type Exchange struct {
	ExchangeID  string         `json:"exchange_id"`
	Timestamp   time.Time      `json:"-"`
	Provider    string         `json:"provider,omitempty"`
	Model       string         `json:"model"`
	Request     any            `json:"request"`
	Response    any            `json:"response"`
	Tools       []ToolActivity `json:"tool_activity"`
	Usage       Usage          `json:"usage"`
	LatencyMS   int64          `json:"latency_ms"`
	RequestKind string         `json:"request_kind"`
	Title       string         `json:"title,omitempty"`
}

func (e Exchange) MarshalJSON() ([]byte, error) {
	type wire Exchange
	return json.Marshal(struct {
		wire
		Timestamp string `json:"ts"`
	}{wire: wire(e), Timestamp: e.Timestamp.UTC().Format(time.RFC3339Nano)})
}

type Session struct {
	ID                string     `json:"id"`
	SourceID          string     `json:"source_id"`
	Harness           string     `json:"harness"`
	Directory         string     `json:"directory,omitempty"`
	Repo              string     `json:"repo,omitempty"`
	Title             string     `json:"title,omitempty"`
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           time.Time  `json:"ended_at"`
	Exchanges         []Exchange `json:"exchanges"`
	SkippedOpenRouter int        `json:"skipped_openrouter"`
	SkippedInvalid    int        `json:"skipped_invalid"`
}

type Source interface {
	Name() string
	Discover(context.Context) ([]Session, error)
}

type OptionSource interface {
	DiscoverWithOptions(context.Context, Options) ([]Session, error)
}

type SourceReport struct {
	Source string `json:"source"`
	Status string `json:"status"`
	Count  int    `json:"count"`
	Error  string `json:"error,omitempty"`
}

type Discovery struct {
	Sessions []Session      `json:"sessions"`
	Sources  []SourceReport `json:"sources"`
}

type GitArtifactUpload struct {
	ArtifactsFound     int `json:"artifacts_found"`
	ArtifactsSaved     int `json:"artifacts_saved"`
	ArtifactsDuplicate int `json:"artifacts_duplicate"`
}

type SessionReport struct {
	Source                string `json:"source"`
	SourceID              string `json:"source_id,omitempty"`
	SessionID             string `json:"session_id,omitempty"`
	Status                string `json:"status"`
	ExchangesUploaded     int    `json:"exchanges_uploaded"`
	ExchangesSaved        int    `json:"exchanges_saved"`
	ExchangesDuplicate    int    `json:"exchanges_duplicate"`
	ExchangesSkipped      int    `json:"exchanges_skipped"`
	SkippedOpenRouter     int    `json:"skipped_openrouter"`
	SkippedInvalid        int    `json:"skipped_invalid"`
	GitArtifactsFound     int    `json:"git_artifacts_found"`
	GitArtifactsSaved     int    `json:"git_artifacts_saved"`
	GitArtifactsDuplicate int    `json:"git_artifacts_duplicate"`
	GitArtifactError      string `json:"git_artifact_error,omitempty"`
	Error                 string `json:"error,omitempty"`
}

type Report struct {
	SessionsDiscovered    int             `json:"sessions_discovered"`
	SessionsImported      int             `json:"sessions_imported"`
	SessionsPartial       int             `json:"sessions_partial"`
	SessionsSkipped       int             `json:"sessions_skipped"`
	SessionsFailed        int             `json:"sessions_failed"`
	ExchangesUploaded     int             `json:"exchanges_uploaded"`
	ExchangesSaved        int             `json:"exchanges_saved"`
	ExchangesDuplicate    int             `json:"exchanges_duplicate"`
	ExchangesSkipped      int             `json:"exchanges_skipped"`
	SkippedOpenRouter     int             `json:"skipped_openrouter"`
	SkippedInvalid        int             `json:"skipped_invalid"`
	SourcesFailed         int             `json:"sources_failed"`
	GitArtifactsFound     int             `json:"git_artifacts_found"`
	GitArtifactsSaved     int             `json:"git_artifacts_saved"`
	GitArtifactsDuplicate int             `json:"git_artifacts_duplicate"`
	Sessions              []SessionReport `json:"sessions"`
}
