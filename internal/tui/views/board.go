package views

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
)

// ponytail: the board loads at most boardMaxPages pages of issues; add
// per-column lazy loading if teams routinely exceed that.
const boardMaxPages = 5

// Board is a kanban view of a team's issues by workflow state.
type Board struct {
	ctx      *common.Ctx
	team     *linear.Team
	cycleID  string
	title    string
	showDone bool

	states []linear.State
	issues []linear.Issue
	col    int
	row    []int
	colOff int
	rowOff []int

	loading bool
	err     error
	reqID   int
	spin    spinner.Model
	w, h    int
}

type boardLoadedMsg struct {
	owner  *Board
	reqID  int
	issues []linear.Issue
	err    error
}

func NewBoard(ctx *common.Ctx, team *linear.Team, cycleID, title string) *Board {
	if title == "" && team != nil {
		title = team.Name + " board"
	}
	return &Board{ctx: ctx, team: team, cycleID: cycleID, title: title, loading: true,
		spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(ctx.Theme.Cursor))}
}

func (b *Board) Init() tea.Cmd {
	b.layoutStates()
	return tea.Batch(b.load(), b.spin.Tick)
}

func (b *Board) layoutStates() {
	b.states = b.states[:0]
	if b.team == nil {
		return
	}
	for _, s := range b.team.States.Nodes {
		if !b.showDone && (s.Type == "completed" || s.Type == "canceled") {
			continue
		}
		b.states = append(b.states, s)
	}
	b.row = make([]int, len(b.states))
	b.rowOff = make([]int, len(b.states))
	b.col = min(b.col, max(0, len(b.states)-1))
}

func (b *Board) load() tea.Cmd {
	if b.team == nil {
		b.loading = false
		return nil
	}
	b.reqID++
	req := b.reqID
	f := linear.Filter{TeamID: b.team.Id, CycleID: b.cycleID}
	if !b.showDone {
		f.StateTypes = []string{"triage", "backlog", "unstarted", "started"}
	}
	return func() tea.Msg {
		var all []linear.Issue
		cursor := ""
		for range boardMaxPages {
			p, err := b.ctx.Svc.Issues(bg(), f, linear.PaginationOrderByUpdatedat, cursor)
			if err != nil {
				return boardLoadedMsg{owner: b, reqID: req, err: err}
			}
			all = append(all, p.Items...)
			if !p.HasMore {
				break
			}
			cursor = p.Cursor
		}
		return boardLoadedMsg{owner: b, reqID: req, issues: all}
	}
}

func (b *Board) Title() string    { return b.title }
func (b *Board) Capturing() bool  { return false }
func (b *Board) SetSize(w, h int) { b.w, b.h = w, h }

func (b *Board) Help() []key.Binding {
	k := b.ctx.Keys
	return []key.Binding{k.Left, k.Right, k.Up, k.Down, k.MoveLeft, k.MoveRight, k.ToggleDone, k.Enter, k.State}
}

func (b *Board) column(i int) []int {
	var out []int
	if i < 0 || i >= len(b.states) {
		return out
	}
	for idx, iss := range b.issues {
		if iss.State.Id == b.states[i].Id {
			out = append(out, idx)
		}
	}
	return out
}

func (b *Board) current() *linear.Issue {
	col := b.column(b.col)
	if len(col) == 0 {
		return nil
	}
	r := min(b.row[b.col], len(col)-1)
	return &b.issues[col[r]]
}

func (b *Board) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		b.spin, cmd = b.spin.Update(msg)
		return cmd
	case boardLoadedMsg:
		if msg.owner != b || msg.reqID != b.reqID {
			return nil
		}
		b.loading = false
		if msg.err != nil {
			b.err = msg.err
			return common.Err(msg.err)
		}
		b.err = nil
		b.issues = msg.issues
		return nil
	case common.IssueUpdatedMsg:
		for i := range b.issues {
			if b.issues[i].Id == msg.Issue.Id {
				b.issues[i] = msg.Issue
			}
		}
		return nil
	case common.IssueRemovedMsg:
		for i := range b.issues {
			if b.issues[i].Id == msg.ID {
				b.issues = append(b.issues[:i], b.issues[i+1:]...)
				break
			}
		}
		return nil
	case common.IssueCreatedMsg, common.RefreshMsg:
		return b.load()
	case common.TeamChangedMsg:
		b.team = msg.Team
		b.title = msg.Team.Name + " board"
		b.cycleID = ""
		b.loading = true
		b.issues = nil
		b.layoutStates()
		return b.load()
	case tea.KeyPressMsg:
		return b.handleKey(msg)
	}
	return nil
}

func (b *Board) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := b.ctx.Keys
	if len(b.states) == 0 {
		return nil
	}
	n := len(b.column(b.col))
	switch {
	case key.Matches(msg, k.Left):
		b.col = max(0, b.col-1)
	case key.Matches(msg, k.Right):
		b.col = min(len(b.states)-1, b.col+1)
	case key.Matches(msg, k.Down):
		b.row[b.col] = min(max(0, n-1), b.row[b.col]+1)
	case key.Matches(msg, k.Up):
		b.row[b.col] = max(0, b.row[b.col]-1)
	case key.Matches(msg, k.Top):
		b.row[b.col] = 0
	case key.Matches(msg, k.Bottom):
		b.row[b.col] = max(0, n-1)
	case key.Matches(msg, k.ToggleDone):
		b.showDone = !b.showDone
		b.loading = true
		b.layoutStates()
		return b.load()
	case key.Matches(msg, k.MoveLeft, k.MoveRight):
		cur := b.current()
		if cur == nil {
			return nil
		}
		target := b.col - 1
		if key.Matches(msg, k.MoveRight) {
			target = b.col + 1
		}
		if target < 0 || target >= len(b.states) {
			return nil
		}
		st := b.states[target]
		b.col = target
		b.row[target] = len(b.column(target))
		return Mutate(b.ctx, []linear.Issue{*cur}, linear.Input{"stateId": st.Id}, func(i *linear.Issue) { i.State = st })
	case key.Matches(msg, k.Enter):
		if cur := b.current(); cur != nil {
			return common.Cmd(common.PushMsg{View: NewIssueDetail(b.ctx, cur.Id, cur)})
		}
	default:
		if cur := b.current(); cur != nil {
			cmd, _ := IssueAction(b.ctx, msg, []linear.Issue{*cur})
			return cmd
		}
	}
	return nil
}

func (b *Board) Defaults() common.IssueDefaults {
	d := common.IssueDefaults{CycleID: b.cycleID}
	if b.team != nil {
		d.TeamID = b.team.Id
	}
	if b.col < len(b.states) {
		d.StateID = b.states[b.col].Id
	}
	return d
}

func (b *Board) Selection() []linear.Issue {
	if cur := b.current(); cur != nil {
		return []linear.Issue{*cur}
	}
	return nil
}

func (b *Board) View() string {
	t := b.ctx.Theme
	switch {
	case b.team == nil:
		return common.FitLines(" "+t.Dim.Render("No team selected. Use the command palette (ctrl+k) → Switch team."), b.h)
	case b.loading && len(b.issues) == 0:
		return common.FitLines(" "+b.spin.View()+" Loading board…", b.h)
	case b.err != nil && len(b.issues) == 0:
		return common.FitLines(" "+t.Error.Render(common.ErrText(b.err))+"\n "+t.Dim.Render("Press r to retry."), b.h)
	}
	colW := 30
	if b.w/colW > len(b.states) && len(b.states) > 0 {
		colW = max(30, b.w/len(b.states))
	}
	visible := max(1, b.w/colW)
	if b.col < b.colOff {
		b.colOff = b.col
	}
	if b.col >= b.colOff+visible {
		b.colOff = b.col - visible + 1
	}
	cardH := 3
	var cols []string
	for ci := b.colOff; ci < min(len(b.states), b.colOff+visible); ci++ {
		st := b.states[ci]
		items := b.column(ci)
		head := t.StateIcon(st.Type, st.Color) + " " + t.Bold.Render(st.Name) + " " + t.Dim.Render(fmt.Sprint(len(items)))
		if ci == b.col {
			head = t.Cursor.Render("▌") + head
		} else {
			head = " " + head
		}
		lines := []string{common.Fit(head, colW-1), ""}
		maxCards := max(1, (b.h-2)/cardH)
		r := min(b.row[ci], max(0, len(items)-1))
		b.row[ci] = r
		if r < b.rowOff[ci] {
			b.rowOff[ci] = r
		}
		if r >= b.rowOff[ci]+maxCards {
			b.rowOff[ci] = r - maxCards + 1
		}
		for ri := b.rowOff[ci]; ri < min(len(items), b.rowOff[ci]+maxCards); ri++ {
			iss := &b.issues[items[ri]]
			lines = append(lines, b.card(iss, colW-1, ci == b.col && ri == r)...)
		}
		if len(items) == 0 {
			lines = append(lines, " "+t.Faint.Render("empty"))
		}
		cols = append(cols, lipgloss.NewStyle().Width(colW).Render(strings.Join(lines, "\n")))
	}
	out := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	footer := t.Dim.Render(fmt.Sprintf(" %d issues · columns %d-%d of %d · . toggle done · </> move", len(b.issues), b.colOff+1, min(len(b.states), b.colOff+visible), len(b.states)))
	return common.FitLines(out, b.h-1) + "\n" + footer
}

func (b *Board) card(iss *linear.Issue, w int, focused bool) []string {
	t := b.ctx.Theme
	bar := t.Faint.Render("│")
	if focused {
		bar = t.Cursor.Render("┃")
	}
	who := ""
	if iss.Assignee != nil {
		who = " @" + iss.Assignee.DisplayName
	}
	l1 := bar + " " + t.Priority(iss.Priority) + " " + t.Dim.Render(iss.Identifier+who)
	l2 := bar + " " + iss.Title
	if focused {
		l1, l2 = t.Selected.Render(common.Fit(l1, w)), t.Selected.Render(common.Fit(l2, w))
	} else {
		l1, l2 = common.Fit(l1, w), common.Fit(l2, w)
	}
	return []string{l1, l2, ""}
}
