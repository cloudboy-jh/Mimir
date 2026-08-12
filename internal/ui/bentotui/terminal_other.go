//go:build !windows && !linux && !darwin

package bentotui

import (
	"context"
	"fmt"
	"os"
)

type terminalState struct{}

func enterRawMode(_, _ *os.File) (terminalState, error) {
	return terminalState{}, fmt.Errorf("interactive terminal is unsupported on this platform")
}

func (terminalState) restore()         {}
func terminalSize(*os.File) (int, int) { return 0, 0 }

type terminalByteReader struct{}

func newTerminalByteReader(*os.File) *terminalByteReader           { return &terminalByteReader{} }
func (*terminalByteReader) read(ctx context.Context) (byte, error) { return 0, ctx.Err() }
