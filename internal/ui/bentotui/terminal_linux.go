//go:build linux

package bentotui

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

type terminalState struct {
	file *os.File
	mode syscall.Termios
	set  bool
}

func enterRawMode(in, _ *os.File) (terminalState, error) {
	state := terminalState{file: in}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, in.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&state.mode))); errno != 0 {
		return state, fmt.Errorf("reading terminal mode: %v", errno)
	}
	state.set = true
	raw := state.mode
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN], raw.Cc[syscall.VTIME] = 1, 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, in.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&raw))); errno != 0 {
		return state, fmt.Errorf("setting terminal mode: %v", errno)
	}
	return state, nil
}

func (s terminalState) restore() {
	if s.set {
		syscall.Syscall(syscall.SYS_IOCTL, s.file.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&s.mode)))
	}
}

func terminalSize(file *os.File) (int, int) {
	var size struct{ Row, Col, X, Y uint16 }
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(0x5413), uintptr(unsafe.Pointer(&size))); errno != 0 {
		return 0, 0
	}
	return int(size.Col), int(size.Row)
}
