//go:build !windows && !linux && !darwin

package bentotui

import (
	"fmt"
	"os"
)

type terminalState struct{}

func enterRawMode(_, _ *os.File) (terminalState, error) {
	return terminalState{}, fmt.Errorf("interactive terminal is unsupported on this platform")
}

func (terminalState) restore()         {}
func terminalSize(*os.File) (int, int) { return 0, 0 }
