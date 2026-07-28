package bentotui

type Variant string

const (
	VariantNeutral Variant = "neutral"
	VariantInfo    Variant = "info"
	VariantSuccess Variant = "success"
	VariantWarning Variant = "warning"
	VariantDanger  Variant = "danger"
)

func Badge(theme Theme, enabled bool, text string, variant Variant) string {
	color := theme.Muted
	switch variant {
	case VariantInfo:
		color = theme.Info
	case VariantSuccess:
		color = theme.Success
	case VariantWarning:
		color = theme.Warning
	case VariantDanger:
		color = theme.Error
	}
	return Style{Color: color, Bold: true, Enabled: enabled}.Render("[" + text + "]")
}
