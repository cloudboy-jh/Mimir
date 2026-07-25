package mimircli

import (
	"errors"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

const (
	ExitSuccess            = 0
	ExitInvalidInvocation  = 2
	ExitUnauthorized       = 3
	ExitRemoteFailure      = 4
	ExitLocalConflict      = 5
	ExitIncompatibleWorker = 6
)

func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "older than this cli") || strings.Contains(message, "predates the versioned machine api") || strings.Contains(message, "lacks required capability") {
		return ExitIncompatibleWorker
	}
	var apiErr *mimirapi.Error
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == 401 || apiErr.StatusCode == 403 {
			return ExitUnauthorized
		}
		return ExitRemoteFailure
	}
	if strings.Contains(message, "not connected") || strings.Contains(message, "machine token is missing") || strings.Contains(message, "unauthorized") {
		return ExitUnauthorized
	}
	for _, fragment := range []string{"conflict", "modified", "repair required", "action required", "unowned", "symlinked", "non-regular", "package manager", "refusing to update", "refusing to replace"} {
		if strings.Contains(message, fragment) {
			return ExitLocalConflict
		}
	}
	for _, prefix := range []string{"usage:", "unknown ", "invalid ", "unexpected argument", "flag provided but not defined", "--"} {
		if strings.HasPrefix(message, prefix) {
			return ExitInvalidInvocation
		}
	}
	return ExitRemoteFailure
}
