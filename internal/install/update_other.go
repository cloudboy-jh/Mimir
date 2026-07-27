//go:build !windows

package install

import (
	"errors"
	"os"
)

// platformSwap replaces the executable atomically; non-Windows platforms can
// rename over a running binary, so locks are not expected.
func platformSwap(target, staged, _, _ string) error {
	return os.Rename(staged, target)
}

func platformRecoverTarget(target, _ string) error {
	_, err := os.Stat(target)
	return err
}

var siblingMimirProcesses = func(self int, target string) ([]int, error) { return nil, nil }

func stopSiblingMimirProcesses(self int, target string) []int { return nil }

func scheduleDeferredSwap(target string, binary []byte, previousHash, newVersion string) (string, error) {
	return "", errors.New("deferred update is only supported on Windows")
}
