//go:build !windows && !linux && !darwin

package ui

import "os"

func terminalWidth(_ *os.File) int { return 0 }
