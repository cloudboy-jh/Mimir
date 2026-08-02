package search

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type requesterFunc func(context.Context, string, string, any) ([]byte, error)

func (f requesterFunc) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	return f(ctx, method, path, body)
}

func TestSearchWithOptionsReturnsTypedResultsAndSendsFilters(t *testing.T) {
	service := New(requesterFunc(func(_ context.Context, method, path string, body any) ([]byte, error) {
		if method != "POST" || path != "/search" {
			t.Fatalf("request %s %s", method, path)
		}
		got := body.(map[string]any)
		filters := got["filters"].(map[string]string)
		if got["query"] != "retry policy" || got["budget"] != 1200 || filters["repo"] != "mimir" || filters["outcome"] != "landed" {
			t.Fatalf("body = %#v", got)
		}
		types := got["types"].([]string)
		if len(types) != 2 || types[0] != "intent" || types[1] != "errors" {
			t.Fatalf("types = %#v", types)
		}
		return []byte(`{"query":"retry policy","budget":1200,"matches":[{"session_id":"session-1","started_at":"2026-07-31T10:00:00Z","outcome":"landed","repo":"mimir","model_primary":null,"exchange_id":"exchange-1","request_excerpt":"retry","response_excerpt":"policy","r2_key":"log/1"}],"future_sequence":9007199254740993123456789}`), nil
	}))
	service.Dir = t.TempDir()

	result, err := service.SearchWithOptions(context.Background(), SearchOptions{
		Query: "retry policy", Types: []string{"intent", "errors"}, Budget: 1200, Repo: "mimir", Outcome: "landed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Query != "retry policy" || result.Budget != 1200 || len(result.Matches) != 1 || result.Matches[0].SessionID != "session-1" {
		t.Fatalf("result = %#v", result)
	}
	if string(result.Raw["future_sequence"]) != "9007199254740993123456789" {
		t.Fatalf("raw = %#v", result.Raw)
	}
}

func TestSearchWithOptionsIncludesLocalCodeRecall(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) {
		return []byte(`{"query":"needle","budget":4000,"matches":[]}`), nil
	}))
	service.Dir = t.TempDir()
	result, err := service.SearchWithOptions(context.Background(), SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != nil {
		t.Fatalf("code recall should be absent outside a repository: %#v", result.Code)
	}
}

func TestSearchCompatibilityPreservesUnknownRemoteData(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) {
		return []byte(`{"query":"future","matches":[],"future":{"value":true}}`), nil
	}))
	service.Dir = t.TempDir()
	data, err := service.Search(context.Background(), "future")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	var future struct {
		Value bool `json:"value"`
	}
	if err := json.Unmarshal(result["future"], &future); err != nil {
		t.Fatal(err)
	}
	if !future.Value {
		t.Fatalf("result = %s", data)
	}
}

func TestSearchPreservesRemoteResultsWhenCodeIndexIsUnavailable(t *testing.T) {
	service := New(requesterFunc(func(_ context.Context, method, path string, body any) ([]byte, error) {
		if method != "POST" || path != "/search" || body.(map[string]any)["query"] != "retry policy" {
			t.Fatalf("request %s %s %#v", method, path, body)
		}
		return []byte(`{"query":"retry policy","matches":[]}`), nil
	}))
	service.Dir = t.TempDir()
	data, err := service.Search(context.Background(), "retry policy")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"query": "retry policy"`) {
		t.Fatalf("result %s", data)
	}
}
