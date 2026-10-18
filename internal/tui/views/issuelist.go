package views

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/components"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

// Source loads one page of issues for a list.
type Source func(f linear.Filter, cursor string) (linear.Page[linear.Issue], error)

type GroupBy int

const (
	GroupNone GroupBy = iota
	GroupState
	GroupAssignee
	GroupPriority
	GroupProject
	GroupMilestone
)

var groupNames = []string{"none", "status", "assignee", "priority", "project", "milestone"}

type SortBy int

const (
	SortPriority SortBy = iota
	SortUpdated
	SortCreated
	SortManual
)

var sortNames = []string{"priority", "updated", "created", "manual"}

type loadState int

const (
	stateLoading loadState = iota
	stateLoaded
	stateError
)

// IssueList is the core issue table, reused by every issue-listing screen.
type IssueList struct {
	ctx      *common.Ctx
	title    string
	source   Source
	base     linear.Filter
	chips    []chip
	defaults common.IssueDefaults
	// Filterable is false for server-defined lists such as custom views.
	filterable bool

	issues   []linear.Issue
	visible  []int
	cursor   int
	offset   int
	selected map[string]bool
	group    GroupBy
	sort     SortBy

	state   loadState
	err     error
	cursorS string
	hasMore bool
	paging  bool
	reqID   int

	search    textinput.Model
	searching bool
	spin      spinner.Model
	w, h      int
	// listTop is the screen row of the first issue row, for mouse clicks.
	listTop int
	rowMap  []int
}

type chip struct {
	label string
	apply func(*linear.Filter)
}

type issuesLoadedMsg struct {
	owner  *IssueList
	reqID  int
	page   linear.Page[linear.Issue]
	err    error
	append bool
}

func NewIssueList(ctx *common.Ctx, title string, base linear.Filter, source Source) *IssueList {
	in := textinput.New()
	in.Prompt = "/"
	in.Placeholder = "filter loaded issues"
	in.SetStyles(textinput.DefaultStyles(ctx.Theme.Dark))
	l := &IssueList{
		ctx: ctx, title: title, base: base, source: source, filterable: true,
		selected: map[string]bool{}, search: in,
		spin:     spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(ctx.Theme.Cursor)),
		defaults: common.IssueDefaults{TeamID: base.TeamID, CycleID: base.CycleID, ProjectID: base.ProjectID},
	}
	if source == nil {
		l.source = func(f linear.Filter, cursor string) (linear.Page[linear.Issue], error) {
			return ctx.Svc.Issues(bg(), f, linear.PaginationOrderByUpdatedat, cursor)
		}
	}
	return l
}

// WithGroup sets the initial grouping.
func (l *IssueList) WithGroup(g GroupBy) *IssueList { l.group = g; return l }

// Fixed disables server-side filters (for custom views and search results).
func (l *IssueList) Fixed() *IssueList { l.filterable = false; return l }

// WithChip adds a removable initial filter.
func (l *IssueList) WithChip(label string, apply func(*linear.Filter)) *IssueList {
	l.chips = append(l.chips, chip{label, apply})
	return l
}

// ActiveOnly adds a removable "active" chip hiding completed and canceled
// issues.
func (l *IssueList) ActiveOnly() *IssueList {
	return l.WithChip("status: active", func(f *linear.Filter) {
		f.StateTypes = []string{"triage", "backlog", "unstarted", "started"}
	})
}

func (l *IssueList) Init() tea.Cmd { return tea.Batch(l.load(false), l.spin.Tick) }

func (l *IssueList) Title() string   { return l.title }
func (l *IssueList) Capturing() bool { return l.searching }
func (l *IssueList) SetSize(w, h int) {
	l.w, l.h = w, h
	l.search.SetWidth(w - 4)
}

func (l *IssueList) Help() []key.Binding {
	k := l.ctx.Keys
	b := []key.Binding{k.Enter, k.Filter, k.Group, k.Sort, k.Select}
	if l.filterable {
		b = append(b, k.FilterBar, k.ClearFilter)
	}
	return append(b, k.State, k.Assignee, k.Priority)
}

func (l *IssueList) effective() linear.Filter {
	f := l.base
	for _, c := range l.chips {
		c.apply(&f)
	}
	return f
}

func (l *IssueList) load(more bool) tea.Cmd {
	l.reqID++
	id := l.reqID
	cursor := ""
	if more {
		cursor = l.cursorS
		l.paging = true
	} else if len(l.issues) == 0 {
		l.state = stateLoading
	}
	f := l.effective()
	return func() tea.Msg {
		p, err := l.source(f, cursor)
		return issuesLoadedMsg{owner: l, reqID: id, page: p, err: err, append: more}
	}
}

// Selection returns the issues targeted by an action: the multi-selection
// if any, otherwise the issue under the cursor.
func (l *IssueList) Selection() []linear.Issue {
	var out []linear.Issue
	for _, i := range l.issues {
		if l.selected[i.Id] {
			out = append(out, i)
		}
	}
	if len(out) == 0 {
		if cur := l.Current(); cur != nil {
			out = append(out, *cur)
		}
	}
	return out
}

func (l *IssueList) Current() *linear.Issue {
	if l.cursor < 0 || l.cursor >= len(l.visible) {
		return nil
	}
	return &l.issues[l.visible[l.cursor]]
}

func (l *IssueList) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		l.spin, cmd = l.spin.Update(msg)
		return cmd
	case issuesLoadedMsg:
		if msg.owner != l || msg.reqID != l.reqID {
			return nil
		}
		l.paging = false
		if msg.err != nil {
			if len(l.issues) == 0 {
				l.state, l.err = stateError, msg.err
			}
			return common.Cmd(common.ErrMsg{Err: msg.err})
		}
		l.state, l.err = stateLoaded, nil
		var keep string
		if cur := l.Current(); cur != nil {
			keep = cur.Id
		}
		if msg.append {
			l.issues = append(l.issues, msg.page.Items...)
		} else {
			l.issues = msg.page.Items
		}
		l.cursorS, l.hasMore = msg.page.Cursor, msg.page.HasMore
		l.rebuild(keep)
		return nil
	case common.IssueUpdatedMsg:
		for i := range l.issues {
			if l.issues[i].Id == msg.Issue.Id {
				l.issues[i] = msg.Issue
				keep := ""
				if cur := l.Current(); cur != nil {
					keep = cur.Id
				}
				l.rebuild(keep)
				break
			}
		}
		return nil
	case common.IssueRemovedMsg:
		l.issues = slices.DeleteFunc(l.issues, func(i linear.Issue) bool { return i.Id == msg.ID })
		delete(l.selected, msg.ID)
		l.rebuild("")
		return nil
	case common.IssueCreatedMsg, common.RefreshMsg:
		return l.load(false)
	case common.ClickMsg:
		if row := msg.Y - l.listTop; row >= 0 && row < len(l.rowMap) && l.rowMap[row] >= 0 {
			if l.cursor == l.rowMap[row] {
				return l.open()
			}
			l.cursor = l.rowMap[row]
		}
		return nil
	case tea.KeyPressMsg:
		return l.handleKey(msg)
	case tea.PasteMsg:
		if !l.searching {
			return nil
		}
		var cmd tea.Cmd
		l.search, cmd = l.search.Update(msg)
		l.cursor = 0
		l.rebuild("")
		return cmd
	}
	return nil
}

func (l *IssueList) open() tea.Cmd {
	if cur := l.Current(); cur != nil {
		return common.Cmd(common.PushMsg{View: NewIssueDetail(l.ctx, cur.Id, cur)})
	}
	return nil
}

func (l *IssueList) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := l.ctx.Keys
	if l.searching {
		switch msg.String() {
		case "esc":
			l.searching = false
			l.search.SetValue("")
			l.search.Blur()
			l.rebuild("")
			return nil
		case "enter", "down", "up":
			l.searching = false
			l.search.Blur()
			return nil
		}
		var cmd tea.Cmd
		l.search, cmd = l.search.Update(msg)
		l.cursor = 0
		l.rebuild("")
		return cmd
	}
	n := len(l.visible)
	switch {
	case key.Matches(msg, k.Down):
		l.cursor = min(n-1, l.cursor+1)
	case key.Matches(msg, k.Up):
		l.cursor = max(0, l.cursor-1)
	case key.Matches(msg, k.PageDown):
		l.cursor = min(n-1, l.cursor+l.h/2)
	case key.Matches(msg, k.PageUp):
		l.cursor = max(0, l.cursor-l.h/2)
	case key.Matches(msg, k.Top):
		l.cursor = 0
	case key.Matches(msg, k.Bottom):
		l.cursor = n - 1
	case key.Matches(msg, k.Enter):
		return l.open()
	case key.Matches(msg, k.Filter):
		l.searching = true
		return l.search.Focus()
	case key.Matches(msg, k.Select):
		if cur := l.Current(); cur != nil {
			l.selected[cur.Id] = !l.selected[cur.Id]
			if !l.selected[cur.Id] {
				delete(l.selected, cur.Id)
			}
			l.cursor = min(n-1, l.cursor+1)
		}
	case msg.String() == "esc" && len(l.selected) > 0:
		l.selected = map[string]bool{}
	case key.Matches(msg, k.Group):
		l.group = (l.group + 1) % GroupBy(len(groupNames))
		l.rebuild(l.currentID())
		return common.Toast("Group by " + groupNames[l.group])
	case key.Matches(msg, k.Sort):
		l.sort = (l.sort + 1) % SortBy(len(sortNames))
		l.rebuild(l.currentID())
		return common.Toast("Sort by " + sortNames[l.sort])
	case key.Matches(msg, k.FilterBar) && l.filterable:
		return modal(l.filterPicker())
	case key.Matches(msg, k.ClearFilter) && l.filterable:
		l.chips = nil
		return l.load(false)
	default:
		if cmd, ok := IssueAction(l.ctx, msg, l.Selection()); ok {
			if key.Matches(msg, k.Archive, k.Delete, k.State, k.Assignee, k.Priority, k.Labels, k.Cycle, k.Project, k.AssignMe) {
				l.selected = map[string]bool{}
			}
			return cmd
		}
		return nil
	}
	if l.hasMore && !l.paging && l.cursor >= n-10 {
		return l.load(true)
	}
	return nil
}

// Defaults prefill new issues created from this list.
func (l *IssueList) Defaults() common.IssueDefaults {
	d := l.defaults
	if d.TeamID == "" {
		if cur := l.Current(); cur != nil {
			d.TeamID = cur.Team.Id
		} else if l.ctx.Team != nil {
			d.TeamID = l.ctx.Team.Id
		}
	}
	return d
}

// HandlesEsc reports whether esc clears local state instead of going back.
func (l *IssueList) HandlesEsc() bool { return len(l.selected) > 0 }

func (l *IssueList) currentID() string {
	if cur := l.Current(); cur != nil {
		return cur.Id
	}
	return ""
}

// rebuild recomputes visible rows (text filter, sorting, grouping) and
// restores the cursor to keepID if it is still visible.
func (l *IssueList) rebuild(keepID string) {
	q := strings.ToLower(strings.TrimSpace(l.search.Value()))
	l.visible = l.visible[:0]
	for i, iss := range l.issues {
		if q != "" && !strings.Contains(strings.ToLower(iss.Identifier+" "+iss.Title), q) {
			continue
		}
		l.visible = append(l.visible, i)
	}
	slices.SortStableFunc(l.visible, func(a, b int) int {
		x, y := &l.issues[a], &l.issues[b]
		if c := cmp.Compare(groupRank(l.group, x), groupRank(l.group, y)); c != 0 {
			return c
		}
		if c := cmp.Compare(groupKey(l.group, x), groupKey(l.group, y)); c != 0 {
			return c
		}
		return compareIssues(l.sort, x, y)
	})
	if keepID != "" {
		for i, idx := range l.visible {
			if l.issues[idx].Id == keepID {
				l.cursor = i
				return
			}
		}
	}
	l.cursor = max(0, min(l.cursor, len(l.visible)-1))
}

func priorityRank(p float64) int {
	if p == 0 {
		return 5
	}
	return int(p)
}

func compareIssues(s SortBy, x, y *linear.Issue) int {
	switch s {
	case SortPriority:
		if c := cmp.Compare(priorityRank(x.Priority), priorityRank(y.Priority)); c != 0 {
			return c
		}
		return y.UpdatedAt.Compare(x.UpdatedAt)
	case SortCreated:
		return y.CreatedAt.Compare(x.CreatedAt)
	case SortManual:
		return cmp.Compare(x.SortOrder, y.SortOrder)
	}
	return y.UpdatedAt.Compare(x.UpdatedAt)
}

func groupRank(g GroupBy, i *linear.Issue) float64 {
	switch g {
	case GroupState:
		return float64(slices.Index(linear.StateTypeOrder, i.State.Type))*1000 + i.State.Position
	case GroupPriority:
		return float64(priorityRank(i.Priority))
	case GroupAssignee:
		if i.Assignee == nil {
			return 1
		}
	case GroupProject:
		if i.Project == nil {
			return 1
		}
	case GroupMilestone:
		if i.ProjectMilestone == nil {
			return math.MaxFloat64
		}
		return i.ProjectMilestone.SortOrder
	}
	return 0
}

func groupKey(g GroupBy, i *linear.Issue) string {
	switch g {
	case GroupState:
		return i.State.Name
	case GroupAssignee:
		if i.Assignee != nil {
			return i.Assignee.DisplayName
		}
		return "Unassigned"
	case GroupPriority:
		return theme.PriorityName(i.Priority)
	case GroupProject:
		if i.Project != nil {
			return i.Project.Name
		}
		return "No project"
	case GroupMilestone:
		if i.ProjectMilestone != nil {
			return i.ProjectMilestone.Name
		}
		return "No milestone"
	}
	return ""
}

// groupHeader renders Linear's group band: icon, name, count, and "+".
func (l *IssueList) groupHeader(i *linear.Issue, count int) string {
	t := l.ctx.Theme
	icon := ""
	switch l.group {
	case GroupState:
		icon = t.StateIcon(i.State.Type, i.State.Color) + " "
	case GroupPriority:
		icon = t.Priority(i.Priority) + " "
	case GroupAssignee:
		name, url := "", ""
		if i.Assignee != nil {
			name, url = i.Assignee.DisplayName, i.Assignee.AvatarUrl
		}
		icon = t.Avatar(name, url) + " "
	case GroupProject:
		if i.Project != nil {
			icon = t.Fgc(t.Hex(i.Project.Color), "◆") + " "
		}
	}
	left := "  " + icon + t.Group.Render(groupKey(l.group, i)) + " " + t.Dim.Render(fmt.Sprint(count))
	right := t.Faint.Render("+  ")
	return theme.Surface(common.Fit(left, l.w-lipgloss.Width(right))+right, l.w, t.CardBg)
}

// row renders an issue like Linear's list: priority, identifier, status,
// title, then right-aligned chips, assignee, and date.
func (l *IssueList) row(iss *linear.Issue, idW int, selected bool) string {
	t := l.ctx.Theme
	mark := "  "
	if selected {
		mark = t.Cursor.Render("☑ ")
	} else if len(l.selected) > 0 {
		mark = t.Faint.Render("☐ ")
	}
	left := mark + t.Priority(iss.Priority) + "  " + common.Fit(t.Dim.Render(iss.Identifier), idW) + "  " +
		t.StateIcon(iss.State.Type, iss.State.Color) + "  " + t.Bold.Render(iss.Title)
	if iss.Parent != nil && l.w >= 100 {
		left += t.Faint.Render(" › ") + t.Dim.Render(iss.Parent.Title)
	}
	var right []string
	if l.w >= 90 {
		var chips []string
		if iss.Project != nil {
			chips = append(chips, theme.Pill(t.Fgc(t.Hex(iss.Project.Color), "◆")+" "+t.Dim.Render(iss.Project.Name), t.ChipBg))
		}
		for _, lb := range iss.Labels.Nodes {
			chips = append(chips, theme.Pill(t.Fgc(t.Hex(lb.Color), "●")+" "+t.Dim.Render(lb.Name), t.ChipBg))
		}
		if iss.DueDate != "" {
			chips = append(chips, theme.Pill(t.Dim.Render("◷ ")+dueShort(t, iss.DueDate), t.ChipBg))
		}
		budget := l.w / 3
		used := 0
		for _, c := range chips {
			if used+lipgloss.Width(c)+1 > budget {
				break
			}
			right = append(right, c)
			used += lipgloss.Width(c) + 1
		}
	}
	avatar := t.Faint.Render("◌ ")
	if iss.Assignee != nil {
		avatar = t.Avatar(iss.Assignee.DisplayName, iss.Assignee.AvatarUrl)
	}
	right = append(right, avatar)
	if l.w >= 70 {
		right = append(right, t.Dim.Render(fmt.Sprintf("%6s", shortDate(iss.CreatedAt))))
	}
	r := strings.Join(right, " ") + "  "
	return common.Fit(left, l.w-lipgloss.Width(r)-1) + " " + r
}

func labelsInline(t *theme.Theme, labels []linear.Label) string {
	var parts []string
	for _, lb := range labels {
		parts = append(parts, t.Fgc(t.Hex(lb.Color), "●")+lb.Name)
	}
	return strings.Join(parts, " ")
}

func (l *IssueList) View() string {
	t := l.ctx.Theme
	var lines []string
	if len(l.chips) > 0 || len(l.selected) > 0 {
		var parts []string
		for _, c := range l.chips {
			parts = append(parts, theme.Pill(t.Dim.Render(c.label), t.ChipBg))
		}
		if len(l.selected) > 0 {
			parts = append(parts, t.Badge.Render(fmt.Sprintf("%d selected", len(l.selected))))
		}
		lines = append(lines, strings.Join(parts, " "))
	}
	switch {
	case l.state == stateLoading:
		lines = append(lines, "", "  "+l.spin.View()+" Loading issues…")
		for range 3 {
			lines = append(lines, "  "+t.Faint.Render(strings.Repeat("░", max(0, min(60, l.w-4)))))
		}
		return common.FitLines(strings.Join(lines, "\n"), l.h)
	case l.state == stateError:
		lines = append(lines, "", "  "+t.Error.Render(common.ErrText(l.err)), "  "+t.Dim.Render("Press r to retry."))
		return common.FitLines(strings.Join(lines, "\n"), l.h)
	case len(l.visible) == 0:
		msg := "No issues."
		if l.search.Value() != "" {
			msg = "No issues match “" + l.search.Value() + "”."
		} else if len(l.chips) > 0 {
			msg = "No issues match these filters. Press F to clear."
		} else {
			msg += " Press c to create one."
		}
		lines = append(lines, "", "  "+t.Dim.Render(msg))
		return common.FitLines(strings.Join(lines, "\n")+"\n"+l.footer(), l.h)
	}

	idW := 7
	for _, i := range l.issues {
		idW = max(idW, len(i.Identifier))
	}

	type row struct {
		text  string
		issue int
	}
	var rows []row
	cursorRow := 0
	for vi, idx := range l.visible {
		iss := &l.issues[idx]
		if l.group != GroupNone && (vi == 0 || groupKey(l.group, &l.issues[l.visible[vi-1]]) != groupKey(l.group, iss)) {
			count := 0
			for _, j := range l.visible[vi:] {
				if groupKey(l.group, &l.issues[j]) != groupKey(l.group, iss) {
					break
				}
				count++
			}
			rows = append(rows, row{text: l.groupHeader(iss, count), issue: -1})
		}
		if vi == l.cursor {
			cursorRow = len(rows)
		}
		text := common.Fit(l.row(iss, idW, l.selected[iss.Id]), l.w)
		if vi == l.cursor {
			text = t.Select(text, l.w)
		}
		rows = append(rows, row{text: text, issue: vi})
	}

	listH := max(1, l.h-len(lines)-1)
	l.offset = common.Scroll(cursorRow, l.offset, listH, len(rows))
	l.listTop = len(lines)
	l.rowMap = l.rowMap[:0]
	for _, r := range rows[l.offset:min(len(rows), l.offset+listH)] {
		lines = append(lines, r.text)
		l.rowMap = append(l.rowMap, r.issue)
	}
	return common.FitLines(strings.Join(lines, "\n"), l.h-1) + "\n" + l.footer()
}

func (l *IssueList) footer() string {
	t := l.ctx.Theme
	if l.searching || l.search.Value() != "" {
		return l.search.View()
	}
	info := fmt.Sprintf("%d issues", len(l.visible))
	if l.hasMore {
		info += "+"
	}
	if l.paging {
		info += " · " + l.spin.View() + " loading more"
	}
	info += " · sort " + sortNames[l.sort]
	if l.group != GroupNone {
		info += " · grouped by " + groupNames[l.group]
	}
	return t.Faint.Render("  " + info)
}

// filterPicker offers filter categories, then their values.
func (l *IssueList) filterPicker() *components.Picker {
	ctx := l.ctx
	teamID := l.base.TeamID
	if teamID == "" && ctx.Team != nil {
		teamID = ctx.Team.Id
	}
	add := func(c chip) tea.Cmd {
		l.chips = append(l.chips, c)
		return l.load(false)
	}
	cats := []components.Item{
		{ID: "status", Label: "Status type"},
		{ID: "assignee", Label: "Assignee"},
		{ID: "me", Label: "Assigned to me"},
		{ID: "unassigned", Label: "Unassigned"},
		{ID: "priority", Label: "Priority"},
		{ID: "label", Label: "Label"},
		{ID: "project", Label: "Project"},
		{ID: "cycle", Label: "Cycle"},
		{ID: "due", Label: "Has due date"},
	}
	values := func(title string, load func() ([]components.Item, error), apply func([]components.Item) chip) tea.Cmd {
		return modal(components.NewPicker(ctx, title, nil, func(sel []components.Item) tea.Cmd {
			if len(sel) == 0 {
				return nil
			}
			return add(apply(sel))
		}).Multi().Loading(load))
	}
	names := func(sel []components.Item) string {
		var n []string
		for _, s := range sel {
			n = append(n, s.Label)
		}
		return strings.Join(n, ", ")
	}
	ids := func(sel []components.Item) []string {
		var n []string
		for _, s := range sel {
			n = append(n, s.ID)
		}
		return n
	}
	return components.NewPicker(ctx, "Add filter", cats, func(sel []components.Item) tea.Cmd {
		switch sel[0].ID {
		case "status":
			return values("Status type", func() ([]components.Item, error) {
				var items []components.Item
				for _, s := range linear.StateTypeOrder {
					items = append(items, components.Item{ID: s, Prefix: ctx.Theme.StateIcon(s, ""), Label: s})
				}
				return items, nil
			}, func(sel []components.Item) chip {
				v := ids(sel)
				return chip{"status: " + names(sel), func(f *linear.Filter) { f.StateTypes = v }}
			})
		case "assignee":
			if teamID == "" {
				return common.Toast("Select a team first")
			}
			return values("Assignee", func() ([]components.Item, error) { return memberItems(ctx, teamID, false) }, func(sel []components.Item) chip {
				v := ids(sel)
				return chip{"assignee: " + names(sel), func(f *linear.Filter) { f.AssigneeIDs = v }}
			})
		case "me":
			return add(chip{"assigned to me", func(f *linear.Filter) { f.AssignedToMe = true }})
		case "unassigned":
			return add(chip{"unassigned", func(f *linear.Filter) { f.Unassigned = true }})
		case "due":
			return add(chip{"has due date", func(f *linear.Filter) { f.HasDueDate = true }})
		case "priority":
			return values("Priority", func() ([]components.Item, error) { return PriorityItems(ctx.Theme), nil }, func(sel []components.Item) chip {
				var p []int
				for _, s := range sel {
					p = append(p, s.Value.(int))
				}
				return chip{"priority: " + names(sel), func(f *linear.Filter) { f.Priorities = p }}
			})
		case "label":
			return values("Label", func() ([]components.Item, error) { return labelItems(ctx, teamID, nil) }, func(sel []components.Item) chip {
				v := ids(sel)
				return chip{"label: " + names(sel), func(f *linear.Filter) { f.LabelIDs = v }}
			})
		case "project":
			return modal(components.NewPicker(ctx, "Project", nil, func(sel []components.Item) tea.Cmd {
				id := sel[0].ID
				return add(chip{"project: " + sel[0].Label, func(f *linear.Filter) { f.ProjectID = id }})
			}).Loading(func() ([]components.Item, error) { return projectItems(ctx, false) }))
		case "cycle":
			if teamID == "" {
				return common.Toast("Select a team first")
			}
			return modal(components.NewPicker(ctx, "Cycle", nil, func(sel []components.Item) tea.Cmd {
				id := sel[0].ID
				return add(chip{"cycle: " + sel[0].Label, func(f *linear.Filter) { f.CycleID = id }})
			}).Loading(func() ([]components.Item, error) { return cycleItems(ctx, teamID, false) }))
		}
		return nil
	})
}
