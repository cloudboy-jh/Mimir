//go:build !windows

package mimircli

import (
	"os"
	"os/exec"
)

// disableEcho turns off console echo via stty and returns a restore function.
func disableEcho(file *os.File) (func(), error) {
	disable := exec.Command("stty", "-echo")
	disable.Stdin = file
	if err := disable.Run(); err != nil {
		return nil, err
	}
	return func() {
		restore := exec.Command("stty", "echo")
		restore.Stdin = file
		_ = restore.Run()
	}, nil
}
