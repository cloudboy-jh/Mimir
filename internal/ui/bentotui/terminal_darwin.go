//go:build darwin

package bentotui

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	tioGetA    = 0x40487413
	tioSetA    = 0x80487414
	tioCGWSize = 0x40087468
)

type terminalState struct {
	file *os.File
	mode syscall.Termios
	set  bool
}

func enterRawMode(in, _ *os.File) (terminalState, error) {
	state := terminalState{file: in}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, in.Fd(), tioGetA, uintptr(unsafe.Pointer(&state.mode))); errno != 0 {
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
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, in.Fd(), tioSetA, uintptr(unsafe.Pointer(&raw))); errno != 0 {
		return state, fmt.Errorf("setting terminal mode: %v", errno)
	}
	return state, nil
}

func (s terminalState) restore() {
	if s.set {
		syscall.Syscall(syscall.SYS_IOCTL, s.file.Fd(), tioSetA, uintptr(unsafe.Pointer(&s.mode)))
	}
}

func terminalSize(file *os.File) (int, int) {
	var size struct{ Row, Col, X, Y uint16 }
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), tioCGWSize, uintptr(unsafe.Pointer(&size))); errno != 0 {
		return 0, 0
	}
	return int(size.Col), int(size.Row)
}

type terminalByteReader struct{ file *os.File }

func newTerminalByteReader(file *os.File) *terminalByteReader { return &terminalByteReader{file: file} }

func (r *terminalByteReader) read(ctx context.Context) (byte, error) {
	fd := int(r.file.Fd())
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		var set syscall.FdSet
		mask := int32(1 << uint(fd%32))
		set.Bits[fd/32] |= mask
		timeout := syscall.Timeval{Usec: 25000}
		err := syscall.Select(fd+1, &set, nil, nil, &timeout)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return 0, err
		}
		if set.Bits[fd/32]&mask == 0 {
			continue
		}
		var buffer [1]byte
		count, err := syscall.Read(fd, buffer[:])
		if err != nil {
			return 0, err
		}
		if count == 1 {
			return buffer[0], nil
		}
	}
}
