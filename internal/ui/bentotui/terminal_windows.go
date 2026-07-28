//go:build windows

package bentotui

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	enableEchoInput             = 0x0004
	enableLineInput             = 0x0002
	enableVirtualTerminalInput  = 0x0200
	enableVirtualTerminalOutput = 0x0004
)

type terminalState struct {
	in, out       *os.File
	inputMode     uint32
	outputMode    uint32
	inputModeSet  bool
	outputModeSet bool
}

func enterRawMode(in, out *os.File) (terminalState, error) {
	getMode := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleMode")
	setMode := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")
	state := terminalState{in: in, out: out}
	if ok, _, _ := getMode.Call(in.Fd(), uintptr(unsafe.Pointer(&state.inputMode))); ok == 0 {
		return state, fmt.Errorf("reading console input mode")
	}
	state.inputModeSet = true
	if ok, _, _ := getMode.Call(out.Fd(), uintptr(unsafe.Pointer(&state.outputMode))); ok == 0 {
		return state, fmt.Errorf("reading console output mode")
	}
	state.outputModeSet = true
	inputMode := (state.inputMode &^ (enableEchoInput | enableLineInput | 0x0001)) | enableVirtualTerminalInput
	if ok, _, err := setMode.Call(in.Fd(), uintptr(inputMode)); ok == 0 {
		return state, fmt.Errorf("enabling console input: %v", err)
	}
	if ok, _, err := setMode.Call(out.Fd(), uintptr(state.outputMode|enableVirtualTerminalOutput)); ok == 0 {
		state.restore()
		return state, fmt.Errorf("enabling console output: %v", err)
	}
	return state, nil
}

func (s terminalState) restore() {
	setMode := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")
	if s.inputModeSet {
		setMode.Call(s.in.Fd(), uintptr(s.inputMode))
	}
	if s.outputModeSet {
		setMode.Call(s.out.Fd(), uintptr(s.outputMode))
	}
}

func terminalSize(file *os.File) (int, int) {
	type info struct {
		Size, Cursor struct{ X, Y int16 }
		Attributes   uint16
		Window       struct{ Left, Top, Right, Bottom int16 }
		Maximum      struct{ X, Y int16 }
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleScreenBufferInfo")
	var value info
	ok, _, _ := proc.Call(file.Fd(), uintptr(unsafe.Pointer(&value)))
	if ok == 0 {
		return 0, 0
	}
	return int(value.Window.Right-value.Window.Left) + 1, int(value.Window.Bottom-value.Window.Top) + 1
}
