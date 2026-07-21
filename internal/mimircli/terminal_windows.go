//go:build windows

package mimircli

import (
	"os"
	"syscall"
	"unsafe"
)

// disableEcho turns off console echo using the Windows console API and
// returns a restore function. Standard library only.
func disableEcho(file *os.File) (func(), error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	handle := file.Fd()
	var mode uint32
	if r, _, err := getConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return nil, err
	}
	const enableEchoInput = 0x0004
	if r, _, err := setConsoleMode.Call(handle, uintptr(mode&^enableEchoInput)); r == 0 {
		return nil, err
	}
	return func() { _, _, _ = setConsoleMode.Call(handle, uintptr(mode)) }, nil
}
