package views

import (
	"slices"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
)

const searchDebounce = 300 * time.Millisecond

// Search is server-side issue search with debounced input.
type Search struct {
	ctx     *common.Ctx
	input   textinput.Model
	results *IssueList
	term    string
	seq     int
	focused bool
	w, h    int
}

type searchTickMsg struct {
	owner *Search
	seq   int
}

func NewSearch(ctx *common.Ctx, initial string) *Search {
	in := textinput.New()
	in.Prompt = "Search › "
	in.Placeholder = "title, description, or ENG-123"
	in.SetStyles(textinput.DefaultStyles(ctx.Theme.Dark))
	in.SetValue(initial)
	s := &Search{ctx: ctx, input: in, focused: true, term: initial}
	s.results = NewIssueList(ctx, "Search", linear.Filter{}, func(_ linear.Filter, cursor string) (linear.Page[linear.Issue], error) {
		if s.term == "" {
			return linear.Page[linear.Issue]{}, nil
		}
		return ctx.Svc.Search(bg(), s.term, cursor)
	}).Fixed()
	s.results.sort = SortUpdated
	return s
}

func (s *Search) Init() tea.Cmd {
	cmds := []tea.Cmd{s.input.Focus()}
	if s.term != "" {
		cmds = append(cmds, s.results.Init())
	} else {
		s.results.state = stateLoaded
	}
	return tea.Batch(cmds...)
}

func (s *Search) Title() string   { return "Search" }
func (s *Search) Capturing() bool { return s.focused || s.results.Capturing() }

func (s *Search) SetSize(w, h int) {
	s.w, s.h = w, h
	s.input.SetWidth(w - 12)
	s.results.SetSize(w, h-2)
}

func (s *Search) Help() []key.Binding {
	if s.focused {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter/↓", "results")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}
	}
	return append([]key.Binding{key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "edit query"))}, s.results.Help()...)
}

func (s *Search) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case searchTickMsg:
		if msg.owner != s || msg.seq != s.seq {
			return nil
		}
		s.term = s.input.Value()
		s.results.issues = nil
		return s.results.load(false)
	case common.ClickMsg:
		msg.Y -= 2
		s.focused = false
		s.input.Blur()
		return s.results.Update(msg)
	case tea.PasteMsg:
		if s.focused {
			return s.edit(msg)
		}
	case tea.KeyPressMsg:
		if s.focused {
			switch msg.String() {
			case "esc":
				return common.Cmd(common.PopMsg{})
			case "enter", "down":
				s.focused = false
				s.input.Blur()
				return nil
			}
			return s.edit(msg)
		}
		if msg.String() == "/" && !s.results.Capturing() {
			s.focused = true
			return s.input.Focus()
		}
	}
	return s.results.Update(msg)
}

func (s *Search) Selection() []linear.Issue { return s.results.Selection() }
func (s *Search) HandlesEsc() bool          { return s.results.HandlesEsc() }

// edit applies typed or pasted input and schedules a debounced search.
func (s *Search) edit(msg tea.Msg) tea.Cmd {
	before := s.input.Value()
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if s.input.Value() == before {
		return cmd
	}
	s.seq++
	seq := s.seq
	return tea.Batch(cmd, tea.Tick(searchDebounce, func(time.Time) tea.Msg { return searchTickMsg{owner: s, seq: seq} }))
}

func (s *Search) View() string {
	t := s.ctx.Theme
	body := s.results.View()
	if s.term == "" {
		body = common.FitLines("  "+t.Dim.Render("Type to search issues across your workspace."), s.h-2)
	}
	return s.input.View() + "\n\n" + body
}

// CustomViews lists saved issue views.
type CustomViews struct {
	cursorList
	views []linear.CustomView
	team  *linear.Team
}

type customViewsLoadedMsg struct {
	owner *CustomViews
	views []linear.CustomView
	err   error
}

func NewCustomViews(ctx *common.Ctx) *CustomViews {
	return &CustomViews{cursorList: newCursorList(ctx)}
}

// NewTeamViews lists views scoped to the team.
func NewTeamViews(ctx *common.Ctx, team *linear.Team) *CustomViews {
	return &CustomViews{cursorList: newCursorList(ctx), team: team}
}

func (c *CustomViews) Title() string {
	if c.team != nil {
		return c.team.Name + " › Views"
	}
	return "Views"
}
func (c *CustomViews) Help() []key.Binding {
	return []key.Binding{c.ctx.Keys.Enter, c.ctx.Keys.Refresh}
}
func (c *CustomViews) Init() tea.Cmd { return tea.Batch(c.load(), c.spin.Tick) }

func (c *CustomViews) load() tea.Cmd {
	return func() tea.Msg {
		v, err := c.ctx.Svc.CustomViews(bg())
		return customViewsLoadedMsg{owner: c, views: v, err: err}
	}
}

func (c *CustomViews) Update(msg tea.Msg) tea.Cmd {
	if cmd, ok := c.tick(msg); ok {
		return cmd
	}
	if c.nav(msg) {
		return nil
	}
	switch msg := msg.(type) {
	case customViewsLoadedMsg:
		if msg.owner != c {
			return nil
		}
		c.loading, c.err = false, msg.err
		if msg.err != nil {
			return common.Err(msg.err)
		}
		c.views = msg.views
		if c.team != nil {
			c.views = slices.DeleteFunc(c.views, func(v linear.CustomView) bool { return v.Team == nil || v.Team.Id != c.team.Id })
		}
		c.n = len(c.views)
	case common.RefreshMsg:
		return c.load()
	case tea.KeyPressMsg:
		if key.Matches(msg, c.ctx.Keys.Enter) && c.cursor < len(c.views) {
			v := c.views[c.cursor]
			l := NewIssueList(c.ctx, v.Name, linear.Filter{}, func(_ linear.Filter, cursor string) (linear.Page[linear.Issue], error) {
				return c.ctx.Svc.CustomViewIssues(bg(), v.Id, cursor)
			}).Fixed()
			if v.Team != nil {
				l.defaults.TeamID = v.Team.Id
			}
			return common.Cmd(common.PushMsg{View: l})
		}
	}
	return nil
}

func (c *CustomViews) View() string {
	t := c.ctx.Theme
	return c.render(nil, "No saved views. Create views in Linear to see them here.", func(i int, _ bool) string {
		v := c.views[i]
		scope := "workspace"
		if v.Team != nil {
			scope = v.Team.Key
		}
		shared := ""
		if !v.Shared {
			shared = t.Faint.Render(" (personal)")
		}
		return "≡ " + common.Fit(t.Bold.Render(v.Name)+shared, 40) + " " + common.Fit(t.Dim.Render(scope), 10) + " " + t.Dim.Render(v.Description)
	})
}
