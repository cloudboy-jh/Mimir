package appframe

import (
	"strings"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type Binding struct {
	Key, Label string
}

func Footer(ctx bentotui.Context, left, right []Binding) string {
	return FooterStatus(ctx, left, "", right)
}

// FooterStatus renders up to four contextual actions around a compact status
// segment. Status is chrome, not another key binding, and disappears first on
// constrained terminals.
func FooterStatus(ctx bentotui.Context, left []Binding, status string, right []Binding) string {
	for len(left)+len(right) > 4 {
		if len(left) > 2 {
			left = append([]Binding(nil), left[:len(left)-1]...)
		} else if len(right) > 1 {
			right = append([]Binding(nil), right[1:]...)
		} else {
			left = append([]Binding(nil), left[:len(left)-1]...)
		}
	}
	available := max(1, ctx.Width-4)
	for {
		leftText := renderBindings(ctx, left)
		rightText := renderBindings(ctx, right)
		bindingWidth := bentotui.VisibleWidth(leftText) + bentotui.VisibleWidth(rightText)
		statusWidth := max(0, available-bindingWidth-4)
		statusText := ""
		if statusWidth >= 8 && strings.TrimSpace(status) != "" {
			muted := bentotui.Style{Color: ctx.Theme.Muted, Enabled: ctx.Color}
			statusText = muted.Render(bentotui.Truncate(status, statusWidth))
		}
		used := bindingWidth + bentotui.VisibleWidth(statusText)
		if used+2 <= available {
			leftGap := max(1, (available-used)/2)
			rightGap := max(1, available-used-leftGap)
			return leftText + strings.Repeat(" ", leftGap) + statusText + strings.Repeat(" ", rightGap) + rightText
		}
		if len(left) > 1 {
			left = append([]Binding(nil), left[:len(left)-1]...)
			continue
		}
		if len(right) > 1 {
			right = append([]Binding(nil), right[1:]...)
			continue
		}
		return bentotui.Truncate(leftText+"  "+rightText, available)
	}
}

func renderBindings(ctx bentotui.Context, bindings []Binding) string {
	accent := bentotui.Style{Color: ctx.Theme.Accent, Bold: true, Enabled: ctx.Color}
	muted := bentotui.Style{Color: ctx.Theme.Muted, Enabled: ctx.Color}
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		parts = append(parts, accent.Render(binding.Key)+" "+muted.Render(binding.Label))
	}
	return strings.Join(parts, "   ")
}
