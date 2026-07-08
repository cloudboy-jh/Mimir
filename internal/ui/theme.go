package ui

import (
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/cloudboy-jh/bentotui/theme"
)

var registerOnce sync.Once

type ChurnTheme struct {
	theme.BaseTheme
}

func RegisterChurnTheme() theme.Theme {
	t := NewChurnTheme()
	registerOnce.Do(func() {
		_ = theme.RegisterTheme("churn", t)
	})
	return t
}

func NewChurnTheme() *ChurnTheme {
	t := &ChurnTheme{}
	t.ThemeName = "churn"

	bg := lipgloss.Color("#11111b")
	surface := lipgloss.Color("#181825")
	panel := lipgloss.Color("#1e1e2e")
	primary := lipgloss.Color("#ff5656")
	secondary := lipgloss.Color("#ff8585")
	text := lipgloss.Color("#f2e9e4")
	muted := lipgloss.Color("#a6adc8")
	success := lipgloss.Color("#a6e3a1")
	info := lipgloss.Color("#8ab4f8")
	warning := lipgloss.Color("#f9e2af")
	errorColor := lipgloss.Color("#f38ba8")

	t.BackgroundColor = bg
	t.BackgroundPanelColor = panel
	t.BackgroundOverlayColor = surface
	t.BackgroundInteractiveColor = surface
	t.CardChromeColor = primary
	t.CardBodyColor = surface
	t.CardFrameFGColor = secondary
	t.CardFocusEdgeColor = primary
	t.TextColor = text
	t.TextMutedColor = muted
	t.TextInverseColor = bg
	t.TextAccentColor = primary
	t.BorderNormalColor = primary
	t.BorderSubtleColor = muted
	t.BorderFocusColor = secondary
	t.SuccessColor = success
	t.WarningColor = warning
	t.ErrorColor = errorColor
	t.InfoColor = info
	t.SelectionBGColor = primary
	t.SelectionFGColor = bg
	t.InputBGColor = surface
	t.InputFGColor = text
	t.InputPlaceholderColor = muted
	t.InputCursorColor = primary
	t.InputBorderColor = primary
	t.BarBGColor = primary
	t.BarFGColor = bg
	t.FooterBGColor = surface
	t.FooterFGColor = text
	t.FooterMutedColor = muted
	t.DialogBGColor = panel
	t.DialogFGColor = text
	t.DialogBorderColor = primary
	t.DialogScrimColor = bg
	t.DiffAddedBGColor = lipgloss.Color("#1f3d2b")
	t.DiffRemovedBGColor = lipgloss.Color("#4a1f2d")
	t.DiffContextBGColor = surface
	t.DiffAddedLineNumBGColor = lipgloss.Color("#244d35")
	t.DiffRemovedLineNumBGColor = lipgloss.Color("#5a2636")
	t.DiffAddedColor = success
	t.DiffRemovedColor = errorColor
	t.DiffLineNumColor = muted
	t.DiffHighlightAddedColor = success
	t.DiffHighlightRemovedColor = errorColor
	t.SyntaxKeywordColor = secondary
	t.SyntaxTypeColor = info
	t.SyntaxFunctionColor = primary
	t.SyntaxVariableColor = text
	t.SyntaxStringColor = success
	t.SyntaxNumberColor = warning
	t.SyntaxCommentColor = muted
	t.SyntaxOperatorColor = secondary
	t.SyntaxPunctuationColor = muted

	return t
}
