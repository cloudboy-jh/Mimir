package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type setupProgress struct {
	out     io.Writer
	enabled bool
	mu      sync.Mutex
	done    chan struct{}
	wg      sync.WaitGroup
	running bool
	phases  []string
	phase   int
}

func startSetupProgress(out io.Writer, phases []string) *setupProgress {
	progress := &setupProgress{out: out, phases: phases}
	file, ok := out.(*os.File)
	if !ok || !isTerminal(file) {
		return progress
	}
	progress.enabled = true
	progress.resumeCurrent()
	return progress
}

func (progress *setupProgress) resumeCurrent() {
	if progress == nil || !progress.enabled {
		return
	}
	if progress.phase >= len(progress.phases) {
		return
	}
	label := progress.phases[progress.phase]
	progress.mu.Lock()
	if progress.running {
		progress.mu.Unlock()
		return
	}
	progress.done = make(chan struct{})
	done := progress.done
	progress.running = true
	progress.wg.Add(1)
	progress.mu.Unlock()
	go func() {
		defer progress.wg.Done()
		frames := []byte{'|', '/', '-', '\\'}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; ; i++ {
			fmt.Fprintf(progress.out, "\r\x1b[2K\x1b[38;5;116m%c\x1b[0m %s", frames[i%len(frames)], label)
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (progress *setupProgress) Pause() {
	if progress == nil || !progress.enabled {
		return
	}
	progress.mu.Lock()
	if !progress.running {
		progress.mu.Unlock()
		return
	}
	done := progress.done
	progress.running = false
	progress.mu.Unlock()
	close(done)
	progress.wg.Wait()
	fmt.Fprint(progress.out, "\r\x1b[2K")
}

func (progress *setupProgress) Stop() { progress.Pause() }

func (progress *setupProgress) Resume() { progress.resumeCurrent() }

func (progress *setupProgress) Complete(label string) {
	if progress == nil || !progress.enabled {
		return
	}
	progress.Pause()
	fmt.Fprintf(progress.out, "\x1b[38;5;116m✓\x1b[0m %s\n", label)
	progress.phase++
	progress.resumeCurrent()
}
