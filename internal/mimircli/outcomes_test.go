package mimircli

import "testing"

func TestOverlapsSessionFiles(t *testing.T) {
	if !overlaps([]string{"src/auth/login.go"}, []string{"src/auth/login.go"}) {
		t.Fatal("exact file did not overlap")
	}
	if overlaps([]string{"src/auth/login.go"}, []string{"src/store.go"}) {
		t.Fatal("unrelated files overlapped")
	}
}

func TestDurableBranch(t *testing.T) {
	if !durableBranch([]string{"origin/main"}) {
		t.Fatal("main should be durable")
	}
	if durableBranch([]string{"origin/feature"}) {
		t.Fatal("feature should not be durable")
	}
}
