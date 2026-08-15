//go:build !windows

package selector

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func terminalAvailable(in, out *os.File) bool {
	for _, file := range []*os.File{in, out} {
		info, err := file.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	check := exec.Command("stty", "-g")
	check.Stdin = in
	return check.Run() == nil
}

func prepareTerminal(in, _ *os.File) (func(), error) {
	readMode := exec.Command("stty", "-g")
	readMode.Stdin = in
	var saved bytes.Buffer
	readMode.Stdout = &saved
	if err := readMode.Run(); err != nil {
		return nil, fmt.Errorf("reading terminal mode: %w", err)
	}
	mode := strings.TrimSpace(saved.String())
	setRaw := exec.Command("stty", "-echo", "-icanon", "-isig", "min", "1", "time", "0")
	setRaw.Stdin = in
	if err := setRaw.Run(); err != nil {
		return nil, fmt.Errorf("setting terminal mode: %w", err)
	}
	return func() {
		restore := exec.Command("stty", mode)
		restore.Stdin = in
		_ = restore.Run()
	}, nil
}
