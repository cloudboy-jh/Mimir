package ui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type BrowserSession struct {
	Title, Outcome, Capture, Started, Repo, Model, ID, DashboardURL string
}

type SessionBrowserOptions struct {
	Out     io.Writer
	Items   []BrowserSession
	Filters string
	Refresh func(context.Context) ([]BrowserSession, error)
	Open    func(context.Context, string) error
	Copy    func(string) error
}

type SessionBrowser struct {
	options   SessionBrowserOptions
	items     []BrowserSession
	visible   []int
	selected  int
	offset    int
	detail    bool
	filtering bool
	query     string
	message   string
	detailTop int
	detailMax int
}

func NewSessionBrowser(options SessionBrowserOptions) *SessionBrowser {
	browser := &SessionBrowser{options: options, items: append([]BrowserSession(nil), options.Items...)}
	browser.applyFilter()
	return browser
}

func (b *SessionBrowser) View(screen bentotui.Screen) string {
	render := New(b.options.Out).WithWidth(screen.Width)
	if b.detail && len(b.visible) > 0 {
		return b.detailView(render, screen)
	}
	return b.listView(render, screen)
}

func (b *SessionBrowser) Handle(ctx context.Context, key bentotui.Key) bool {
	if key.Kind == bentotui.KeyInterrupt {
		return true
	}
	if b.filtering {
		switch key.Kind {
		case bentotui.KeyEscape:
			b.query, b.filtering = "", false
			b.applyFilter()
		case bentotui.KeyEnter:
			b.filtering = false
		case bentotui.KeyBackspace:
			if len(b.query) > 0 {
				b.query = b.query[:len(b.query)-1]
				b.applyFilter()
			}
		case bentotui.KeyRune:
			b.query += string(key.Rune)
			b.applyFilter()
		}
		return false
	}
	if key.Kind == bentotui.KeyEscape {
		if b.detail {
			b.detail = false
		} else if b.query != "" {
			b.query = ""
			b.applyFilter()
		}
		return false
	}
	if key.Kind == bentotui.KeyEnter && !b.detail && len(b.visible) > 0 {
		b.detail = true
		b.detailTop = 0
		b.message = ""
		return false
	}
	if key.Kind == bentotui.KeyUp {
		b.move(-1)
		return false
	}
	if key.Kind == bentotui.KeyDown {
		b.move(1)
		return false
	}
	if key.Kind != bentotui.KeyRune {
		return false
	}
	switch key.Rune {
	case 'q':
		return true
	case 'j':
		b.move(1)
	case 'k':
		b.move(-1)
	case 'g':
		if !b.detail {
			b.selected, b.offset = 0, 0
		}
	case 'G':
		if !b.detail && len(b.visible) > 0 {
			b.selected = len(b.visible) - 1
		}
	case '/':
		if !b.detail {
			b.filtering = true
			b.message = ""
		}
	case 'r':
		if b.options.Refresh != nil {
			items, err := b.options.Refresh(ctx)
			if err != nil {
				b.message = "Refresh failed: " + err.Error()
			} else {
				b.items = append([]BrowserSession(nil), items...)
				b.applyFilter()
				b.message = "Sessions refreshed"
			}
		}
	case 'o':
		if session, ok := b.current(); ok && b.options.Open != nil && session.DashboardURL != "" {
			if err := b.options.Open(ctx, session.DashboardURL); err != nil {
				b.message = "Open failed: " + err.Error()
			} else {
				b.message = "Opened dashboard"
			}
		}
	case 'y':
		if session, ok := b.current(); ok && b.options.Copy != nil {
			if err := b.options.Copy(session.ID); err != nil {
				b.message = "Copy failed: " + err.Error()
			} else {
				b.message = "Session ID copied"
			}
		}
	}
	return false
}

func (b *SessionBrowser) listView(render Renderer, screen bentotui.Screen) string {
	title := fmt.Sprintf("Sessions  %d results", len(b.visible))
	if b.options.Filters != "" {
		title += "  ·  " + b.options.Filters
	}
	blocks := []string{render.Heading(title)}
	if b.filtering || b.query != "" {
		cursor := ""
		if b.filtering {
			cursor = "_"
		}
		blocks = append(blocks, bentotui.Style{Color: render.Theme.Accent, Enabled: render.Color}.Render("/ "+b.query+cursor))
	}
	if len(b.visible) == 0 {
		body := "Captured model traffic will appear here as work sessions."
		if b.query != "" {
			body = "No loaded sessions match the current filter."
		}
		blocks = append(blocks, render.EmptyState("No sessions found", body))
	} else {
		rowsAvailable := max(1, (screen.Height-3-2*(len(blocks)-1))/4)
		b.keepVisible(rowsAvailable)
		end := min(len(b.visible), b.offset+rowsAvailable)
		for position := b.offset; position < end; position++ {
			blocks = append(blocks, b.sessionRow(render, b.items[b.visible[position]], position == b.selected))
		}
	}
	help := "↑/↓ navigate   enter open   / filter   r refresh   q quit"
	if b.message != "" {
		help = b.message
	}
	blocks = append(blocks, bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}.Render(bentotui.Truncate(help, screen.Width)))
	return bentotui.Join("\n\n", blocks...)
}

func (b *SessionBrowser) sessionRow(render Renderer, session BrowserSession, selected bool) string {
	marker := "  "
	if selected {
		marker = bentotui.Style{Color: render.Theme.Accent, Bold: true, Enabled: render.Color}.Render("› ")
	}
	badge := render.OutcomeBadge(session.Outcome)
	prefix := marker + badge + " "
	contentWidth := max(1, render.Width-bentotui.VisibleWidth(prefix))
	stat := bentotui.Truncate(session.Capture, max(1, min(24, contentWidth/3)))
	if contentWidth < 5 {
		stat = ""
	}
	right := bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}.Render(stat)
	titleWidth := contentWidth
	if stat != "" {
		titleWidth = max(1, contentWidth-bentotui.VisibleWidth(stat)-2)
	}
	title := bentotui.Truncate(emptyValue(session.Title, "Untitled session"), titleWidth)
	line := prefix + bentotui.PadRight(title, titleWidth)
	if stat != "" {
		line += "  " + right
	}
	muted := bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}
	metadata := strings.Join([]string{session.Started, emptyValue(session.Repo, "No repository"), emptyValue(session.Model, "unknown model")}, " · ")
	return strings.Join([]string{line, "    " + muted.Render(bentotui.Truncate(metadata, max(1, render.Width-4))), "    " + muted.Render(bentotui.Truncate(session.ID, max(1, render.Width-4)))}, "\n")
}

func (b *SessionBrowser) detailView(render Renderer, screen bentotui.Screen) string {
	session, _ := b.current()
	fields := []bentotui.Field{
		{Label: "Outcome", Value: render.OutcomeBadge(session.Outcome)},
		{Label: "Capture", Value: session.Capture},
		{Label: "Repository", Value: emptyValue(session.Repo, "No repository")},
		{Label: "Model", Value: emptyValue(session.Model, "unknown model")},
		{Label: "Started", Value: session.Started},
		{Label: "Session", Value: session.ID},
	}
	if session.DashboardURL != "" {
		fields = append(fields, bentotui.Field{Label: "Dashboard", Value: session.DashboardURL})
	}
	body := render.KeyValues(emptyValue(session.Title, "Untitled session"), fields...)
	lines := strings.Split(body, "\n")
	capacity := max(1, screen.Height-4)
	b.detailMax = max(0, len(lines)-capacity)
	b.detailTop = min(b.detailTop, b.detailMax)
	lines = lines[b.detailTop:min(len(lines), b.detailTop+capacity)]
	body = strings.Join(lines, "\n")
	help := "esc back   y copy ID   o open dashboard   q quit"
	if b.message != "" {
		help = b.message
	}
	return bentotui.Join("\n\n", bentotui.Truncate(render.Heading("Session detail"), screen.Width), body, bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}.Render(bentotui.Truncate(help, screen.Width)))
}

func (b *SessionBrowser) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(b.query))
	b.visible = b.visible[:0]
	for index, item := range b.items {
		haystack := strings.ToLower(strings.Join([]string{item.Title, item.Outcome, item.Capture, item.Started, item.Repo, item.Model, item.ID}, " "))
		if query == "" || strings.Contains(haystack, query) {
			b.visible = append(b.visible, index)
		}
	}
	if b.selected >= len(b.visible) {
		b.selected = max(0, len(b.visible)-1)
	}
	b.offset = min(b.offset, b.selected)
}

func (b *SessionBrowser) move(delta int) {
	if b.detail {
		b.detailTop = min(b.detailMax, max(0, b.detailTop+delta))
		return
	}
	if len(b.visible) == 0 {
		return
	}
	b.selected = min(len(b.visible)-1, max(0, b.selected+delta))
}

func (b *SessionBrowser) keepVisible(capacity int) {
	if b.selected < b.offset {
		b.offset = b.selected
	}
	if b.selected >= b.offset+capacity {
		b.offset = b.selected - capacity + 1
	}
}

func (b *SessionBrowser) current() (BrowserSession, bool) {
	if len(b.visible) == 0 || b.selected >= len(b.visible) {
		return BrowserSession{}, false
	}
	return b.items[b.visible[b.selected]], true
}

func emptyValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
