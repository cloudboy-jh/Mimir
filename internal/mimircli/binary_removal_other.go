//go:build !windows

package mimircli

import "fmt"

func platformLaunchDeferredBinaryRemoval(_ int, path string) error {
	return fmt.Errorf("deferred binary removal is unsupported for %s", path)
}
