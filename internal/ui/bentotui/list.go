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
		out.WriteString("  " + Badge(theme, enabled, marker, variant) + " " + row.Primary)
		if row.RightStat != "" {
			out.WriteString("  " + Style{Color: theme.Muted, Enabled: enabled}.Render(row.RightStat))
		}
		if row.Secondary != "" {
			out.WriteString("\n      " + Style{Color: theme.Muted, Enabled: enabled}.Render(row.Secondary))
		}
	}
	return out.String()
}
