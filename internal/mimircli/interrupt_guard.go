package mimircli

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
)

// interruptGuard preserves operation cancellation without owning the terminal.
// Before a command enters its protected commit phase, Ctrl+C cancels its
// context. During commit, interrupts are ignored until receipt and managed-file
// reconciliation finish.
type interruptGuard struct {
	ctx        context.Context
	cancel     context.CancelFunc
	interrupts chan os.Signal
	done       chan struct{}
	committed  atomic.Bool
	stopOnce   sync.Once
}

func startInterruptGuard(parent context.Context) *interruptGuard {
	ctx, cancel := context.WithCancel(parent)
	guard := &interruptGuard{
		ctx:        ctx,
		cancel:     cancel,
		interrupts: make(chan os.Signal, 1),
		done:       make(chan struct{}),
	}
	signal.Notify(guard.interrupts, os.Interrupt)
	go func() {
		select {
		case <-guard.interrupts:
			if !guard.committed.Load() {
				guard.cancel()
			}
		case <-guard.done:
		}
	}()
	return guard
}

func (g *interruptGuard) Context() context.Context { return g.ctx }

func (g *interruptGuard) Commit() { g.committed.Store(true) }

func (g *interruptGuard) Stop() {
	if g == nil {
		return
	}
	g.stopOnce.Do(func() {
		signal.Stop(g.interrupts)
		close(g.done)
		g.cancel()
	})
}
