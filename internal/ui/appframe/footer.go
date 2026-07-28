package appframe

import (
	"strings"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type Binding struct {
	Key, Label string
}

func Footer(ctx bentotui.Context, left, right []Binding) string {
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
		gap := max(1, available-bentotui.VisibleWidth(leftText)-bentotui.VisibleWidth(rightText))
		if bentotui.VisibleWidth(leftText)+gap+bentotui.VisibleWidth(rightText) <= available {
			return leftText + strings.Repeat(" ", gap) + rightText
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
