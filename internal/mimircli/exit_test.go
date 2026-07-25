package mimircli

import (
	"fmt"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

func TestExitCodeClasses(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, ExitSuccess},
		{fmt.Errorf("usage: mimir search <query>"), ExitInvalidInvocation},
		{fmt.Errorf("Mimir is not connected; run mimir setup"), ExitUnauthorized},
		{&mimirapi.Error{StatusCode: 401, Status: "401 Unauthorized"}, ExitUnauthorized},
		{&mimirapi.Error{StatusCode: 500, Status: "500 Internal Server Error"}, ExitRemoteFailure},
		{fmt.Errorf("conflicting managed file"), ExitLocalConflict},
		{fmt.Errorf("refusing to update symlinked executable path"), ExitLocalConflict},
		{fmt.Errorf("target is an unowned executable"), ExitLocalConflict},
		{fmt.Errorf("flag provided but not defined: -bogus"), ExitInvalidInvocation},
		{fmt.Errorf("unknown access option --bogus"), ExitInvalidInvocation},
		{fmt.Errorf("deployed Worker predates the versioned machine API"), ExitIncompatibleWorker},
	}
	for _, test := range tests {
		if got := ExitCode(test.err); got != test.want {
			t.Errorf("ExitCode(%v) = %d, want %d", test.err, got, test.want)
		}
	}
}
