package common

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/pkg/browser"
)

// Copy writes s to the system clipboard, falling back to OSC52 (which also
// works over SSH) when no clipboard utility is available.
func Copy(s, what string) tea.Cmd {
	if err := clipboard.WriteAll(s); err != nil {
		return tea.Batch(tea.SetClipboard(s), Toast("Copied "+what))
	}
	return Toast("Copied " + what)
}

func OpenURL(url string) tea.Cmd {
	return func() tea.Msg {
		if err := browser.OpenURL(url); err != nil {
			return ErrMsg{Err: err}
		}
		return nil
	}
}

// Fit truncates or pads s (which may contain ANSI) to exactly w cells.
func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// FitLines clips content to h lines, padding when shorter.
func FitLines(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// Scroll keeps cursor within a window of height h starting at offset.
func Scroll(cursor, offset, h, n int) int {
	if h <= 0 {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+h {
		offset = cursor - h + 1
	}
	return max(0, min(offset, max(0, n-h)))
}
