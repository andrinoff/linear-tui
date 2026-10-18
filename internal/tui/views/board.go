package views

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

// ponytail: the board loads at most boardMaxPages pages of issues; add
// per-column lazy loading if boards routinely exceed that.
const boardMaxPages = 5

const (
	boardColW   = 36
	boardColGap = 2
)

// Board is Linear's board layout: one column per workflow status, with
// cards. With a team, every team status is a column; otherwise (for
// example My issues across teams) columns come from the loaded issues.
type Board struct {
	ctx   *common.Ctx
	team  *linear.Team
	base  linear.Filter
	title string
	// showDone shows canceled and older completed issues. Off by default,
	// like Linear's display options.
	showDone bool

	issues  []linear.Issue
	columns []boardColumn
	col     int
	row     map[string]int
	scroll  map[string]int
	colOff  int
	hits    []cardHit

	loading bool
	err     error
	reqID   int
	spin    spinner.Model
	w, h    int
}

type boardColumn struct {
	name, typ, color string
	position         float64
	issues           []int
}

func (c boardColumn) key() string { return c.typ + "/" + c.name }

type cardHit struct{ x0, x1, y0, y1, col, row int }

type boardLoadedMsg struct {
	owner  *Board
	reqID  int
	issues []linear.Issue
	err    error
}

func NewBoard(ctx *common.Ctx, team *linear.Team, base linear.Filter, title string) *Board {
	if team != nil && base.TeamID == "" {
		base.TeamID = team.Id
	}
	return &Board{ctx: ctx, team: team, base: base, title: title, loading: true,
		row: map[string]int{}, scroll: map[string]int{},
		spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(ctx.Theme.Cursor))}
}

func (b *Board) Init() tea.Cmd {
	b.layout()
	return tea.Batch(b.load(), b.spin.Tick)
}

func (b *Board) load() tea.Cmd {
	b.reqID++
	req := b.reqID
	f := b.base
	f.RecentDone = !b.showDone
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

// layout rebuilds columns from the team's statuses and the loaded issues.
func (b *Board) layout() {
	var keep string
	if b.col < len(b.columns) {
		keep = b.columns[b.col].key()
	}
	byKey := map[string]*boardColumn{}
	var cols []*boardColumn
	add := func(s linear.State) *boardColumn {
		c := boardColumn{name: s.Name, typ: s.Type, color: s.Color, position: s.Position}
		if existing := byKey[c.key()]; existing != nil {
			return existing
		}
		byKey[c.key()] = &c
		cols = append(cols, &c)
		return &c
	}
	if b.team != nil {
		for _, s := range b.team.States.Nodes {
			if b.showDone || s.Type != "canceled" {
				add(s)
			}
		}
	}
	for i, iss := range b.issues {
		c := add(iss.State)
		c.issues = append(c.issues, i)
	}
	slices.SortStableFunc(cols, func(x, y *boardColumn) int {
		rx, ry := slices.Index(linear.StateTypeOrder, x.typ), slices.Index(linear.StateTypeOrder, y.typ)
		if rx != ry {
			return rx - ry
		}
		switch {
		case x.position < y.position:
			return -1
		case x.position > y.position:
			return 1
		}
		return 0
	})
	b.columns = b.columns[:0]
	for _, c := range cols {
		slices.SortStableFunc(c.issues, func(x, y int) int {
			return compareIssues(SortPriority, &b.issues[x], &b.issues[y])
		})
		b.columns = append(b.columns, *c)
	}
	b.col = 0
	for i, c := range b.columns {
		if c.key() == keep {
			b.col = i
		}
	}
}

func (b *Board) Title() string    { return b.title }
func (b *Board) Capturing() bool  { return false }
func (b *Board) SetSize(w, h int) { b.w, b.h = w, h }

func (b *Board) Help() []key.Binding {
	k := b.ctx.Keys
	return []key.Binding{k.Left, k.Right, k.Up, k.Down, k.MoveLeft, k.MoveRight, k.ToggleDone, k.Enter, k.State, k.Assignee}
}

func (b *Board) current() *linear.Issue {
	if b.col >= len(b.columns) {
		return nil
	}
	c := b.columns[b.col]
	if len(c.issues) == 0 {
		return nil
	}
	r := min(b.row[c.key()], len(c.issues)-1)
	return &b.issues[c.issues[r]]
}

func (b *Board) Defaults() common.IssueDefaults {
	d := common.IssueDefaults{TeamID: b.base.TeamID, CycleID: b.base.CycleID, ProjectID: b.base.ProjectID}
	if d.TeamID == "" && b.ctx.Team != nil {
		d.TeamID = b.ctx.Team.Id
	}
	if b.col < len(b.columns) && d.TeamID != "" {
		if st := b.stateFor(d.TeamID, b.columns[b.col]); st != nil {
			d.StateID = st.Id
		}
	}
	if b.base.AssignedToMe {
		if me := b.ctx.Me(); me != nil {
			d.Assignee = me.Id
		}
	}
	return d
}

func (b *Board) Selection() []linear.Issue {
	if cur := b.current(); cur != nil {
		return []linear.Issue{*cur}
	}
	return nil
}

// stateFor finds the status in teamID matching a column (by name, then type).
func (b *Board) stateFor(teamID string, c boardColumn) *linear.State {
	team, err := b.ctx.Svc.Team(bg(), teamID)
	if err != nil {
		return nil
	}
	var byType *linear.State
	for i, s := range team.States.Nodes {
		if s.Name == c.name {
			return &team.States.Nodes[i]
		}
		if s.Type == c.typ && byType == nil {
			byType = &team.States.Nodes[i]
		}
	}
	return byType
}

func (b *Board) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !b.loading {
			return nil
		}
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
		b.err, b.issues = nil, msg.issues
		b.layout()
	case common.IssueUpdatedMsg:
		for i := range b.issues {
			if b.issues[i].Id == msg.Issue.Id {
				b.issues[i] = msg.Issue
				b.layout()
				break
			}
		}
	case common.IssueRemovedMsg:
		b.issues = slices.DeleteFunc(b.issues, func(i linear.Issue) bool { return i.Id == msg.ID })
		b.layout()
	case common.IssueCreatedMsg, common.RefreshMsg:
		return b.load()
	case common.ClickMsg:
		for _, h := range b.hits {
			if msg.X >= h.x0 && msg.X < h.x1 && msg.Y >= h.y0 && msg.Y < h.y1 {
				if b.col == h.col && b.row[b.columns[h.col].key()] == h.row {
					return b.open()
				}
				b.col = h.col
				b.row[b.columns[h.col].key()] = h.row
			}
		}
	case tea.KeyPressMsg:
		return b.handleKey(msg)
	}
	return nil
}

func (b *Board) open() tea.Cmd {
	if cur := b.current(); cur != nil {
		return common.Cmd(common.PushMsg{View: NewIssueDetail(b.ctx, cur.Id, cur)})
	}
	return nil
}

func (b *Board) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := b.ctx.Keys
	if key.Matches(msg, k.ToggleDone) {
		b.showDone = !b.showDone
		b.loading = true
		b.layout()
		return b.load()
	}
	if len(b.columns) == 0 {
		return nil
	}
	c := b.columns[b.col]
	n := len(c.issues)
	switch {
	case key.Matches(msg, k.Left):
		b.col = max(0, b.col-1)
	case key.Matches(msg, k.Right):
		b.col = min(len(b.columns)-1, b.col+1)
	case key.Matches(msg, k.Down):
		b.row[c.key()] = min(max(0, n-1), b.row[c.key()]+1)
	case key.Matches(msg, k.Up):
		b.row[c.key()] = max(0, b.row[c.key()]-1)
	case key.Matches(msg, k.Top):
		b.row[c.key()] = 0
	case key.Matches(msg, k.Bottom):
		b.row[c.key()] = max(0, n-1)
	case key.Matches(msg, k.MoveLeft, k.MoveRight):
		cur := b.current()
		if cur == nil {
			return nil
		}
		target := b.col - 1
		if key.Matches(msg, k.MoveRight) {
			target = b.col + 1
		}
		if target < 0 || target >= len(b.columns) {
			return nil
		}
		st := b.stateFor(cur.Team.Id, b.columns[target])
		if st == nil {
			return common.Toast(cur.Team.Name + " has no " + b.columns[target].name + " status")
		}
		b.col = target
		b.row[b.columns[target].key()] = 0
		return Mutate(b.ctx, []linear.Issue{*cur}, linear.Input{"stateId": st.Id}, func(i *linear.Issue) { i.State = *st })
	case key.Matches(msg, k.Enter):
		return b.open()
	default:
		if cur := b.current(); cur != nil {
			cmd, _ := IssueAction(b.ctx, msg, []linear.Issue{*cur})
			return cmd
		}
	}
	return nil
}

func (b *Board) View() string {
	t := b.ctx.Theme
	switch {
	case b.loading && len(b.issues) == 0:
		return common.FitLines(" "+b.spin.View()+t.Dim.Render(" Loading issues…"), b.h)
	case b.err != nil && len(b.issues) == 0:
		return common.FitLines(" "+t.Error.Render(common.ErrText(b.err))+"\n "+t.Dim.Render("Press r to retry."), b.h)
	case len(b.columns) == 0:
		return common.FitLines("\n"+lipgloss.PlaceHorizontal(b.w, lipgloss.Center, t.Dim.Render("No issues. Press c to create one."))+"\n\n"+b.footer(), b.h)
	}

	visible := max(1, (b.w+boardColGap)/(boardColW+boardColGap))
	if b.col < b.colOff {
		b.colOff = b.col
	}
	if b.col >= b.colOff+visible {
		b.colOff = b.col - visible + 1
	}
	b.colOff = max(0, min(b.colOff, len(b.columns)-visible))
	bodyH := max(1, b.h-3)
	b.hits = b.hits[:0]
	var cols []string
	for ci := b.colOff; ci < min(len(b.columns), b.colOff+visible); ci++ {
		x0 := (ci - b.colOff) * (boardColW + boardColGap)
		if ci > b.colOff {
			cols = append(cols, strings.Repeat(" ", boardColGap))
		}
		cols = append(cols, b.renderColumn(ci, bodyH, x0))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	return common.FitLines(body, bodyH) + "\n" + b.scrollbar(visible) + "\n\n" + b.footer()
}

func (b *Board) renderColumn(ci, h, x0 int) string {
	t := b.ctx.Theme
	c := b.columns[ci]
	focusedCol := ci == b.col
	head := t.StateIcon(c.typ, c.color) + " " + t.Bold.Render(c.name) + " " + t.Dim.Render(fmt.Sprint(len(c.issues)))
	if c.typ == "completed" && !b.showDone {
		head += " " + t.Faint.Render("Past week")
	}
	right := t.Faint.Render("···  + ")
	lines := []string{" " + common.Fit(head, boardColW-lipgloss.Width(right)-1) + right, ""}

	var cards []string
	var bounds [][2]int
	cur := min(b.row[c.key()], len(c.issues)-1)
	for ri, idx := range c.issues {
		card := b.card(&b.issues[idx], boardColW, focusedCol && ri == cur)
		start := len(cards)
		cards = append(cards, strings.Split(card, "\n")...)
		bounds = append(bounds, [2]int{start, len(cards)})
	}
	if focusedCol {
		plus := lipgloss.PlaceHorizontal(boardColW, lipgloss.Center, t.Dim.Render("+"))
		cards = append(cards, b.rounded(theme.Surface(plus, boardColW, t.CardBg), boardColW, t.CardBg)...)
	}
	avail := max(1, h-len(lines))
	off := b.scroll[c.key()]
	if focusedCol && len(bounds) > 0 {
		s, e := bounds[cur][0], bounds[cur][1]
		if s < off {
			off = s
		}
		if e > off+avail {
			off = e - avail
		}
	}
	off = max(0, min(off, len(cards)-avail))
	b.scroll[c.key()] = off
	for ri, bd := range bounds {
		y0, y1 := bd[0]-off+len(lines), bd[1]-off+len(lines)
		if y1 > len(lines) && y0 < h {
			b.hits = append(b.hits, cardHit{x0: x0, x1: x0 + boardColW, y0: y0, y1: y1, col: ci, row: ri})
		}
	}
	lines = append(lines, cards[off:min(len(cards), off+avail)]...)
	for i := range lines {
		lines[i] = common.Fit(lines[i], boardColW)
	}
	return strings.Join(lines, "\n")
}

// rounded adds half-block caps above and below a painted block so it reads
// as a rounded card with half a row of padding.
func (b *Board) rounded(block string, w int, bg color.Color) []string {
	t := b.ctx.Theme
	out := []string{t.Fgc(bg, strings.Repeat("▄", w))}
	out = append(out, strings.Split(block, "\n")...)
	return append(out, t.Fgc(bg, strings.Repeat("▀", w)))
}

// card renders an issue like Linear's board card: identifier and avatar,
// status and title, property chips, and the creation date.
func (b *Board) card(iss *linear.Issue, w int, focused bool) string {
	t := b.ctx.Theme
	bg, chipBg := t.CardBg, t.ChipBg
	if focused {
		bg, chipBg = t.CardFocusBg, t.ChipFocusBg
	}
	inner := w - 4
	pad := func(s string) string { return "  " + common.Fit(s, inner) + "  " }

	id := t.Dim.Render(iss.Identifier)
	if iss.Parent != nil {
		id += t.Faint.Render(" › ") + t.Dim.Render(iss.Parent.Title)
	}
	avatar := "  "
	if iss.Assignee != nil {
		avatar = t.Avatar(iss.Assignee.DisplayName, iss.Assignee.AvatarUrl)
	}
	lines := []string{pad(common.Fit(id, inner-3) + " " + avatar)}

	titleW := inner - 2
	wrapped := strings.Split(ansi.Hardwrap(ansi.Wordwrap(iss.Title, titleW, " -/"), titleW, false), "\n")
	if len(wrapped) > 2 {
		wrapped = wrapped[:2]
		wrapped[1] = ansi.Truncate(wrapped[1]+" …", titleW, "…")
	}
	for i, l := range wrapped {
		prefix := "  "
		if i == 0 {
			prefix = t.StateIcon(iss.State.Type, iss.State.Color) + " "
		}
		lines = append(lines, pad(prefix+t.Bold.Render(l)))
	}
	for _, row := range layoutChips(b.chips(iss, chipBg), inner, 2) {
		lines = append(lines, pad(row))
	}
	lines = append(lines, pad(t.Dim.Render("Created "+shortDate(iss.CreatedAt))))
	return strings.Join(b.rounded(theme.Surface(strings.Join(lines, "\n"), w, bg), w, bg), "\n")
}

func (b *Board) chips(iss *linear.Issue, bg color.Color) []string {
	t := b.ctx.Theme
	pill := func(s string) string { return theme.Pill(s, bg) }
	chips := []string{pill(t.Priority(iss.Priority))}
	if iss.Cycle != nil {
		name := fmt.Sprintf("Cycle %d", int(iss.Cycle.Number))
		if iss.Cycle.Name != "" {
			name += ": " + iss.Cycle.Name
		}
		chips = append(chips, pill(t.Fgc(t.Accent, "↻")+" "+t.Dim.Render(name)))
	}
	if iss.Project != nil {
		chips = append(chips, pill(t.Fgc(t.Hex(iss.Project.Color), "◆")+" "+t.Dim.Render(iss.Project.Name)))
	}
	for _, l := range iss.Labels.Nodes {
		chips = append(chips, pill(t.Fgc(t.Hex(l.Color), "●")+" "+t.Dim.Render(l.Name)))
	}
	if iss.DueDate != "" {
		chips = append(chips, pill(t.Dim.Render("◷ ")+dueShort(t, iss.DueDate)))
	}
	if iss.Estimate != 0 {
		chips = append(chips, pill(t.Dim.Render(fmt.Sprintf("◇ %g", iss.Estimate))))
	}
	return chips
}

// layoutChips flows chips into at most maxRows rows of width w.
func layoutChips(chips []string, w, maxRows int) []string {
	var rows []string
	cur := ""
	for _, c := range chips {
		switch {
		case cur == "":
			cur = c
		case lipgloss.Width(cur)+1+lipgloss.Width(c) <= w:
			cur += " " + c
		default:
			rows = append(rows, cur)
			if len(rows) == maxRows {
				return rows
			}
			cur = c
		}
	}
	if cur != "" {
		rows = append(rows, cur)
	}
	return rows
}

func shortDate(t time.Time) string {
	if t.Year() != time.Now().Year() {
		return t.Format("Jan 2, 2006")
	}
	return t.Format("Jan 2")
}

func (b *Board) scrollbar(visible int) string {
	n := len(b.columns)
	if n <= visible {
		return ""
	}
	thumb := max(4, b.w*visible/n)
	start := (b.w - thumb) * b.colOff / max(1, n-visible)
	return strings.Repeat(" ", start) + b.ctx.Theme.Fgc(b.ctx.Theme.Subtle, strings.Repeat("━", thumb))
}

func (b *Board) footer() string {
	t := b.ctx.Theme
	msg := t.Dim.Render("Canceled and older completed issues hidden by display options") + "   " + t.Bold.Render("Show options") + t.Faint.Render(" .")
	if b.showDone {
		msg = t.Dim.Render("Showing all issues") + "   " + t.Bold.Render("Hide old issues") + t.Faint.Render(" .")
	}
	return lipgloss.PlaceHorizontal(b.w, lipgloss.Center, msg)
}
