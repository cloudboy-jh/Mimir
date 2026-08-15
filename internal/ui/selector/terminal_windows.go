//go:build windows

package selector

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func terminalAvailable(in, out *os.File) bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	for _, file := range []*os.File{in, out} {
		var mode uint32
		if ok, _, _ := getConsoleMode.Call(file.Fd(), uintptr(unsafe.Pointer(&mode))); ok == 0 {
			return false
		}
	}
	return true
}

func prepareTerminal(in, out *os.File) (func(), error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	var inputMode uint32
	if ok, _, callErr := getConsoleMode.Call(in.Fd(), uintptr(unsafe.Pointer(&inputMode))); ok == 0 {
		return nil, fmt.Errorf("reading console input mode: %w", callErr)
	}
	const (
		enableProcessedInput        = 0x0001
		enableLineInput             = 0x0002
		enableEchoInput             = 0x0004
		enableVirtualTerminalInput  = 0x0200
		enableVirtualTerminalOutput = 0x0004
	)
	rawInputMode := inputMode &^ (enableProcessedInput | enableLineInput | enableEchoInput)
	rawInputMode |= enableVirtualTerminalInput
	if ok, _, callErr := setConsoleMode.Call(in.Fd(), uintptr(rawInputMode)); ok == 0 {
		return nil, fmt.Errorf("setting console input mode: %w", callErr)
	}
	var outputMode uint32
	if ok, _, callErr := getConsoleMode.Call(out.Fd(), uintptr(unsafe.Pointer(&outputMode))); ok == 0 {
		_, _, _ = setConsoleMode.Call(in.Fd(), uintptr(inputMode))
		return nil, fmt.Errorf("reading console output mode: %w", callErr)
	}
	if ok, _, callErr := setConsoleMode.Call(out.Fd(), uintptr(outputMode|enableVirtualTerminalOutput)); ok == 0 {
		_, _, _ = setConsoleMode.Call(in.Fd(), uintptr(inputMode))
		return nil, fmt.Errorf("setting console output mode: %w", callErr)
	}
	return func() {
		_, _, _ = setConsoleMode.Call(in.Fd(), uintptr(inputMode))
		_, _, _ = setConsoleMode.Call(out.Fd(), uintptr(outputMode))
	}, nil
}
