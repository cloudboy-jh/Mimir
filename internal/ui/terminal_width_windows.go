//go:build windows

package ui

import (
	"os"
	"syscall"
	"unsafe"
)

type consoleScreenBufferInfo struct {
	Size              struct{ X, Y int16 }
	CursorPosition    struct{ X, Y int16 }
	Attributes        uint16
	Window            struct{ Left, Top, Right, Bottom int16 }
	MaximumWindowSize struct{ X, Y int16 }
}

func terminalWidth(file *os.File) int {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleScreenBufferInfo")
	var info consoleScreenBufferInfo
	ok, _, _ := proc.Call(file.Fd(), uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return 0
	}
	return int(info.Window.Right-info.Window.Left) + 1
}
