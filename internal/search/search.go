package search

import (
	"context"
	"encoding/json"

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

func New(api Requester) Service {
	return Service{API: api, Dir: ".", Budget: 4000}
}

func (s Service) Search(ctx context.Context, query string) ([]byte, error) {
	remote, err := s.API.Request(ctx, "POST", "/search", map[string]any{"query": query})
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(remote, &result); err != nil {
		return nil, err
	}
	dir, budget := s.Dir, s.Budget
	if dir == "" {
		dir = "."
	}
	if budget <= 0 {
		budget = 4000
	}
	if code, err := codeindex.Recall(ctx, codeindex.RecallOptions{Dir: dir, Query: query, Budget: budget}); err == nil {
		result["code"] = code
	}
	return json.MarshalIndent(result, "", "  ")
}
