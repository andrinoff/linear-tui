// Package components holds reusable modal overlays.
package components

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"github.com/andrinoff/linear-tui/internal/tui/common"
)

type Item struct {
	ID string
	// Prefix is rendered before the label (for example a colored icon).
	Prefix   string
	Label    string
	Detail   string
	Selected bool
	Value    any
}

// Picker is a fuzzy-filterable list in a modal. In Multi mode tab toggles
// items and enter submits the selection.
type Picker struct {
	ctx      *common.Ctx
	title    string
	input    textinput.Model
	items    []Item
	matches  []int
	cursor   int
	offset   int
	multi    bool
	loading  bool
	err      error
	w, h     int
	load     func() ([]Item, error)
	onSubmit func([]Item) tea.Cmd
}

type pickerLoadedMsg struct {
	owner *Picker
	items []Item
	err   error
}

func NewPicker(ctx *common.Ctx, title string, items []Item, onSubmit func([]Item) tea.Cmd) *Picker {
	in := textinput.New()
	in.Placeholder = "Type to filter…"
	in.Prompt = "› "
	in.SetStyles(textinput.DefaultStyles(ctx.Theme.Dark))
	p := &Picker{ctx: ctx, title: title, input: in, items: items, onSubmit: onSubmit}
	p.refilter()
	return p
}

// Multi enables multi-select.
func (p *Picker) Multi() *Picker { p.multi = true; return p }

// Loading defers items to an async loader.
func (p *Picker) Loading(load func() ([]Item, error)) *Picker {
	p.load, p.loading = load, true
	return p
}

func (p *Picker) Init() tea.Cmd {
	cmds := []tea.Cmd{p.input.Focus()}
	if p.load != nil {
		cmds = append(cmds, func() tea.Msg {
			items, err := p.load()
			return pickerLoadedMsg{owner: p, items: items, err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (p *Picker) Title() string       { return p.title }
func (p *Picker) Capturing() bool     { return true }
func (p *Picker) SetSize(w, h int)    { p.w, p.h = min(w, 72), min(h, 20); p.input.SetWidth(p.w - 6) }
func (p *Picker) Help() []key.Binding { return pickerHelp(p.multi) }

func pickerHelp(multi bool) []key.Binding {
	b := []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
	if multi {
		b = append(b, key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "toggle")))
	}
	return b
}

func (p *Picker) refilter() {
	q := strings.TrimSpace(p.input.Value())
	p.matches = p.matches[:0]
	if q == "" {
		for i := range p.items {
			p.matches = append(p.matches, i)
		}
	} else {
		data := make([]string, len(p.items))
		for i, it := range p.items {
			data[i] = it.Label + " " + it.Detail
		}
		for _, m := range fuzzy.Find(q, data) {
			p.matches = append(p.matches, m.Index)
		}
	}
	p.cursor = min(p.cursor, max(0, len(p.matches)-1))
}

func (p *Picker) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case pickerLoadedMsg:
		if msg.owner != p {
			return nil
		}
		p.loading, p.items, p.err = false, msg.items, msg.err
		p.refilter()
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			return common.Cmd(common.CloseModalMsg{})
		case "up", "ctrl+p", "ctrl+k":
			p.cursor = max(0, p.cursor-1)
			return nil
		case "down", "ctrl+n", "ctrl+j":
			p.cursor = min(len(p.matches)-1, p.cursor+1)
			return nil
		case "pgup":
			p.cursor = max(0, p.cursor-p.listHeight())
			return nil
		case "pgdown":
			p.cursor = max(0, min(len(p.matches)-1, p.cursor+p.listHeight()))
			return nil
		case "tab":
			if p.multi && len(p.matches) > 0 {
				it := &p.items[p.matches[p.cursor]]
				it.Selected = !it.Selected
				p.cursor = min(len(p.matches)-1, p.cursor+1)
			}
			return nil
		case "enter":
			var chosen []Item
			if p.multi {
				for _, it := range p.items {
					if it.Selected {
						chosen = append(chosen, it)
					}
				}
			} else if len(p.matches) > 0 {
				chosen = []Item{p.items[p.matches[p.cursor]]}
			} else {
				return nil
			}
			return tea.Sequence(common.Cmd(common.CloseModalMsg{}), p.onSubmit(chosen))
		}
	}
	var cmd tea.Cmd
	before := p.input.Value()
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != before {
		p.cursor = 0
		p.refilter()
	}
	return cmd
}

func (p *Picker) listHeight() int { return max(3, p.h-6) }

func (p *Picker) View() string {
	t := p.ctx.Theme
	w := p.w - 4
	var b strings.Builder
	b.WriteString(t.Title.Render(p.title) + "\n")
	b.WriteString(p.input.View() + "\n")
	lh := p.listHeight()
	switch {
	case p.loading:
		b.WriteString(t.Dim.Render("Loading…"))
	case p.err != nil:
		b.WriteString(t.Error.Render(common.ErrText(p.err)))
	case len(p.matches) == 0:
		b.WriteString(t.Dim.Render("No matches"))
	default:
		p.offset = common.Scroll(p.cursor, p.offset, lh, len(p.matches))
		end := min(len(p.matches), p.offset+lh)
		for i := p.offset; i < end; i++ {
			it := p.items[p.matches[i]]
			line := ""
			if p.multi {
				if it.Selected {
					line = t.Cursor.Render("[x] ")
				} else {
					line = t.Dim.Render("[ ] ")
				}
			}
			if it.Prefix != "" {
				line += it.Prefix + " "
			}
			line += it.Label
			if it.Detail != "" {
				line += "  " + t.Dim.Render(it.Detail)
			}
			if i == p.cursor {
				line = t.Selected.Render(common.Fit(t.Cursor.Render("▌")+line, w))
			} else {
				line = common.Fit(" "+line, w)
			}
			b.WriteString(line)
			if i < end-1 {
				b.WriteString("\n")
			}
		}
	}
	return t.Modal.Width(p.w).Render(b.String())
}
