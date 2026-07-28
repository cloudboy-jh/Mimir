package bentotui

import "strings"

type Tone string

const (
	ToneNeutral Tone = "neutral"
	ToneInfo    Tone = "info"
	ToneSuccess Tone = "success"
	ToneWarn    Tone = "warn"
	ToneDanger  Tone = "danger"
)

type Row struct {
	Primary, Secondary, RightStat string
	Tone                          Tone
}

func RenderRows(theme Theme, enabled bool, rows []Row) string {
	return renderRows(Context{Color: enabled, Theme: theme}, rows, false)
}

// RenderRowsContext aligns right-hand statistics against the available width
// and wraps primary and secondary text without overflowing it.
func RenderRowsContext(ctx Context, rows []Row) string {
	return renderRows(ctx.normalized(), rows, true)
}

// RenderRowsWithContext is the descriptive alias for RenderRowsContext.
func RenderRowsWithContext(ctx Context, rows []Row) string {
	return RenderRowsContext(ctx, rows)
}

func renderRows(ctx Context, rows []Row, constrained bool) string {
	var out strings.Builder
	for i, row := range rows {
		if i > 0 {
			out.WriteByte('\n')
		}
		marker, variant := "·", VariantNeutral
		switch row.Tone {
		case ToneInfo:
			marker, variant = "i", VariantInfo
		case ToneSuccess:
			marker, variant = "✓", VariantSuccess
		case ToneWarn:
			marker, variant = "!", VariantWarning
		case ToneDanger:
			marker, variant = "×", VariantDanger
		}
		prefix := "  " + Badge(ctx.Theme, ctx.Color, marker, variant) + " "
		muted := Style{Color: ctx.Theme.Muted, Enabled: ctx.Color}
		if !constrained || ctx.Width <= 0 {
			out.WriteString(prefix + row.Primary)
			if row.RightStat != "" {
				out.WriteString("  " + muted.Render(row.RightStat))
			}
			if row.Secondary != "" {
				out.WriteString("\n      " + muted.Render(row.Secondary))
			}
			continue
		}
		contentWidth := max(1, ctx.Width-VisibleWidth(prefix))
		right := Truncate(row.RightStat, max(1, min(contentWidth-3, contentWidth/3)))
		primaryWidth := contentWidth
		if right != "" {
			primaryWidth = max(1, contentWidth-VisibleWidth(right)-2)
		}
		primary := Wrap(row.Primary, primaryWidth)
		out.WriteString(prefix + PadRight(primary[0], primaryWidth))
		if right != "" {
			out.WriteString("  " + muted.Render(right))
		}
		for _, line := range primary[1:] {
			out.WriteString("\n" + strings.Repeat(" ", VisibleWidth(prefix)) + line)
		}
		for _, line := range Wrap(row.Secondary, max(1, ctx.Width-6)) {
			if row.Secondary != "" {
				out.WriteString("\n      " + muted.Render(line))
			}
		}
	}
	return out.String()
}
