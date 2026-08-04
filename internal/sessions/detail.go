package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Detail is the complete session aggregate returned by the machine API. Raw
// retains the original top-level values so callers can consume newer fields.
type Detail struct {
	Session            Session                    `json:"session"`
	Capture            CaptureSummary             `json:"capture"`
	OutcomeEvents      []OutcomeEvent             `json:"outcome_events"`
	SupportingSessions []Session                  `json:"supporting_sessions"`
	Files              []string                   `json:"files"`
	Errors             []string                   `json:"errors"`
	Exchanges          []Exchange                 `json:"exchanges"`
	Raw                map[string]json.RawMessage `json:"-"`
}

type Session struct {
	ID                string  `json:"id"`
	ParentSessionID   *string `json:"parent_session_id"`
	StartedAt         string  `json:"started_at"`
	EndedAt           *string `json:"ended_at"`
	State             string  `json:"state"`
	LastActiveAt      *string `json:"last_active_at"`
	InactiveAt        *string `json:"inactive_at"`
	Harness           *string `json:"harness"`
	Boundary          string  `json:"boundary"`
	Outcome           string  `json:"outcome"`
	OutcomeSource     *string `json:"outcome_src"`
	OutcomeUpdatedAt  *string `json:"outcome_updated_at"`
	OutcomeReason     *string `json:"outcome_reason"`
	Repo              *string `json:"repo"`
	SourceRef         *string `json:"source_ref"`
	ModelPrimary      *string `json:"model_primary"`
	RequestCount      int     `json:"request_count"`
	TokensIn          int     `json:"tokens_in"`
	TokensOut         int     `json:"tokens_out"`
	Title             *string `json:"title"`
	TitleSource       *string `json:"title_source"`
	TitleUpdatedAt    *string `json:"title_updated_at"`
	DisplayTitle      *string `json:"display_title"`
	Intent            *string `json:"intent"`
	ChildSessionCount int     `json:"child_session_count"`
}

type CaptureSummary struct {
	Status           string  `json:"status"`
	SavedExchanges   int     `json:"saved_exchanges"`
	FailedExchanges  int     `json:"failed_exchanges"`
	PendingExchanges int     `json:"pending_exchanges"`
	LastSavedAt      *string `json:"last_saved_at"`
}

type OutcomeEvent struct {
	ID           string  `json:"id"`
	Outcome      string  `json:"outcome"`
	Source       string  `json:"source"`
	Reason       *string `json:"reason"`
	EvidenceJSON *string `json:"evidence_json"`
	CreatedAt    string  `json:"created_at"`
}

type Exchange struct {
	ID               string  `json:"id"`
	SessionID        string  `json:"session_id"`
	Timestamp        string  `json:"ts"`
	Model            string  `json:"model"`
	Provider         *string `json:"provider"`
	FinishReason     *string `json:"finish_reason"`
	Endpoint         string  `json:"endpoint"`
	LatencyMS        int     `json:"latency_ms"`
	Repo             *string `json:"repo"`
	Harness          *string `json:"harness"`
	AccessTokenLabel string  `json:"access_token_label"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	R2Key            string  `json:"r2_key"`
	RequestExcerpt   string  `json:"request_excerpt"`
	ResponseExcerpt  string  `json:"response_excerpt"`
	CaptureStatus    string  `json:"capture_status"`
	CaptureReason    *string `json:"capture_reason"`
	FailureCode      *string `json:"failure_code"`
}

func (s Service) Get(ctx context.Context, id string) (Detail, error) {
	data, err := s.API.Request(ctx, "GET", "/sessions/"+url.PathEscape(id), nil)
	if err != nil {
		return Detail{}, err
	}
	var detail Detail
	if err := json.Unmarshal(data, &detail); err != nil {
		return Detail{}, fmt.Errorf("decoding session detail: %w", err)
	}
	if err := json.Unmarshal(data, &detail.Raw); err != nil {
		return Detail{}, fmt.Errorf("decoding raw session detail: %w", err)
	}
	return detail, nil
}
