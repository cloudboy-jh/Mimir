package mimircli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/sessions"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func TestHumanSessionSurfacesFitTerminalWidth(t *testing.T) {
	intent := "Investigate a long capture failure involving a repository and several model requests"
	repo := "github.com/cloudboy-jh/mimir/with/a/long/path"
	model := "openai/gpt-5.6-with-a-long-identifier"
	receipt := sessions.Receipt{
		ID: "session-with-a-long-machine-identifier", StartedAt: "2026-07-27T22:10:00Z",
		Outcome: "landed", Intent: &intent, Repo: &repo, Model: &model,
	}
	receipt.Capture.Status = "partial"
	receipt.Capture.SavedExchanges = 14
	receipt.Capture.FailedExchanges = 2

	search := []byte(`{"query":"long query","matches":[{"session_id":"session-with-a-long-machine-identifier","outcome":"abandoned","repo":"github.com/cloudboy-jh/mimir/with/a/long/path","model_primary":"openai/gpt-5.6-with-a-long-identifier","request_excerpt":"A very long request excerpt that must wrap without crossing the terminal boundary even on narrow displays."}],"code":"\tfunc aVeryLongFunctionNameThatNeedsToWrap() { return }"}`)

	for _, width := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			t.Setenv("COLUMNS", fmt.Sprint(width))
			var output bytes.Buffer
			if err := renderReceipts(&output, []sessions.Receipt{receipt}, 20); err != nil {
				t.Fatal(err)
			}
			if err := renderSearch(&output, search); err != nil {
				t.Fatal(err)
			}
			if !bentotui.FitsWidth(output.String(), width) {
				var over []string
				for _, line := range strings.Split(output.String(), "\n") {
					if bentotui.VisibleWidth(line) > width {
						over = append(over, line)
					}
				}
				t.Fatalf("output exceeds %d columns:\n%s", width, strings.Join(over, "\n"))
			}
		})
	}
}
