package views

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

// Tab is one of the view tabs under a Linear page header, such as
// "Assigned" or "Created" on My issues.
type Tab struct {
	Name   string
	Filter linear.Filter
}

var (
	nextTabKey = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "next tab"))
	prevTabKey = key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "prev tab"))
	layoutKey  = key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "list/board"))
)

// Issues is a Linear issues page: tabs, a list/board layout toggle
// (ctrl+b, as in Linear), and the active layout below.
type Issues struct {
	ctx   *common.Ctx
	id    string
	title string
	team  *linear.Team
	tabs  []Tab
	tab   int
	board bool
	child common.View
	w, h  int
	// tabX holds the x range of each tab for mouse clicks.
	tabX [][2]int
}

// NewIssues builds an issues page. id keys the remembered layout;
// defaultBoard picks the layout the first time.
func NewIssues(ctx *common.Ctx, id, title string, team *linear.Team, tabs []Tab, defaultBoard bool) *Issues {
	v := &Issues{ctx: ctx, id: id, title: title, team: team, tabs: tabs, board: defaultBoard}
	if l, ok := ctx.State.Layouts[id]; ok {
		v.board = l == "board"
	}
	return v
}

// MyIssues mirrors Linear's My issues page.
func MyIssues(ctx *common.Ctx) *Issues {
	return NewIssues(ctx, "my", "My issues", nil, []Tab{
		{Name: "Assigned", Filter: linear.Filter{AssignedToMe: true}},
		{Name: "Created", Filter: linear.Filter{CreatedByMe: true}},
		{Name: "Subscribed", Filter: linear.Filter{SubscribedBy: true}},
	}, true)
}

// TeamIssues mirrors a team's Issues page.
func TeamIssues(ctx *common.Ctx, team *linear.Team) *Issues {
	if team == nil {
		return NewIssues(ctx, "all", "All issues", nil, []Tab{{Name: "All issues"}}, false)
	}
	id := linear.Filter{TeamID: team.Id}
	active, backlog := id, id
	active.StateTypes = []string{"unstarted", "started"}
	backlog.StateTypes = []string{"backlog"}
	return NewIssues(ctx, "team", team.Name, team, []Tab{
		{Name: "All issues", Filter: id}, {Name: "Active", Filter: active}, {Name: "Backlog", Filter: backlog},
	}, false)
}

// CycleIssues shows the team's current or upcoming cycle.
func CycleIssues(ctx *common.Ctx, team *linear.Team, next bool) *Issues {
	f := linear.Filter{TeamID: team.Id, CycleActive: !next, CycleNext: next}
	name := "Current cycle"
	if next {
		name = "Upcoming cycle"
	}
	return NewIssues(ctx, "cycle", team.Name+" › "+name, team, []Tab{{Name: name, Filter: f}}, true)
}

func (v *Issues) build() tea.Cmd {
	f := v.tabs[v.tab].Filter
	if v.board {
		v.child = NewBoard(v.ctx, v.team, f, v.title)
	} else {
		l := NewIssueList(v.ctx, v.title, f, nil).WithGroup(GroupState)
		if len(f.StateTypes) == 0 {
			l.WithChip("hide old done", func(f *linear.Filter) { f.RecentDone = true })
		}
		v.child = l
	}
	v.resize()
	return v.child.Init()
}

func (v *Issues) Init() tea.Cmd { return v.build() }

func (v *Issues) resize() {
	if v.child != nil {
		v.child.SetSize(v.w, max(1, v.h-2))
	}
}

func (v *Issues) SetSize(w, h int) { v.w, v.h = w, h; v.resize() }
func (v *Issues) Title() string    { return v.title }
func (v *Issues) Capturing() bool  { return v.child != nil && v.child.Capturing() }

func (v *Issues) Help() []key.Binding {
	b := []key.Binding{layoutKey}
	if len(v.tabs) > 1 {
		b = append(b, nextTabKey)
	}
	return append(b, v.child.Help()...)
}

func (v *Issues) Defaults() common.IssueDefaults {
	if d, ok := v.child.(interface{ Defaults() common.IssueDefaults }); ok {
		return d.Defaults()
	}
	return common.IssueDefaults{}
}

func (v *Issues) Selection() []linear.Issue {
	if s, ok := v.child.(interface{ Selection() []linear.Issue }); ok {
		return s.Selection()
	}
	return nil
}

func (v *Issues) HandlesEsc() bool {
	h, ok := v.child.(interface{ HandlesEsc() bool })
	return ok && h.HandlesEsc()
}

func (v *Issues) switchTab(i int) tea.Cmd {
	v.tab = (i + len(v.tabs)) % len(v.tabs)
	return v.build()
}

func (v *Issues) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if v.child.Capturing() {
			break
		}
		switch {
		case key.Matches(msg, layoutKey):
			v.board = !v.board
			if v.ctx.State.Layouts == nil {
				v.ctx.State.Layouts = map[string]string{}
			}
			v.ctx.State.Layouts[v.id] = map[bool]string{true: "board", false: "list"}[v.board]
			return v.build()
		case key.Matches(msg, nextTabKey) && len(v.tabs) > 1:
			return v.switchTab(v.tab + 1)
		case key.Matches(msg, prevTabKey) && len(v.tabs) > 1:
			return v.switchTab(v.tab - 1)
		}
	case common.ClickMsg:
		if msg.Y == 0 {
			for i, r := range v.tabX {
				if msg.X >= r[0] && msg.X < r[1] {
					return v.switchTab(i)
				}
			}
			if msg.X >= v.w-16 {
				return v.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
			}
			return nil
		}
		msg.Y -= 2
		return v.child.Update(msg)
	}
	return v.child.Update(msg)
}

func (v *Issues) header() string {
	t := v.ctx.Theme
	var parts []string
	v.tabX = v.tabX[:0]
	x := 0
	for i, tb := range v.tabs {
		var p string
		if i == v.tab {
			p = theme.Pill(t.Bold.Render(tb.Name), t.ChipFocusBg)
		} else {
			p = theme.Pill(t.Dim.Render(tb.Name), t.ChipBg)
		}
		v.tabX = append(v.tabX, [2]int{x, x + lipgloss.Width(p)})
		x += lipgloss.Width(p) + 1
		parts = append(parts, p)
	}
	left := strings.Join(parts, " ")
	list, board := t.Dim.Render("≡"), t.Dim.Render("▥")
	if v.board {
		board = theme.Pill(t.Bold.Render("▥"), t.ChipFocusBg)
		list = theme.Pill(list, t.ChipBg)
	} else {
		list = theme.Pill(t.Bold.Render("≡"), t.ChipFocusBg)
		board = theme.Pill(board, t.ChipBg)
	}
	right := t.Faint.Render("ctrl+b ") + list + board
	return common.Fit(left, v.w-lipgloss.Width(right)) + right
}

func (v *Issues) View() string {
	t := v.ctx.Theme
	rule := t.Fgc(t.Border, strings.Repeat("─", v.w))
	return v.header() + "\n" + rule + "\n" + v.child.View()
}
