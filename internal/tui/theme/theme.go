// Package theme holds the lipgloss styles used across the TUI.
package theme

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

type Theme struct {
	Dark bool

	Accent, Fg, Muted, Subtle, Border, Selection color.Color
	Red, Orange, Yellow, Green, Blue             color.Color

	App         lipgloss.Style
	Title       lipgloss.Style
	Dim         lipgloss.Style
	Faint       lipgloss.Style
	Bold        lipgloss.Style
	Error       lipgloss.Style
	Success     lipgloss.Style
	Selected    lipgloss.Style
	Cursor      lipgloss.Style
	Header      lipgloss.Style
	Pane        lipgloss.Style
	PaneFocused lipgloss.Style
	Modal       lipgloss.Style
	StatusBar   lipgloss.Style
	StatusKey   lipgloss.Style
	Chip        lipgloss.Style
	Badge       lipgloss.Style
	Toast       lipgloss.Style
	ToastErr    lipgloss.Style
	Group       lipgloss.Style
	Link        lipgloss.Style
}

func New(dark bool) *Theme {
	ld := lipgloss.LightDark(dark)
	t := &Theme{
		Dark:      dark,
		Accent:    lipgloss.Color("#5E6AD2"),
		Fg:        ld(lipgloss.Color("#1F2023"), lipgloss.Color("#E6E6E9")),
		Muted:     ld(lipgloss.Color("#6B6F76"), lipgloss.Color("#8A8F98")),
		Subtle:    ld(lipgloss.Color("#B0B3BA"), lipgloss.Color("#4B4F58")),
		Border:    ld(lipgloss.Color("#D4D6DB"), lipgloss.Color("#34363D")),
		Selection: ld(lipgloss.Color("#E8E9FB"), lipgloss.Color("#2A2D4A")),
		Red:       lipgloss.Color("#EB5757"),
		Orange:    lipgloss.Color("#F2994A"),
		Yellow:    lipgloss.Color("#F2C94C"),
		Green:     lipgloss.Color("#4CB782"),
		Blue:      lipgloss.Color("#4EA7FC"),
	}
	s := lipgloss.NewStyle
	t.App = s().Foreground(t.Fg)
	t.Title = s().Bold(true).Foreground(t.Fg)
	t.Dim = s().Foreground(t.Muted)
	t.Faint = s().Foreground(t.Subtle)
	t.Bold = s().Bold(true)
	t.Error = s().Foreground(t.Red)
	t.Success = s().Foreground(t.Green)
	t.Selected = s().Background(t.Selection)
	t.Cursor = s().Foreground(t.Accent).Bold(true)
	t.Header = s().Foreground(t.Muted).Bold(true)
	t.Pane = s().Border(lipgloss.RoundedBorder()).BorderForeground(t.Border)
	t.PaneFocused = t.Pane.BorderForeground(t.Accent)
	t.Modal = s().Border(lipgloss.RoundedBorder()).BorderForeground(t.Accent).Padding(0, 1)
	t.StatusBar = s().Foreground(t.Muted)
	t.StatusKey = s().Foreground(t.Accent).Bold(true)
	t.Chip = s().Foreground(t.Fg).Background(t.Border).Padding(0, 1)
	t.Badge = s().Foreground(lipgloss.Color("#FFFFFF")).Background(t.Accent).Padding(0, 1).Bold(true)
	t.Toast = s().Border(lipgloss.RoundedBorder()).BorderForeground(t.Green).Padding(0, 1)
	t.ToastErr = t.Toast.BorderForeground(t.Red)
	t.Group = s().Foreground(t.Fg).Bold(true)
	t.Link = s().Foreground(t.Blue).Underline(true)
	return t
}

// Hex returns a color from a hex string, falling back to muted.
func (t *Theme) Hex(hex string) color.Color {
	if hex == "" {
		return t.Muted
	}
	return lipgloss.Color(hex)
}

func (t *Theme) Fgc(c color.Color, s string) string {
	return lipgloss.NewStyle().Foreground(c).Render(s)
}

// Priority renders Linear's priority as a compact icon.
func (t *Theme) Priority(p float64) string {
	switch int(p) {
	case 1:
		return t.Fgc(t.Red, "!!!")
	case 2:
		return t.Fgc(t.Orange, "▮▮▮")
	case 3:
		return t.Fgc(t.Yellow, "▮▮") + t.Fgc(t.Subtle, "▯")
	case 4:
		return t.Fgc(t.Blue, "▮") + t.Fgc(t.Subtle, "▯▯")
	}
	return t.Fgc(t.Subtle, "---")
}

var PriorityNames = []string{"No priority", "Urgent", "High", "Medium", "Low"}

func PriorityName(p float64) string {
	if i := int(p); i >= 0 && i < len(PriorityNames) {
		return PriorityNames[i]
	}
	return PriorityNames[0]
}

// StateIcon renders a workflow state glyph in the state's color.
func (t *Theme) StateIcon(stateType, hex string) string {
	icon := map[string]string{
		"triage": "◇", "backlog": "◌", "unstarted": "○",
		"started": "◐", "completed": "●", "canceled": "⊘",
	}[stateType]
	if icon == "" {
		icon = "○"
	}
	return t.Fgc(t.Hex(hex), icon)
}

// Progress renders a progress bar of width w for a 0..1 fraction.
func (t *Theme) Progress(frac float64, w int) string {
	frac = max(0, min(1, frac))
	n := int(frac*float64(w) + 0.5)
	return t.Fgc(t.Accent, strings.Repeat("█", n)) + t.Fgc(t.Subtle, strings.Repeat("░", max(0, w-n))) + t.Dim.Render(fmt.Sprintf(" %3.0f%%", frac*100))
}
