//go:build windows

package bentotui

import (
	"context"
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

type consoleInputRecord struct {
	EventType uint16
	_         uint16
	Event     [16]byte
}

type consoleKeyEvent struct {
	KeyDown         int32
	RepeatCount     uint16
	VirtualKeyCode  uint16
	VirtualScanCode uint16
	UnicodeChar     uint16
	ControlKeyState uint32
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

func readTerminalByte(ctx context.Context, file *os.File) (byte, error) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	wait := kernel.NewProc("WaitForSingleObject")
	readInput := kernel.NewProc("ReadConsoleInputW")
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		result, _, err := wait.Call(file.Fd(), 25)
		switch result {
		case 0:
			var record consoleInputRecord
			var read uint32
			ok, _, readErr := readInput.Call(file.Fd(), uintptr(unsafe.Pointer(&record)), 1, uintptr(unsafe.Pointer(&read)))
			if ok == 0 {
				return 0, fmt.Errorf("reading console input: %v", readErr)
			}
			if read != 1 || record.EventType != 0x0001 {
				continue
			}
			key := (*consoleKeyEvent)(unsafe.Pointer(&record.Event[0]))
			if key.KeyDown == 0 {
				continue
			}
			switch key.VirtualKeyCode {
			case 0x26:
				return terminalByteUp, nil
			case 0x28:
				return terminalByteDown, nil
			}
			if key.UnicodeChar > 0 && key.UnicodeChar <= 0xff {
				return byte(key.UnicodeChar), nil
			}
		case 0x102:
			continue
		default:
			return 0, fmt.Errorf("waiting for console input: %v", err)
		}
	}
}
