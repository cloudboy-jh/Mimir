package mimircli

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestInterruptGuardCancelsBeforeCommit(t *testing.T) {
	guard := startInterruptGuard(context.Background())
	defer guard.Stop()
	guard.interrupts <- os.Interrupt
	select {
	case <-guard.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("interrupt did not cancel the operation")
	}
}

func TestInterruptGuardIgnoresInterruptAfterCommit(t *testing.T) {
	guard := startInterruptGuard(context.Background())
	defer guard.Stop()
	guard.Commit()
	guard.interrupts <- os.Interrupt
	select {
	case <-guard.Context().Done():
		t.Fatal("committed operation was cancelled")
	case <-time.After(25 * time.Millisecond):
	}
}
