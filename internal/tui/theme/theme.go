// Package theme holds Linear's palette and the styles used across the TUI.
package theme

import (
	"fmt"
	"hash/fnv"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type Theme struct {
	Dark bool
	// Mono drops Linear's decorative colors (labels, projects, team icons)
	// so the UI stays within the theme's grays. Status icons and avatars
	// keep their color, since the color carries meaning.
	Mono bool

	// Surfaces, from back to front, mirroring Linear's app: the app frame
	// (sidebar), the inset content panel, cards, and chips on cards. A nil
	// surface is transparent and shows the terminal background.
	AppBg, PanelBg, CardBg, CardFocusBg, ChipBg, ChipFocusBg, ModalBg color.Color
	// Selection highlights the focused row or nav item.
	Selection color.Color

	Accent, Fg, Bright, Muted, Subtle, Border color.Color
	Red, Orange, Yellow, Green, Blue          color.Color

	// Image returns terminal-graphics cells for an image URL sized cols×1,
	// or false when images are unavailable or still loading.
	Image func(url string, cols int) (string, bool)

	App       lipgloss.Style
	Title     lipgloss.Style
	Dim       lipgloss.Style
	Faint     lipgloss.Style
	Bold      lipgloss.Style
	Error     lipgloss.Style
	Success   lipgloss.Style
	Cursor    lipgloss.Style
	Header    lipgloss.Style
	StatusBar lipgloss.Style
	StatusKey lipgloss.Style
	Badge     lipgloss.Style
	Group     lipgloss.Style
	Link      lipgloss.Style
}

// New returns the built-in Linear theme for a dark or light terminal.
func New(dark bool) *Theme { return FromSpec(DefaultSpec(dark)) }

// FromSpec builds a theme from a palette.
func FromSpec(sp Spec) *Theme {
	c := func(s string) color.Color {
		if s == "" {
			return nil
		}
		return lipgloss.Color(s)
	}
	t := &Theme{
		Dark: sp.Dark, Mono: sp.Monochrome,
		AppBg: c(sp.Background), PanelBg: c(sp.Panel), CardBg: c(sp.Card), CardFocusBg: c(sp.CardFocus),
		ChipBg: c(sp.Chip), ChipFocusBg: c(sp.ChipFocus), ModalBg: c(sp.Modal), Selection: c(sp.Selection),
		Accent: c(sp.Accent), Fg: c(sp.Text), Bright: c(sp.Bright), Muted: c(sp.Muted), Subtle: c(sp.Subtle), Border: c(sp.Border),
		Red: c(sp.Red), Orange: c(sp.Orange), Yellow: c(sp.Yellow), Green: c(sp.Green), Blue: c(sp.Blue),
	}
	s := lipgloss.NewStyle
	t.App = s().Foreground(t.Fg)
	t.Title = s().Bold(true).Foreground(t.Bright)
	t.Dim = s().Foreground(t.Muted)
	t.Faint = s().Foreground(t.Subtle)
	t.Bold = s().Bold(true).Foreground(t.Bright)
	t.Error = s().Foreground(t.Red)
	t.Success = s().Foreground(t.Green)
	t.Cursor = s().Foreground(t.Accent).Bold(true)
	t.Header = s().Foreground(t.Muted)
	t.StatusBar = s().Foreground(t.Muted)
	t.StatusKey = s().Foreground(t.Fg)
	t.Badge = s().Foreground(contrast(t.Accent)).Background(t.Accent).Padding(0, 1).Bold(true)
	t.Group = s().Foreground(t.Bright)
	t.Link = s().Foreground(t.Blue).Underline(true)
	return t
}

// contrast picks black or white text for a background.
func contrast(bg color.Color) color.Color {
	if bg == nil {
		return nil
	}
	r, g, b, _ := bg.RGBA()
	if 0.299*float64(r>>8)+0.587*float64(g>>8)+0.114*float64(b>>8) > 150 {
		return lipgloss.Color("#000000")
	}
	return lipgloss.Color("#FFFFFF")
}

func bgSeq(c color.Color) string { return ansi.NewStyle().BackgroundColor(c).String() }

// Paint fills s with background bg, re-applying it after every reset so
// nested styles keep the surface color. Nested surfaces painted later win.
func Paint(s string, bg color.Color) string {
	if bg == nil {
		return s
	}
	seq := bgSeq(bg)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		l = strings.ReplaceAll(l, "\x1b[0m", ansi.ResetStyle)
		l = strings.ReplaceAll(l, ansi.ResetStyle, ansi.ResetStyle+seq)
		lines[i] = seq + l + ansi.ResetStyle
	}
	return strings.Join(lines, "\n")
}

// Surface pads every line of s to w cells and paints it with bg.
func Surface(s string, w int, bg color.Color) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > w {
			l = ansi.Truncate(l, w, "…")
		}
		lines[i] = l + strings.Repeat(" ", max(0, w-ansi.StringWidth(l)))
	}
	return Paint(strings.Join(lines, "\n"), bg)
}

// Select highlights a row of width w.
func (t *Theme) Select(s string, w int) string { return Surface(s, w, t.Selection) }

// Box draws a floating surface (menus, dialogs, toasts) of outer width w.
func (t *Theme) Box(content string, w int, border color.Color) string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = " " + l
	}
	body := Surface(strings.Join(lines, "\n"), max(1, w-2), t.ModalBg)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Render(body)
}

// Pill renders s as a chip on bg.
func Pill(s string, bg color.Color) string { return Paint(" "+s+" ", bg) }

// Hex returns a Linear-provided decorative color (label, project), or a
// gray in monochrome themes.
func (t *Theme) Hex(hex string) color.Color {
	if hex == "" || t.Mono {
		return t.Muted
	}
	return lipgloss.Color(hex)
}

func (t *Theme) Fgc(c color.Color, s string) string {
	return lipgloss.NewStyle().Foreground(c).Render(s)
}

// Priority renders Linear's priority icon in 3 cells: signal bars, an
// orange "!" box for urgent, and dots for no priority.
func (t *Theme) Priority(p float64) string {
	bars := func(n int) string {
		glyphs := []string{"▂", "▄", "▆"}
		out := ""
		for i, g := range glyphs {
			if i < n {
				out += t.Fgc(t.Fg, g)
			} else {
				out += t.Fgc(t.Subtle, g)
			}
		}
		return out
	}
	switch int(p) {
	case 1:
		return lipgloss.NewStyle().Background(t.Orange).Foreground(contrast(t.Orange)).Bold(true).Render(" ! ")
	case 2:
		return bars(3)
	case 3:
		return bars(2)
	case 4:
		return bars(1)
	}
	return t.Fgc(t.Subtle, "···")
}

var PriorityNames = []string{"No priority", "Urgent", "High", "Medium", "Low"}

func PriorityName(p float64) string {
	if i := int(p); i >= 0 && i < len(PriorityNames) {
		return PriorityNames[i]
	}
	return PriorityNames[0]
}

// StateIcon renders Linear's workflow state glyph in the state's color.
func (t *Theme) StateIcon(stateType, hex string) string {
	icon := map[string]string{
		"triage": "◇", "backlog": "◌", "unstarted": "○",
		"started": "◐", "completed": "●", "canceled": "⊘",
	}[stateType]
	if icon == "" {
		icon = "○"
	}
	c := t.Muted
	if hex != "" {
		c = lipgloss.Color(hex)
	}
	return t.Fgc(c, icon)
}

var (
	avatarColors = []string{"#E5484D", "#F76B15", "#FFC53D", "#46A758", "#12A594", "#0090FF", "#6E56CF", "#D6409F", "#8E8C99"}
	grayColors   = []string{"#3A3A3A", "#4A4A4A", "#5A5A5A", "#6A6A6A", "#7A7A7A"}
)

// Avatar renders a user's picture as terminal graphics when available,
// otherwise a two-letter initials badge in a color derived from name.
func (t *Theme) Avatar(name, url string) string {
	if url != "" && t.Image != nil {
		if img, ok := t.Image(url, 2); ok {
			return img
		}
	}
	if name == "" {
		return t.Fgc(t.Subtle, "◌ ")
	}
	var initials []rune
	for _, f := range strings.FieldsFunc(name, func(r rune) bool { return r == ' ' || r == '.' || r == '_' || r == '-' }) {
		initials = append(initials, []rune(strings.ToUpper(f))[0])
		if len(initials) == 2 {
			break
		}
	}
	if len(initials) == 1 {
		if r := []rune(strings.ToUpper(name)); len(r) > 1 {
			initials = append(initials, r[1])
		}
	}
	bg := lipgloss.Color(pick(avatarColors, name))
	return lipgloss.NewStyle().Background(bg).Foreground(contrast(bg)).Bold(true).Render(string(initials))
}

// ColorFor picks a stable decorative color for name (team icons), gray in
// monochrome themes.
func (t *Theme) ColorFor(name string) string {
	if t.Mono {
		return pick(grayColors, name)
	}
	return pick(avatarColors, name)
}

func pick(palette []string, name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return palette[h.Sum32()%uint32(len(palette))]
}

// Progress renders a progress bar of width w for a 0..1 fraction.
func (t *Theme) Progress(frac float64, w int) string {
	frac = max(0, min(1, frac))
	n := int(frac*float64(w) + 0.5)
	return t.Fgc(t.Accent, strings.Repeat("━", n)) + t.Fgc(t.Border, strings.Repeat("━", max(0, w-n))) + t.Dim.Render(fmt.Sprintf(" %3.0f%%", frac*100))
}
