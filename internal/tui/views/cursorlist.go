package views

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/andrinoff/linear-tui/internal/tui/common"
)

// cursorList is the shared cursor, scrolling, and load-state handling for
// simple one-line-per-row screens (cycles, projects, inbox, views).
type cursorList struct {
	ctx     *common.Ctx
	cursor  int
	offset  int
	n       int
	loading bool
	err     error
	reqID   int
	spin    spinner.Model
	w, h    int
	top     int
}

func newCursorList(ctx *common.Ctx) cursorList {
	return cursorList{ctx: ctx, loading: true,
		spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(ctx.Theme.Cursor))}
}

func (c *cursorList) SetSize(w, h int) { c.w, c.h = w, h }
func (c *cursorList) Capturing() bool  { return false }

// nav handles movement keys and returns true if msg was consumed.
func (c *cursorList) nav(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case common.ClickMsg:
		if i := c.offset + msg.Y - c.top; i >= 0 && i < c.n {
			c.cursor = i
		}
		return true
	case tea.KeyPressMsg:
		k := c.ctx.Keys
		switch {
		case key.Matches(msg, k.Down):
			c.cursor = min(c.n-1, c.cursor+1)
		case key.Matches(msg, k.Up):
			c.cursor = max(0, c.cursor-1)
		case key.Matches(msg, k.PageDown):
			c.cursor = min(c.n-1, c.cursor+c.h/2)
		case key.Matches(msg, k.PageUp):
			c.cursor = max(0, c.cursor-c.h/2)
		case key.Matches(msg, k.Top):
			c.cursor = 0
		case key.Matches(msg, k.Bottom):
			c.cursor = c.n - 1
		default:
			return false
		}
		c.cursor = max(0, c.cursor)
		return true
	}
	return false
}

func (c *cursorList) tick(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(spinner.TickMsg); ok {
		var cmd tea.Cmd
		c.spin, cmd = c.spin.Update(m)
		return cmd, true
	}
	return nil, false
}

// render draws header lines, then rows produced by row(i, focused), handling
// the loading, error, and empty states.
func (c *cursorList) render(header []string, empty string, row func(i int, focused bool) string) string {
	t := c.ctx.Theme
	lines := append([]string(nil), header...)
	switch {
	case c.loading && c.n == 0:
		lines = append(lines, " "+c.spin.View()+" Loading…")
	case c.err != nil && c.n == 0:
		lines = append(lines, " "+t.Error.Render(common.ErrText(c.err)), " "+t.Dim.Render("Press r to retry."))
	case c.n == 0:
		lines = append(lines, " "+t.Dim.Render(empty))
	default:
		c.cursor = min(c.cursor, c.n-1)
		h := max(1, c.h-len(lines))
		c.offset = common.Scroll(c.cursor, c.offset, h, c.n)
		c.top = len(lines)
		for i := c.offset; i < min(c.n, c.offset+h); i++ {
			focused := i == c.cursor
			text := row(i, focused)
			if focused {
				text = t.Select(" "+text, c.w)
			} else {
				text = common.Fit(" "+text, c.w)
			}
			lines = append(lines, text)
		}
	}
	return common.FitLines(strings.Join(lines, "\n"), c.h)
}
