package app

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/cloudboy-jh/bentotui/registry/bricks/surface"
	"github.com/cloudboy-jh/bentotui/registry/rooms"
	"github.com/cloudboy-jh/bentotui/theme"

	churngit "github.com/cloudboy-jh/churn/internal/git"
	"github.com/cloudboy-jh/churn/internal/ui"
)

type model struct {
	ctx   context.Context
	dir   string
	theme theme.Theme
	repo  churngit.RepoInfo
	err   error
	w     int
	h     int
}

func Run(ctx context.Context, dir string) error {
	t := ui.RegisterChurnTheme()
	m := &model{ctx: ctx, dir: dir, theme: t}
	m.refreshRepo()

	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w = msg.Width
		m.h = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "s":
			m.refreshRepo()
		}
	}
	return m, nil
}

func (m *model) View() tea.View {
	w, h := m.w, m.h
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}

	body := rooms.RenderFunc(func(width, height int) string {
		return m.renderBody(width, height)
	})
	footer := rooms.RenderFunc(func(width, height int) string {
		return m.renderFooter(width)
	})
	screen := rooms.Focus(w, h, body, footer)

	surf := surface.New(w, h)
	surf.Fill(m.theme.Background())
	surf.Draw(0, 0, screen)

	v := tea.NewView(surf.Render())
	v.AltScreen = true
	v.BackgroundColor = m.theme.Background()
	return v
}

func (m *model) refreshRepo() {
	info, err := churngit.Detect(m.ctx, m.dir)
	m.repo = info
	m.err = err
}

func (m *model) renderBody(width, height int) string {
	contentWidth := clamp(width-10, 70, 112)
	modalWidth := clamp(contentWidth-8, 62, 86)

	base := lipgloss.NewStyle().Foreground(m.theme.Text()).Background(m.theme.Background())
	panelBG := m.theme.BackgroundPanel()
	accent := lipgloss.NewStyle().Foreground(m.theme.TextAccent()).Background(panelBG).Bold(true)
	muted := lipgloss.NewStyle().Foreground(m.theme.TextMuted()).Background(panelBG)
	subtle := lipgloss.NewStyle().Foreground(m.theme.TextMuted()).Background(panelBG)
	errorStyle := lipgloss.NewStyle().Foreground(m.theme.Error()).Background(panelBG).Bold(true)
	plain := lipgloss.NewStyle().Foreground(m.theme.Text()).Background(panelBG)
	lineWidth := max(20, modalWidth-6)
	line := func(style lipgloss.Style, text string) string {
		return style.Width(lineWidth).Render(text)
	}
	blank := plain.Width(lineWidth).Render("")

	wordmark := lipgloss.NewStyle().
		Foreground(m.theme.TextAccent()).
		Bold(true).
		Render(asciiWordmark())

	statusText := []string{
		line(accent, "repo memory"),
		line(plain, fmt.Sprintf("root   %s", compactPath(m.repo.Root, modalWidth-15))),
		line(plain, fmt.Sprintf("branch %s", dash(m.repo.Branch))),
		line(plain, fmt.Sprintf("head   %s", dash(shortSHA(m.repo.HeadSHA)))),
		line(plain, fmt.Sprintf("store  %s", m.storeText())),
	}
	if errors.Is(m.err, churngit.ErrNotRepo) {
		statusText = []string{line(errorStyle, "no git repo"), line(plain, "run churn inside"), line(plain, "a repository")}
	} else if m.err != nil {
		statusText = []string{line(errorStyle, "git error"), line(plain, compactPath(m.err.Error(), modalWidth-8))}
	}

	modalLines := []string{
		line(plain, "Hermes remembers you. Churn remembers your code."),
		line(muted, "Durable local code memory for agents that should not wake up cold."),
		blank,
	}
	modalLines = append(modalLines, statusText...)
	modalLines = append(modalLines,
		blank,
		line(accent, "commands"),
		line(plain, fmt.Sprintf("  %-24s %s", "churn index --full", "build memory")),
		line(plain, fmt.Sprintf("  %-24s %s", "churn recall <query>", "pull context")),
		line(plain, fmt.Sprintf("  %-24s %s", "churn serve", "MCP surface")),
		blank,
		line(subtle, "gittrix → churn → glib-code → human approval"),
	)

	modal := lipgloss.NewStyle().
		Width(modalWidth).
		Padding(1, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.BorderNormal()).
		Background(m.theme.BackgroundPanel()).
		Render(lipgloss.JoinVertical(lipgloss.Left, modalLines...))

	ui := lipgloss.JoinVertical(lipgloss.Center, wordmark, "", modal)
	return base.Render(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, ui))
}

func asciiWordmark() string {
	return `  ██████╗██╗  ██╗██╗   ██╗██████╗ ███╗   ██╗
 ██╔════╝██║  ██║██║   ██║██╔══██╗████╗  ██║
 ██║     ███████║██║   ██║██████╔╝██╔██╗ ██║
 ██║     ██╔══██║██║   ██║██╔══██╗██║╚██╗██║
 ╚██████╗██║  ██║╚██████╔╝██║  ██║██║ ╚████║
  ╚═════╝╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═══╝`
}

func (m *model) renderFooter(width int) string {
	style := lipgloss.NewStyle().
		Width(width).
		Foreground(m.theme.FooterFG()).
		Background(m.theme.FooterBG()).
		Padding(0, 1)
	return style.Render("q quit  s refresh  commands: churn index --full · churn recall <query> · churn serve")
}

func (m *model) repoLine() string {
	if m.repo.Root == "" {
		return "repo:        -"
	}
	return fmt.Sprintf("repo:        %s", m.repo.Root)
}

func (m *model) storeLine() string {
	return fmt.Sprintf("store:       %s", m.storeState())
}

func (m *model) storeState() string {
	if m.repo.StoreExists {
		return "present"
	}
	return "missing"
}

func (m *model) storeText() string {
	if m.repo.StoreExists && !m.repo.Stale {
		return "fresh"
	}
	if m.repo.StoreExists {
		return "stale"
	}
	return "missing"
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func compactPath(path string, limit int) string {
	if path == "" {
		return "-"
	}
	if limit < 10 || len(path) <= limit {
		return path
	}
	return "…" + path[len(path)-limit+1:]
}
