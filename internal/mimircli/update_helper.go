package mimircli

import (
	"fmt"
	"time"

	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

// applyPendingUpdateHelper runs from a verified copy of the new release in the
// temporary directory. It retries the same receipt/path/hash-validated
// finalizer used by normal CLI startup until the old process releases the
// managed executable, then schedules deletion of this helper after exit.
func applyPendingUpdateHelper() error {
	configureInstall()
	if err := installpkg.ValidateCurrentUpdateHelper(); err != nil {
		return err
	}
	defer func() { _ = installpkg.RemoveCurrentExecutableAfterExit() }()
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		applied, err := installpkg.FinalizePendingUpdate()
		if err != nil {
			return err
		}
		found, err := installpkg.PendingUpdateExists()
		if err != nil {
			return err
		}
		if applied || !found {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting to apply pending update")
}
