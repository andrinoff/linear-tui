package components

import (
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andrinoff/linear-tui/internal/tui/common"
)

var (
	escKey   = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	enterKey = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit"))
)

// Confirm asks a yes/no question.
type Confirm struct {
	ctx   *common.Ctx
	text  string
	onYes tea.Cmd
	w     int
}

func NewConfirm(ctx *common.Ctx, text string, onYes tea.Cmd) *Confirm {
	return &Confirm{ctx: ctx, text: text, onYes: onYes}
}

func (c *Confirm) Init() tea.Cmd    { return nil }
func (c *Confirm) Title() string    { return "Confirm" }
func (c *Confirm) Capturing() bool  { return true }
func (c *Confirm) SetSize(w, _ int) { c.w = min(w, 60) }
func (c *Confirm) Help() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yes")), key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no"))}
}

func (c *Confirm) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "y", "Y", "enter":
			return tea.Sequence(common.Cmd(common.CloseModalMsg{}), c.onYes)
		case "n", "N", "esc", "q":
			return common.Cmd(common.CloseModalMsg{})
		}
	}
	return nil
}

func (c *Confirm) View() string {
	t := c.ctx.Theme
	return t.Modal.Width(c.w).Render(t.Title.Render(c.text) + "\n\n" + t.StatusKey.Render("y") + t.Dim.Render(" yes   ") + t.StatusKey.Render("n") + t.Dim.Render(" no"))
}

// Prompt asks for a single line of text.
type Prompt struct {
	ctx      *common.Ctx
	title    string
	input    textinput.Model
	onSubmit func(string) tea.Cmd
	w        int
}

func NewPrompt(ctx *common.Ctx, title, placeholder string, onSubmit func(string) tea.Cmd) *Prompt {
	in := textinput.New()
	in.Placeholder = placeholder
	in.Prompt = "› "
	in.SetStyles(textinput.DefaultStyles(ctx.Theme.Dark))
	return &Prompt{ctx: ctx, title: title, input: in, onSubmit: onSubmit}
}

func (p *Prompt) Init() tea.Cmd       { return p.input.Focus() }
func (p *Prompt) Title() string       { return p.title }
func (p *Prompt) Capturing() bool     { return true }
func (p *Prompt) SetSize(w, _ int)    { p.w = min(w, 72); p.input.SetWidth(p.w - 6) }
func (p *Prompt) Help() []key.Binding { return []key.Binding{enterKey, escKey} }

func (p *Prompt) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			return common.Cmd(common.CloseModalMsg{})
		case "enter":
			v := strings.TrimSpace(p.input.Value())
			if v == "" {
				return nil
			}
			return tea.Sequence(common.Cmd(common.CloseModalMsg{}), p.onSubmit(v))
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

func (p *Prompt) View() string {
	t := p.ctx.Theme
	return t.Modal.Width(p.w).Render(t.Title.Render(p.title) + "\n" + p.input.View())
}

// Composer edits multi-line markdown, for comments.
type Composer struct {
	ctx      *common.Ctx
	title    string
	area     textarea.Model
	onSubmit func(string) tea.Cmd
	w, h     int
	discard  bool
	initial  string
}

type editorDoneMsg struct {
	owner *Composer
	text  string
	err   error
}

func NewComposer(ctx *common.Ctx, title, initial string, onSubmit func(string) tea.Cmd) *Composer {
	ta := textarea.New()
	ta.Placeholder = "Write in markdown…  (ctrl+e opens $EDITOR)"
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetStyles(textarea.DefaultStyles(ctx.Theme.Dark))
	ta.SetValue(initial)
	return &Composer{ctx: ctx, title: title, area: ta, onSubmit: onSubmit, initial: initial}
}

func (c *Composer) Init() tea.Cmd   { return c.area.Focus() }
func (c *Composer) Title() string   { return c.title }
func (c *Composer) Capturing() bool { return true }
func (c *Composer) SetSize(w, h int) {
	c.w, c.h = min(w, 90), min(h, 20)
	c.area.SetWidth(c.w - 4)
	c.area.SetHeight(c.h - 4)
}

func (c *Composer) Help() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "send")),
		key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "editor")),
		escKey,
	}
}

func (c *Composer) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case editorDoneMsg:
		if msg.owner != c {
			return nil
		}
		if msg.err != nil {
			return common.Err(msg.err)
		}
		c.area.SetValue(msg.text)
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			if c.area.Value() != c.initial && !c.discard {
				c.discard = true
				return common.Toast("Unsaved text. Press esc again to discard.")
			}
			return common.Cmd(common.CloseModalMsg{})
		case "ctrl+s":
			v := strings.TrimSpace(c.area.Value())
			if v == "" {
				return nil
			}
			return tea.Sequence(common.Cmd(common.CloseModalMsg{}), c.onSubmit(v))
		case "ctrl+e":
			return c.openEditor()
		}
		c.discard = false
	}
	var cmd tea.Cmd
	c.area, cmd = c.area.Update(msg)
	return cmd
}

func (c *Composer) openEditor() tea.Cmd {
	cmd, path, err := EditorCmd(c.ctx.Cfg.EditorCommand(), c.area.Value())
	if err != nil {
		return common.Err(err)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return editorDoneMsg{owner: c, err: err}
		}
		b, rerr := os.ReadFile(path)
		return editorDoneMsg{owner: c, text: strings.TrimRight(string(b), "\n"), err: rerr}
	})
}

// EditorCmd writes text to a temp markdown file and returns the command that
// opens it in the user's editor.
func EditorCmd(editor, text string) (*exec.Cmd, string, error) {
	f, err := os.CreateTemp("", "linear-*.md")
	if err != nil {
		return nil, "", err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return nil, "", err
	}
	f.Close()
	parts := strings.Fields(editor)
	return exec.Command(parts[0], append(parts[1:], f.Name())...), f.Name(), nil
}

func (c *Composer) View() string {
	t := c.ctx.Theme
	return t.Modal.Width(c.w).Render(t.Title.Render(c.title) + "\n" + c.area.View() + "\n" +
		t.Dim.Render("ctrl+s send · ctrl+e $EDITOR · esc cancel"))
}

// HelpOverlay shows every binding grouped by area.
type HelpOverlay struct {
	ctx    *common.Ctx
	groups [][]key.Binding
	titles []string
	w, h   int
}

func NewHelpOverlay(ctx *common.Ctx, view []key.Binding) *HelpOverlay {
	k := ctx.Keys
	return &HelpOverlay{
		ctx:    ctx,
		titles: []string{"Global", "Issue", "This view"},
		groups: [][]key.Binding{k.GlobalHelp(), k.IssueHelp(), view},
	}
}

func (h *HelpOverlay) Init() tea.Cmd       { return nil }
func (h *HelpOverlay) Title() string       { return "Help" }
func (h *HelpOverlay) Capturing() bool     { return true }
func (h *HelpOverlay) SetSize(w, hh int)   { h.w, h.h = min(w, 120), hh }
func (h *HelpOverlay) Help() []key.Binding { return []key.Binding{escKey} }

func (h *HelpOverlay) Update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		return common.Cmd(common.CloseModalMsg{})
	}
	return nil
}

func (h *HelpOverlay) View() string {
	t := h.ctx.Theme
	var cols []string
	for i, g := range h.groups {
		var lines []string
		lines = append(lines, t.Header.Render(h.titles[i]))
		for _, b := range g {
			if !b.Enabled() {
				continue
			}
			hb := b.Help()
			lines = append(lines, common.Fit(t.StatusKey.Render(hb.Key), 14)+t.Dim.Render(hb.Desc))
		}
		cols = append(cols, lipgloss.NewStyle().MarginRight(3).Render(strings.Join(lines, "\n")))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	return t.Modal.Render(t.Title.Render("Keyboard shortcuts") + "\n\n" + body + "\n\n" + t.Dim.Render("press any key to close"))
}
