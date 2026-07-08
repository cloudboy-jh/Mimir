package git

import (
	"context"
	"testing"
)

func TestDetectRejectsNonRepo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := Detect(context.Background(), dir)
	if err != ErrNotRepo {
		t.Fatalf("expected ErrNotRepo, got %v", err)
	}
}
