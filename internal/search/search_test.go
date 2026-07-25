package search

import (
	"context"
	"strings"
	"testing"
)

type requesterFunc func(context.Context, string, string, any) ([]byte, error)

func (f requesterFunc) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	return f(ctx, method, path, body)
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
