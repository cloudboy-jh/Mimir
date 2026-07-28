//go:build !windows && !linux && !darwin

package appframe

import "os"

func terminalWidth(_ *os.File) int { return 0 }
