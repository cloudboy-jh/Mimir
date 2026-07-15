package mimircli

import (
	"context"
	"testing"
)

func TestDetectRejectsNonRepo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := detectRepo(context.Background(), dir)
	if err != errNotRepo {
		t.Fatalf("expected errNotRepo, got %v", err)
	}
}
