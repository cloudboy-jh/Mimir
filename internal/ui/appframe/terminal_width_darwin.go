//go:build darwin

package appframe

import (
	"os"
	"syscall"
	"unsafe"
)

func terminalWidth(file *os.File) int {
	var size struct{ Row, Col, X, Y uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(0x40087468), uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0
	}
	return int(size.Col)
}
