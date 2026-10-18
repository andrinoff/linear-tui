package views

import (
	"fmt"
	"strings"
	"time"

	"charm.land/glamour/v2"

	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

// markdown renders s with glamour, caching by input, width and theme.
type markdown struct {
	key, out string
}

func (m *markdown) render(t *theme.Theme, s string, w int) string {
	k := fmt.Sprintf("%v|%d|%s", t.Dark, w, s)
	if k == m.key {
		return m.out
	}
	style := "light"
	if t.Dark {
		style = "dark"
	}
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(max(20, w)), glamour.WithEmoji())
	out := s
	if err == nil {
		if rendered, err := r.Render(s); err == nil {
			out = strings.Trim(rendered, "\n")
		}
	}
	m.key, m.out = k, out
	return out
}

func dueShort(t *theme.Theme, due string) string {
	d, err := time.Parse(time.DateOnly, due)
	if err != nil {
		return t.Dim.Render(due)
	}
	if d.Before(time.Now().Truncate(24 * time.Hour)) {
		return t.Error.Render(d.Format("Jan 2"))
	}
	return t.Dim.Render(d.Format("Jan 2"))
}
