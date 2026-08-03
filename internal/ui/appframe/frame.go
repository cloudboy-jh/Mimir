package appframe

import (
	"io"
	"os"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

const (
	PreferredWidth  = 80
	PreferredHeight = 20
	MinimumWidth    = 48
	MinimumHeight   = 12
)

type Layout struct {
	Width, Height, BodyWidth, BodyHeight int
}

type Frame struct {
	Surface, Status, Footer string
	Lines                   []string
	Offset                  int
	Follow                  bool
}

func Interactive(in io.Reader, out io.Writer) bool {
	if !bentotui.Interactive(in, out) {
		return false
	}
	output, ok := out.(*os.File)
	if !ok {
		return false
	}
	screen := bentotui.Dimensions(output)
	return screen.Width >= MinimumWidth && screen.Height >= MinimumHeight
}

func ForScreen(screen bentotui.Screen) Layout {
	width := min(PreferredWidth, screen.Width)
	height := min(PreferredHeight, screen.Height)
	return Layout{Width: width, Height: height, BodyWidth: max(1, width-4), BodyHeight: max(2, height-4)}
}

// FullScreenForScreen consumes the measured terminal dimensions. Persistent
// applications use this layout; short-lived command surfaces use ForScreen.
func FullScreenForScreen(screen bentotui.Screen) Layout {
	width, height := max(1, screen.Width), max(1, screen.Height)
	return Layout{Width: width, Height: height, BodyWidth: max(1, width-4), BodyHeight: max(2, height-4)}
}

func TooSmall(screen bentotui.Screen) bool {
	return screen.Width < MinimumWidth || screen.Height < MinimumHeight
}

func SmallScreen(screen bentotui.Screen) string {
	width, height := max(1, screen.Width), max(1, screen.Height)
	lines := []string{"Mimir", "", "Terminal too small", "Need at least 48x12"}
	for index := range lines {
		lines[index] = bentotui.PadRight(bentotui.Truncate(lines[index], width), width)
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines[:height], "\n")
}

func Wrap(lines []string, width int) []string {
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, bentotui.WrapPreserve(line, max(1, width))...)
	}
	return wrapped
}

func (f Frame) Render(ctx bentotui.Context, screen bentotui.Screen) (string, int) {
	return f.RenderLayout(ctx, screen, ForScreen(screen))
}

func (f Frame) RenderLayout(ctx bentotui.Context, screen bentotui.Screen, layout Layout) (string, int) {
	ctx.Width = layout.Width
	if ctx.Theme.Text.R == 0 && ctx.Theme.Text.G == 0 && ctx.Theme.Text.B == 0 {
		ctx.Theme = bentotui.Mimir
	}
	lines := Wrap(f.Lines, layout.BodyWidth)
	maxOffset := max(0, len(lines)-layout.BodyHeight)
	offset := min(max(0, f.Offset), maxOffset)
	if f.Follow {
		offset = maxOffset
	}
	end := min(len(lines), offset+layout.BodyHeight)
	visible := append([]string(nil), lines[offset:end]...)
	for len(visible) < layout.BodyHeight {
		visible = append(visible, "")
	}

	border := bentotui.Style{Color: ctx.Theme.Border, Enabled: ctx.Color}
	inner := layout.Width - 2
	title := "Mimir · " + strings.TrimSpace(f.Surface)
	result := []string{frameLabelLine(border, layout.Width, "┌─", title, f.Status, "┐")}
	for _, line := range visible {
		content := " " + bentotui.Truncate(line, layout.BodyWidth) + " "
		result = append(result, border.Render("│")+bentotui.PadRight(content, inner)+border.Render("│"))
	}
	result = append(result, border.Render("├"+strings.Repeat("─", inner)+"┤"))
	result = append(result, border.Render("│")+bentotui.PadRight(" "+bentotui.Truncate(f.Footer, layout.BodyWidth)+" ", inner)+border.Render("│"))
	result = append(result, border.Render("└"+strings.Repeat("─", inner)+"┘"))
	return strings.Join(result, "\n"), offset
}

func frameLabelLine(border bentotui.Style, width int, left, title, right, end string) string {
	inner := width - bentotui.VisibleWidth(left) - bentotui.VisibleWidth(end)
	leftLabel := " " + strings.TrimSpace(title) + " "
	rightLabel := ""
	if strings.TrimSpace(right) != "" {
		rightLabel = " " + strings.TrimSpace(right) + " "
		rightLabel = bentotui.Truncate(rightLabel, max(0, inner/2))
	}
	leftLabel = bentotui.Truncate(leftLabel, max(1, inner-bentotui.VisibleWidth(rightLabel)))
	fill := max(0, inner-bentotui.VisibleWidth(leftLabel)-bentotui.VisibleWidth(rightLabel))
	return border.Render(left) + leftLabel + border.Render(strings.Repeat("─", fill)) + rightLabel + border.Render(end)
}
