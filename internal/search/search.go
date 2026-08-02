package search

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudboy-jh/mimir/internal/codeindex"
)

type Requester interface {
	Request(context.Context, string, string, any) ([]byte, error)
}

type Service struct {
	API    Requester
	Dir    string
	Budget int
}

type SearchOptions struct {
	Query   string
	Types   []string
	Budget  int
	Repo    string
	Outcome string
}

type SearchResult struct {
	Query   string                     `json:"query"`
	Budget  int                        `json:"budget"`
	Matches []SearchMatch              `json:"matches"`
	Code    *codeindex.RecallResult    `json:"code,omitempty"`
	Raw     map[string]json.RawMessage `json:"-"`
}

type SearchMatch struct {
	SessionID       string  `json:"session_id"`
	StartedAt       string  `json:"started_at"`
	Outcome         string  `json:"outcome"`
	Repo            *string `json:"repo"`
	ModelPrimary    *string `json:"model_primary"`
	ExchangeID      string  `json:"exchange_id"`
	RequestExcerpt  string  `json:"request_excerpt"`
	ResponseExcerpt string  `json:"response_excerpt"`
	R2Key           string  `json:"r2_key"`
}

func New(api Requester) Service {
	return Service{API: api, Dir: ".", Budget: 4000}
}

func (s Service) Search(ctx context.Context, query string) ([]byte, error) {
	remote, code, err := s.search(ctx, SearchOptions{Query: query}, true)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(remote, &result); err != nil {
		return nil, err
	}
	if code != nil {
		result["code"] = *code
	}
	return json.MarshalIndent(result, "", "  ")
}

func (s Service) SearchWithOptions(ctx context.Context, options SearchOptions) (SearchResult, error) {
	remote, code, err := s.search(ctx, options, false)
	if err != nil {
		return SearchResult{}, err
	}
	var result SearchResult
	if err := json.Unmarshal(remote, &result); err != nil {
		return SearchResult{}, fmt.Errorf("decoding search result: %w", err)
	}
	if err := json.Unmarshal(remote, &result.Raw); err != nil {
		return SearchResult{}, fmt.Errorf("decoding raw search result: %w", err)
	}
	result.Code = code
	return result, nil
}

func (s Service) search(ctx context.Context, options SearchOptions, legacy bool) ([]byte, *codeindex.RecallResult, error) {
	body := map[string]any{"query": options.Query}
	if len(options.Types) > 0 {
		body["types"] = options.Types
	}
	if options.Budget > 0 {
		body["budget"] = options.Budget
	}
	filters := map[string]string{}
	if options.Repo != "" {
		filters["repo"] = options.Repo
	}
	if options.Outcome != "" {
		filters["outcome"] = options.Outcome
	}
	if len(filters) > 0 {
		body["filters"] = filters
	}
	remote, err := s.API.Request(ctx, "POST", "/search", body)
	if err != nil {
		return nil, nil, err
	}
	dir, budget := s.Dir, s.Budget
	if dir == "" {
		dir = "."
	}
	if !legacy && options.Budget > 0 {
		budget = options.Budget
	}
	if budget <= 0 {
		budget = 4000
	}
	if code, err := codeindex.Recall(ctx, codeindex.RecallOptions{Dir: dir, Query: options.Query, Budget: budget}); err == nil {
		return remote, &code, nil
	}
	return remote, nil, nil
}
