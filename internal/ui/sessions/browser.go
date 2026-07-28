package sessionui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type BrowserSession struct {
	Title, Outcome, Capture, Started, Repo, Model, ID, DashboardURL string
}

type SessionBrowserOptions struct {
	Out     io.Writer
	Items   []BrowserSession
	Filters string
	Context context.Context
	Load    bool
	Refresh func(context.Context) ([]BrowserSession, error)
	Open    func(context.Context, string) error
	Copy    func(string) error
}

type SessionBrowser struct {
	mu        sync.Mutex
	options   SessionBrowserOptions
	updates   chan struct{}
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
	help      bool
	loading   bool
	loaded    bool
}

func NewSessionBrowser(options SessionBrowserOptions) *SessionBrowser {
	browser := &SessionBrowser{options: options, updates: make(chan struct{}, 1), items: append([]BrowserSession(nil), options.Items...)}
	browser.applyFilter()
	return browser
}

func (b *SessionBrowser) View(screen bentotui.Screen) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.options.Load && !b.loaded && !b.loading {
		ctx := b.options.Context
		if ctx == nil {
			ctx = context.Background()
		}
		b.refresh(ctx)
	}
	if appframe.TooSmall(screen) {
		return appframe.SmallScreen(screen)
	}
	render := appframe.New(b.options.Out).WithWidth(appframe.ForScreen(screen).Width)
	if b.help {
		return b.helpView(render, screen)
	}
	if b.detail && len(b.visible) > 0 {
		return b.detailView(render, screen)
	}
	return b.listView(render, screen)
}

func (b *SessionBrowser) Handle(ctx context.Context, key bentotui.Key) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if key.Kind == bentotui.KeyInterrupt {
		return true
	}
	if b.help {
		if key.Kind == bentotui.KeyEscape {
			b.help = false
			return false
		}
		return key.Kind == bentotui.KeyRune && key.Rune == 'q'
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
		if b.help {
			b.help = false
		} else if b.detail {
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
	case '?':
		b.help = true
	case 'j':
		b.move(1)
	case 'k':
		b.move(-1)
	case 'g':
		if b.detail {
			b.detailTop = 0
		} else {
			b.selected, b.offset = 0, 0
		}
	case 'G':
		if b.detail {
			b.detailTop = b.detailMax
		} else if len(b.visible) > 0 {
			b.selected = len(b.visible) - 1
		}
	case '/':
		if !b.detail {
			b.filtering = true
			b.message = ""
		}
	case 'r':
		b.refresh(ctx)
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

func (b *SessionBrowser) Updates() <-chan struct{} { return b.updates }

func (b *SessionBrowser) refresh(ctx context.Context) {
	if b.options.Refresh == nil || b.loading {
		return
	}
	initial := b.options.Load && !b.loaded
	b.loading = true
	if initial {
		b.message = "Loading sessions..."
	} else {
		b.message = "Refreshing sessions..."
	}
	go func() {
		items, err := b.options.Refresh(ctx)
		b.mu.Lock()
		b.loading = false
		b.loaded = true
		if err != nil {
			if initial {
				b.message = "Load failed: " + err.Error()
			} else {
				b.message = "Refresh failed: " + err.Error()
			}
		} else {
			b.items = append([]BrowserSession(nil), items...)
			b.applyFilter()
			if initial {
				b.message = "Sessions loaded"
			} else {
				b.message = "Sessions refreshed"
			}
		}
		b.mu.Unlock()
		select {
		case b.updates <- struct{}{}:
		default:
		}
	}()
}

func (b *SessionBrowser) listView(render appframe.Renderer, screen bentotui.Screen) string {
	layout := appframe.ForScreen(screen)
	blocks := []string{}
	if b.filtering || b.query != "" {
		cursor := ""
		if b.filtering {
			cursor = "_"
		}
		blocks = append(blocks, bentotui.Style{Color: render.Theme.Accent, Enabled: render.Color}.Render("/ "+b.query+cursor))
	}
	if len(b.visible) == 0 {
		body := "Captured model traffic will appear here as work sessions."
		if b.loading {
			body = "Loading sessions..."
		} else if b.query != "" {
			body = "No loaded sessions match the current filter."
		}
		title := "No sessions found"
		if b.loading {
			title = "Loading"
		}
		blocks = append(blocks, render.EmptyState(title, body))
	} else {
		reserved := 0
		if len(blocks) > 0 {
			reserved = 2
		}
		rowsAvailable := max(1, (layout.BodyHeight-reserved)/4)
		b.keepVisible(rowsAvailable)
		end := min(len(b.visible), b.offset+rowsAvailable)
		bodyRender := render.WithWidth(layout.BodyWidth)
		for position := b.offset; position < end; position++ {
			blocks = append(blocks, b.sessionRow(bodyRender, b.items[b.visible[position]], position == b.selected))
		}
	}
	footer := appframe.Footer(render.Context(),
		[]appframe.Binding{{Key: "↑↓", Label: "Navigate"}, {Key: "↵", Label: "Open"}},
		[]appframe.Binding{{Key: "?", Label: "Help"}, {Key: "q", Label: "Quit"}},
	)
	if b.message != "" {
		footer = bentotui.Truncate(b.message, layout.BodyWidth)
	}
	status := fmt.Sprintf("%d results", len(b.visible))
	if b.loading {
		status += " · loading"
	}
	if b.options.Filters != "" {
		status = b.options.Filters + " · " + status
	}
	view, _ := (appframe.Frame{Surface: "Sessions", Status: status, Lines: blockLines(blocks), Footer: footer}).Render(render.Context(), screen)
	return view
}

func (b *SessionBrowser) sessionRow(render appframe.Renderer, session BrowserSession, selected bool) string {
	marker := "  "
	if selected {
		marker = bentotui.Style{Color: render.Theme.Accent, Bold: true, Enabled: render.Color}.Render("› ")
	}
	badge := render.OutcomeBadge(session.Outcome)
	prefix := marker + badge + " "
	contentWidth := max(1, render.Width-bentotui.VisibleWidth(prefix))
	stat := bentotui.Truncate(session.Capture, max(1, min(36, contentWidth-12)))
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

func (b *SessionBrowser) detailView(render appframe.Renderer, screen bentotui.Screen) string {
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
	body := render.WithWidth(appframe.ForScreen(screen).BodyWidth).KeyValues(emptyValue(session.Title, "Untitled session"), fields...)
	lines := strings.Split(body, "\n")
	capacity := appframe.ForScreen(screen).BodyHeight
	b.detailMax = max(0, len(lines)-capacity)
	b.detailTop = min(b.detailTop, b.detailMax)
	footer := appframe.Footer(render.Context(),
		[]appframe.Binding{{Key: "↑↓", Label: "Scroll"}, {Key: "Esc", Label: "Back"}, {Key: "o", Label: "Dashboard"}},
		[]appframe.Binding{{Key: "q", Label: "Quit"}},
	)
	if b.message != "" {
		footer = b.message
	}
	view, offset := (appframe.Frame{Surface: "Session", Status: strings.ToUpper(emptyValue(session.Outcome, "unresolved")), Lines: lines, Offset: b.detailTop, Footer: footer}).Render(render.Context(), screen)
	b.detailTop = offset
	return view
}

func (b *SessionBrowser) helpView(render appframe.Renderer, screen bentotui.Screen) string {
	lines := []string{
		"Keyboard", "", "↑/↓ or j/k  Navigate or scroll", "Enter        Open selected session", "/            Filter loaded sessions",
		"r            Refresh sessions", "g/G          First or last", "y            Copy session ID", "o            Open dashboard", "Esc          Back", "q            Quit",
	}
	footer := appframe.Footer(render.Context(), []appframe.Binding{{Key: "Esc", Label: "Back"}}, []appframe.Binding{{Key: "q", Label: "Quit"}})
	view, _ := (appframe.Frame{Surface: "Help", Lines: lines, Footer: footer}).Render(render.Context(), screen)
	return view
}

func blockLines(blocks []string) []string {
	var lines []string
	for index, block := range blocks {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, strings.Split(block, "\n")...)
	}
	return lines
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
