package views

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/components"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

// Form focus areas.
const (
	focusTitle = iota
	focusDesc
	focusChips
)

// Property chips, in display order. chipTeam sits in the header.
const (
	chipTeam = iota
	chipStatus
	chipPriority
	chipAssignee
	chipProject
	chipLabels
	chipCycle
	chipMore
	chipCount
)

var (
	submitKey     = key.NewBinding(key.WithKeys("ctrl+s", "ctrl+enter"), key.WithHelp("ctrl+s", "save"))
	createMoreKey = key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "create more"))
	editorKey     = key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "$EDITOR"))
	nextFieldKey  = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field"))
)

type parentRef struct{ id, identifier string }

// IssueForm is Linear's new-issue dialog: a floating card with the team,
// title, description, and a row of property chips that open pickers. It
// also edits existing issues, sending only what changed.
type IssueForm struct {
	ctx  *common.Ctx
	orig *linear.Issue
	desc string

	team     *linear.Team
	state    *linear.State
	priority int
	assignee *linear.User
	labels   []linear.Label
	project  *linear.ProjectRef
	cycle    *linear.CycleRef
	estimate int
	due      string
	parent   *parentRef

	title      textinput.Model
	body       textarea.Model
	focus      int
	chip       int
	createMore bool
	saving     bool
	discard    bool
	err        error
	w          int
}

type formResolvedMsg struct {
	owner   *IssueForm
	desc    *string
	cycle   *linear.CycleRef
	project *linear.ProjectRef
	parent  *parentRef
	err     error
}

type formSavedMsg struct {
	owner *IssueForm
	issue *linear.Issue
	err   error
}

type formEditorMsg struct {
	owner *IssueForm
	text  string
	err   error
}

func newForm(ctx *common.Ctx) *IssueForm {
	t := ctx.Theme
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "Issue title"
	ts := textinput.DefaultStyles(t.Dark)
	ts.Focused.Text, ts.Blurred.Text = t.Title, t.Title
	ts.Focused.Placeholder, ts.Blurred.Placeholder = t.Faint.Bold(true), t.Faint.Bold(true)
	ti.SetStyles(ts)

	ta := textarea.New()
	ta.Prompt = ""
	ta.Placeholder = "Add description…"
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.EndOfBufferCharacter = ' '
	as := textarea.DefaultStyles(t.Dark)
	for _, st := range []*textarea.StyleState{&as.Focused, &as.Blurred} {
		st.Base, st.CursorLine, st.EndOfBuffer = lipgloss.NewStyle(), t.App, t.Faint
		st.Text, st.Placeholder = t.App, t.Faint
	}
	ta.SetStyles(as)
	ta.SetHeight(5)
	return &IssueForm{ctx: ctx, title: ti, body: ta, estimate: -1, chip: chipStatus}
}

// NewIssueForm opens the create dialog prefilled from the current context.
func NewIssueForm(ctx *common.Ctx, d common.IssueDefaults) *IssueForm {
	f := newForm(ctx)
	teamID := d.TeamID
	if teamID == "" && ctx.Team != nil {
		teamID = ctx.Team.Id
	}
	if teamID == "" && len(ctx.Teams) > 0 {
		teamID = ctx.Teams[0].Id
	}
	f.setTeam(teamID)
	if d.StateID != "" && f.team != nil {
		for i, s := range f.team.States.Nodes {
			if s.Id == d.StateID {
				f.state = &f.team.States.Nodes[i]
			}
		}
	}
	if me := ctx.Me(); me != nil && d.Assignee == me.Id {
		u := *me
		f.assignee = &u
	}
	if d.CycleID != "" {
		f.cycle = &linear.CycleRef{Id: d.CycleID}
	}
	if d.ProjectID != "" {
		f.project = &linear.ProjectRef{Id: d.ProjectID}
	}
	if d.ParentID != "" {
		f.parent = &parentRef{id: d.ParentID}
	}
	return f
}

// EditIssueForm opens the same dialog for an existing issue.
func EditIssueForm(ctx *common.Ctx, iss linear.Issue, desc string) *IssueForm {
	f := newForm(ctx)
	f.orig, f.desc = &iss, desc
	f.setTeam(iss.Team.Id)
	st := iss.State
	f.state, f.priority, f.assignee = &st, int(iss.Priority), iss.Assignee
	f.labels = slices.Clone(iss.Labels.Nodes)
	f.project, f.cycle, f.due = iss.Project, iss.Cycle, iss.DueDate
	if iss.Estimate != 0 {
		f.estimate = int(iss.Estimate)
	}
	if iss.Parent != nil {
		f.parent = &parentRef{id: iss.Parent.Id, identifier: iss.Parent.Identifier}
	}
	f.title.SetValue(iss.Title)
	f.body.SetValue(desc)
	return f
}

// setTeam switches team, keeping only properties valid across teams.
func (f *IssueForm) setTeam(id string) {
	team, err := f.ctx.Svc.Team(bg(), id)
	if err != nil {
		return
	}
	changed := f.team != nil && f.team.Id != team.Id
	f.team = team
	if changed {
		f.state, f.cycle = nil, nil
		f.labels = slices.DeleteFunc(f.labels, func(l linear.Label) bool { return l.Team != nil })
	}
	if f.state == nil && team.DefaultIssueState != nil {
		for i, s := range team.States.Nodes {
			if s.Id == team.DefaultIssueState.Id {
				f.state = &team.States.Nodes[i]
			}
		}
	}
}

func (f *IssueForm) Title() string {
	if f.orig != nil {
		return "Edit " + f.orig.Identifier
	}
	return "New issue"
}

func (f *IssueForm) Capturing() bool { return true }

func (f *IssueForm) SetSize(w, _ int) {
	f.w = min(w, 88)
	f.title.SetWidth(f.w - 6)
	f.body.SetWidth(f.w - 5)
}

func (f *IssueForm) Help() []key.Binding {
	b := []key.Binding{submitKey, nextFieldKey}
	if f.focus == focusDesc {
		b = append(b, editorKey)
	}
	if f.focus == focusChips {
		k := f.ctx.Keys
		b = append(b, key.NewBinding(key.WithKeys("enter"), key.WithHelp("←/→ enter", "set property")), k.State, k.Priority, k.Assignee)
	}
	if f.orig == nil {
		b = append(b, createMoreKey)
	}
	return append(b, key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")))
}

// Init focuses the title and resolves names for prefilled ids (cycle,
// project, parent) and, when editing, the description.
func (f *IssueForm) Init() tea.Cmd {
	cycle, project, parent := f.cycle, f.project, f.parent
	needDesc := f.orig != nil && f.desc == ""
	teamID := ""
	if f.team != nil {
		teamID = f.team.Id
	}
	resolve := func() tea.Msg {
		msg := formResolvedMsg{owner: f}
		svc := f.ctx.Svc
		if needDesc {
			d, err := svc.Issue(bg(), f.orig.Id)
			if err != nil {
				msg.err = err
				return msg
			}
			msg.desc = &d.Description
		}
		if cycle != nil && cycle.Name == "" && cycle.Number == 0 && teamID != "" {
			if cycles, err := svc.Cycles(bg(), teamID); err == nil {
				for _, c := range cycles {
					if c.Id == cycle.Id {
						msg.cycle = &linear.CycleRef{Id: c.Id, Number: c.Number, Name: c.Name}
					}
				}
			}
		}
		if project != nil && project.Name == "" {
			if ps, err := svc.Projects(bg()); err == nil {
				for _, p := range ps {
					if p.Id == project.Id {
						msg.project = &linear.ProjectRef{Id: p.Id, Name: p.Name, Color: p.Color}
					}
				}
			}
		}
		if parent != nil && parent.identifier == "" {
			if d, err := svc.Issue(bg(), parent.id); err == nil {
				msg.parent = &parentRef{id: d.Id, identifier: d.Identifier}
			}
		}
		return msg
	}
	return tea.Batch(f.title.Focus(), resolve)
}

func (f *IssueForm) setFocus(n int) tea.Cmd {
	f.focus = (n + 3) % 3
	f.title.Blur()
	f.body.Blur()
	switch f.focus {
	case focusTitle:
		return f.title.Focus()
	case focusDesc:
		return f.body.Focus()
	}
	return nil
}

func (f *IssueForm) dirty() bool {
	if f.orig != nil {
		return f.title.Value() != f.orig.Title || f.body.Value() != f.desc
	}
	return strings.TrimSpace(f.title.Value()+f.body.Value()) != ""
}

func (f *IssueForm) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case formResolvedMsg:
		if msg.owner != f {
			return nil
		}
		if msg.err != nil {
			f.err = msg.err
		}
		if msg.desc != nil {
			f.desc = *msg.desc
			f.body.SetValue(f.desc)
		}
		if msg.cycle != nil {
			f.cycle = msg.cycle
		}
		if msg.project != nil {
			f.project = msg.project
		}
		if msg.parent != nil {
			f.parent = msg.parent
		}
		return nil
	case formEditorMsg:
		if msg.owner != f {
			return nil
		}
		if msg.err != nil {
			return common.Err(msg.err)
		}
		f.body.SetValue(msg.text)
		return nil
	case formSavedMsg:
		if msg.owner != f {
			return nil
		}
		f.saving = false
		if msg.err != nil {
			f.err = msg.err
			return nil
		}
		iss := *msg.issue
		if f.orig != nil {
			return tea.Batch(common.Cmd(common.CloseModalMsg{}), common.Cmd(common.IssueUpdatedMsg{Issue: iss}),
				common.Cmd(common.ReloadIssueMsg{ID: iss.Id}), common.Toast("Updated "+iss.Identifier))
		}
		if f.createMore {
			f.title.SetValue("")
			f.body.SetValue("")
			f.discard = false
			return tea.Batch(common.Cmd(common.IssueCreatedMsg{Issue: iss}), f.setFocus(focusTitle))
		}
		return tea.Batch(common.Cmd(common.CloseModalMsg{}), common.Cmd(common.IssueCreatedMsg{Issue: iss}))
	case tea.KeyPressMsg:
		return f.handleKey(msg)
	case tea.PasteMsg:
		return f.forward(msg)
	}
	return nil
}

func (f *IssueForm) forward(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch f.focus {
	case focusTitle:
		f.title, cmd = f.title.Update(msg)
	case focusDesc:
		f.body, cmd = f.body.Update(msg)
	}
	return cmd
}

func (f *IssueForm) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if f.saving {
		return nil
	}
	switch {
	case msg.String() == "esc":
		if f.dirty() && !f.discard {
			f.discard = true
			return common.Toast("Unsaved changes. Press esc again to discard.")
		}
		return common.Cmd(common.CloseModalMsg{})
	case key.Matches(msg, submitKey):
		return f.save()
	case key.Matches(msg, createMoreKey) && f.orig == nil:
		f.createMore = !f.createMore
		return nil
	case msg.String() == "tab":
		return f.setFocus(f.focus + 1)
	case msg.String() == "shift+tab":
		return f.setFocus(f.focus - 1)
	}
	f.discard = false
	switch f.focus {
	case focusTitle:
		if msg.String() == "enter" || msg.String() == "down" {
			return f.setFocus(focusDesc)
		}
	case focusDesc:
		if key.Matches(msg, editorKey) {
			return f.openEditor()
		}
		if msg.String() == "up" && f.body.Line() == 0 {
			return f.setFocus(focusTitle)
		}
	case focusChips:
		return f.chipKey(msg)
	}
	return f.forward(msg)
}

func (f *IssueForm) chipKey(msg tea.KeyPressMsg) tea.Cmd {
	k := f.ctx.Keys
	first := chipTeam
	if f.orig != nil {
		first = chipStatus
	}
	switch {
	case msg.String() == "left" || msg.String() == "h":
		f.chip = max(first, f.chip-1)
	case msg.String() == "right" || msg.String() == "l":
		f.chip = min(chipCount-1, f.chip+1)
	case msg.String() == "up" || msg.String() == "k":
		return f.setFocus(focusDesc)
	case msg.String() == "enter" || msg.String() == "space":
		return f.openChip(f.chip)
	case key.Matches(msg, k.State):
		return f.openChip(chipStatus)
	case key.Matches(msg, k.Priority):
		return f.openChip(chipPriority)
	case key.Matches(msg, k.Assignee):
		return f.openChip(chipAssignee)
	case key.Matches(msg, k.Project):
		return f.openChip(chipProject)
	case key.Matches(msg, k.Labels):
		return f.openChip(chipLabels)
	case key.Matches(msg, k.Cycle):
		return f.openChip(chipCycle)
	}
	return nil
}

func (f *IssueForm) teamID() string {
	if f.team == nil {
		return ""
	}
	return f.team.Id
}

// openChip opens the picker for a property, like clicking a chip in Linear.
func (f *IssueForm) openChip(c int) tea.Cmd {
	ctx, teamID := f.ctx, f.teamID()
	f.chip = c
	pick := func(title string, items []components.Item, set func(components.Item)) tea.Cmd {
		return modal(components.NewPicker(ctx, title, items, func(sel []components.Item) tea.Cmd { set(sel[0]); return nil }))
	}
	load := func(title string, loader func() ([]components.Item, error), set func(components.Item)) tea.Cmd {
		return modal(components.NewPicker(ctx, title, nil, func(sel []components.Item) tea.Cmd { set(sel[0]); return nil }).Loading(loader))
	}
	switch c {
	case chipTeam:
		if f.orig != nil {
			return nil
		}
		var items []components.Item
		for _, t := range ctx.Teams {
			items = append(items, components.Item{ID: t.Id, Label: t.Name, Detail: t.Key})
		}
		return pick("Team", items, func(it components.Item) { f.setTeam(it.ID) })
	case chipStatus:
		if f.team == nil {
			return nil
		}
		cur := ""
		if f.state != nil {
			cur = f.state.Id
		}
		return pick("Status", StateItems(ctx.Theme, f.team.States.Nodes, cur), func(it components.Item) {
			s := it.Value.(linear.State)
			f.state = &s
		})
	case chipPriority:
		return pick("Priority", PriorityItems(ctx.Theme), func(it components.Item) { f.priority = it.Value.(int) })
	case chipAssignee:
		return load("Assign to", func() ([]components.Item, error) { return memberItems(ctx, teamID, true) }, func(it components.Item) {
			if it.ID == "" {
				f.assignee = nil
				return
			}
			u := it.Value.(linear.User)
			f.assignee = &u
		})
	case chipProject:
		return load("Project", func() ([]components.Item, error) { return projectItems(ctx, true) }, func(it components.Item) {
			if it.ID == "" {
				f.project = nil
				return
			}
			p := it.Value.(linear.ProjectRef)
			f.project = &p
		})
	case chipLabels:
		selected := f.labels
		return modal(components.NewPicker(ctx, "Labels (tab toggle, enter apply)", nil, func(sel []components.Item) tea.Cmd {
			f.labels = f.labels[:0]
			for _, it := range sel {
				f.labels = append(f.labels, it.Value.(linear.Label))
			}
			return nil
		}).Multi().Loading(func() ([]components.Item, error) { return labelItems(ctx, teamID, selected) }))
	case chipCycle:
		return load("Cycle", func() ([]components.Item, error) { return cycleItems(ctx, teamID, true) }, func(it components.Item) {
			if it.ID == "" {
				f.cycle = nil
				return
			}
			cy := it.Value.(linear.CycleRef)
			f.cycle = &cy
		})
	case chipMore:
		items := []components.Item{
			{ID: "estimate", Prefix: "◇", Label: "Estimate"},
			{ID: "due", Prefix: "◷", Label: "Due date"},
			{ID: "parent", Prefix: "↳", Label: "Parent issue"},
		}
		if f.due != "" {
			items = append(items, components.Item{ID: "clear-due", Prefix: "✕", Label: "Remove due date"})
		}
		if f.parent != nil {
			items = append(items, components.Item{ID: "clear-parent", Prefix: "✕", Label: "Remove parent"})
		}
		return modal(components.NewPicker(ctx, "More properties", items, func(sel []components.Item) tea.Cmd {
			return f.openMore(sel[0].ID)
		}))
	}
	return nil
}

// openMore handles the "···" chip's second step.
func (f *IssueForm) openMore(id string) tea.Cmd {
	ctx := f.ctx
	switch id {
	case "clear-due":
		f.due = ""
	case "clear-parent":
		f.parent = nil
	case "estimate":
		return modal(components.NewPicker(ctx, "Estimate", estimateItems(f.team), func(sel []components.Item) tea.Cmd {
			f.estimate = sel[0].Value.(int)
			return nil
		}))
	case "due":
		return modal(components.NewPrompt(ctx, "Due date", "YYYY-MM-DD", func(s string) tea.Cmd {
			if _, err := time.Parse(time.DateOnly, s); err != nil {
				return common.Err(errors.New("use a date like 2026-10-31"))
			}
			f.due = s
			return nil
		}))
	case "parent":
		return modal(components.NewPrompt(ctx, "Parent issue", "Identifier, e.g. ENG-12", func(ref string) tea.Cmd {
			return func() tea.Msg {
				d, err := ctx.Svc.Issue(bg(), ref)
				if err != nil {
					return common.ErrMsg{Err: err}
				}
				return formResolvedMsg{owner: f, parent: &parentRef{id: d.Id, identifier: d.Identifier}}
			}
		}))
	}
	return nil
}

func estimateItems(t *linear.Team) []components.Item {
	items := []components.Item{{Label: "No estimate", Value: -1}}
	if t == nil {
		return items
	}
	var vals []int
	names := map[int]string{}
	switch t.IssueEstimationType {
	case "notUsed":
		return items
	case "exponential":
		vals = []int{1, 2, 4, 8, 16}
		if t.IssueEstimationExtended {
			vals = append(vals, 32, 64)
		}
	case "fibonacci":
		vals = []int{1, 2, 3, 5, 8}
		if t.IssueEstimationExtended {
			vals = append(vals, 13, 21)
		}
	case "tShirt":
		vals = []int{1, 2, 3, 5, 8}
		names = map[int]string{1: "XS", 2: "S", 3: "M", 5: "L", 8: "XL", 13: "XXL", 21: "XXXL"}
		if t.IssueEstimationExtended {
			vals = append(vals, 13, 21)
		}
	default:
		vals = []int{1, 2, 3, 4, 5}
		if t.IssueEstimationExtended {
			vals = append(vals, 6, 7)
		}
	}
	if t.IssueEstimationAllowZero {
		vals = append([]int{0}, vals...)
	}
	for _, v := range vals {
		name := names[v]
		if name == "" {
			name = fmt.Sprintf("%d points", v)
		}
		items = append(items, components.Item{ID: fmt.Sprint(v), Prefix: "◇", Label: name, Value: v})
	}
	return items
}

func (f *IssueForm) openEditor() tea.Cmd {
	cmd, path, err := components.EditorCmd(f.ctx.Cfg.EditorCommand(), f.body.Value())
	if err != nil {
		return common.Err(err)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return readEditor(f, path, err)
	})
}

// input builds the mutation payload. Edits send only changed fields, with
// explicit nulls for cleared values.
func (f *IssueForm) input() (linear.Input, error) {
	title := strings.TrimSpace(f.title.Value())
	if title == "" {
		return nil, errors.New("a title is required")
	}
	if f.team == nil {
		return nil, errors.New("pick a team")
	}
	desc := f.body.Value()
	var labelIDs []string
	for _, l := range f.labels {
		labelIDs = append(labelIDs, l.Id)
	}
	idOf := func(p *string) any {
		if p == nil {
			return nil
		}
		return *p
	}
	var stateID, assigneeID, projectID, cycleID, parentID *string
	if f.state != nil {
		stateID = &f.state.Id
	}
	if f.assignee != nil {
		assigneeID = &f.assignee.Id
	}
	if f.project != nil {
		projectID = &f.project.Id
	}
	if f.cycle != nil {
		cycleID = &f.cycle.Id
	}
	if f.parent != nil {
		parentID = &f.parent.id
	}
	estimate := any(nil)
	if f.estimate >= 0 {
		estimate = f.estimate
	}
	due := any(nil)
	if f.due != "" {
		due = f.due
	}

	in := linear.Input{}
	if f.orig == nil {
		in["teamId"], in["title"] = f.team.Id, title
		if strings.TrimSpace(desc) != "" {
			in["description"] = desc
		}
		for k, v := range map[string]any{
			"stateId": idOf(stateID), "assigneeId": idOf(assigneeID), "projectId": idOf(projectID),
			"cycleId": idOf(cycleID), "parentId": idOf(parentID), "estimate": estimate, "dueDate": due,
		} {
			if v != nil {
				in[k] = v
			}
		}
		if f.priority != 0 {
			in["priority"] = f.priority
		}
		if len(labelIDs) > 0 {
			in["labelIds"] = labelIDs
		}
		return in, nil
	}

	o := f.orig
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	if title != o.Title {
		in["title"] = title
	}
	if desc != f.desc {
		in["description"] = desc
	}
	if str(stateID) != o.State.Id && stateID != nil {
		in["stateId"] = *stateID
	}
	if f.priority != int(o.Priority) {
		in["priority"] = f.priority
	}
	origAssignee := ""
	if o.Assignee != nil {
		origAssignee = o.Assignee.Id
	}
	if str(assigneeID) != origAssignee {
		in["assigneeId"] = idOf(assigneeID)
	}
	var origProject, origCycle, origParent string
	if o.Project != nil {
		origProject = o.Project.Id
	}
	if o.Cycle != nil {
		origCycle = o.Cycle.Id
	}
	if o.Parent != nil {
		origParent = o.Parent.Id
	}
	if str(projectID) != origProject {
		in["projectId"] = idOf(projectID)
	}
	if str(cycleID) != origCycle {
		in["cycleId"] = idOf(cycleID)
	}
	if str(parentID) != origParent {
		in["parentId"] = idOf(parentID)
	}
	origEstimate := -1
	if o.Estimate != 0 {
		origEstimate = int(o.Estimate)
	}
	if f.estimate != origEstimate {
		in["estimate"] = estimate
	}
	if f.due != o.DueDate {
		in["dueDate"] = due
	}
	var origLabels []string
	for _, l := range o.Labels.Nodes {
		origLabels = append(origLabels, l.Id)
	}
	a, b := slices.Clone(origLabels), slices.Clone(labelIDs)
	slices.Sort(a)
	slices.Sort(b)
	if !slices.Equal(a, b) {
		in["labelIds"] = nonNil(labelIDs)
	}
	return in, nil
}

func (f *IssueForm) save() tea.Cmd {
	in, err := f.input()
	if err != nil {
		f.err = err
		return nil
	}
	f.saving, f.err = true, nil
	return func() tea.Msg {
		var iss *linear.Issue
		var err error
		switch {
		case f.orig == nil:
			iss, err = f.ctx.Svc.CreateIssue(bg(), in)
		case len(in) == 0:
			iss = f.orig
		default:
			iss, err = f.ctx.Svc.UpdateIssue(bg(), f.orig.Id, in)
		}
		return formSavedMsg{owner: f, issue: iss, err: err}
	}
}

// chips renders the property row like Linear: set values show their
// value, unset ones a muted placeholder.
func (f *IssueForm) chips() []string {
	t := f.ctx.Theme
	chip := func(c int, icon, label string, set bool) string {
		text := t.Dim.Render(label)
		if set {
			text = t.App.Render(label)
		}
		if icon != "" {
			text = icon + " " + text
		}
		if f.focus == focusChips && f.chip == c {
			return theme.Pill(t.Cursor.Render("›")+" "+text, t.ChipFocusBg)
		}
		return theme.Pill(text, t.ChipBg)
	}
	var out []string
	if f.state != nil {
		out = append(out, chip(chipStatus, t.StateIcon(f.state.Type, f.state.Color), f.state.Name, true))
	} else {
		out = append(out, chip(chipStatus, t.Faint.Render("◌"), "Status", false))
	}
	if f.priority != 0 {
		out = append(out, chip(chipPriority, t.Priority(float64(f.priority)), theme.PriorityName(float64(f.priority)), true))
	} else {
		out = append(out, chip(chipPriority, t.Faint.Render("···"), "Priority", false))
	}
	if f.assignee != nil {
		out = append(out, chip(chipAssignee, t.Avatar(f.assignee.DisplayName, f.assignee.AvatarUrl), f.assignee.DisplayName, true))
	} else {
		out = append(out, chip(chipAssignee, t.Faint.Render("◌"), "Assignee", false))
	}
	if f.project != nil {
		name := f.project.Name
		if name == "" {
			name = "Project"
		}
		out = append(out, chip(chipProject, t.Fgc(t.Hex(f.project.Color), "◆"), name, true))
	} else {
		out = append(out, chip(chipProject, t.Faint.Render("◆"), "Project", false))
	}
	switch len(f.labels) {
	case 0:
		out = append(out, chip(chipLabels, t.Faint.Render("⊘"), "Labels", false))
	case 1:
		out = append(out, chip(chipLabels, t.Fgc(t.Hex(f.labels[0].Color), "●"), f.labels[0].Name, true))
	default:
		dots := ""
		for _, l := range f.labels[:min(3, len(f.labels))] {
			dots += t.Fgc(t.Hex(l.Color), "●")
		}
		out = append(out, chip(chipLabels, dots, fmt.Sprintf("%d labels", len(f.labels)), true))
	}
	if f.cycle != nil {
		out = append(out, chip(chipCycle, t.Fgc(t.Accent, "↻"), CycleName(f.cycle.Number, f.cycle.Name), true))
	} else {
		out = append(out, chip(chipCycle, t.Faint.Render("↻"), "", false))
	}
	if f.estimate >= 0 {
		out = append(out, theme.Pill(t.Dim.Render("◇ ")+t.App.Render(fmt.Sprintf("%d", f.estimate)), t.ChipBg))
	}
	if f.due != "" {
		out = append(out, theme.Pill(t.Dim.Render("◷ ")+dueShort(t, f.due), t.ChipBg))
	}
	if f.parent != nil && f.parent.identifier != "" {
		out = append(out, theme.Pill(t.Dim.Render("↳ ")+t.App.Render(f.parent.identifier), t.ChipBg))
	}
	return append(out, chip(chipMore, "", "···", false))
}

func (f *IssueForm) View() string {
	t := f.ctx.Theme
	inner := f.w - 4
	teamName := "No team"
	if f.team != nil {
		teamName = f.team.Key
	}
	teamChip := theme.Pill(t.Fgc(lipgloss.Color(t.ColorFor(teamName)), "■")+" "+t.App.Render(teamName), t.ChipBg)
	if f.focus == focusChips && f.chip == chipTeam {
		teamChip = theme.Pill(t.Cursor.Render("›")+" "+t.Bold.Render(teamName), t.ChipFocusBg)
	}
	head := teamChip + t.Faint.Render("  ›  ") + t.Dim.Render(f.Title())
	closeHint := t.Faint.Render("esc ✕")
	lines := []string{
		common.Fit(head, inner-lipgloss.Width(closeHint)) + closeHint,
		"",
		f.title.View(),
		f.body.View(),
		"",
	}
	lines = append(lines, layoutChips(f.chips(), inner, 3)...)
	lines = append(lines, "", t.Fgc(t.Border, strings.Repeat("─", inner)))

	toggle := t.Faint.Render("○ Create more")
	if f.createMore {
		toggle = t.Cursor.Render("●") + t.App.Render(" Create more")
	}
	label := " Create issue "
	if f.orig != nil {
		label, toggle = " Save changes ", ""
	}
	if f.saving {
		label = " Saving… "
	}
	button := t.Badge.Render(label)
	hint := t.Faint.Render("ctrl+s save · tab next")
	if f.err != nil {
		hint = t.Error.Render(common.ErrText(f.err))
	}
	right := toggle + "  " + button
	lines = append(lines, common.Fit(hint, inner-lipgloss.Width(right)-1)+" "+right)
	return t.Box(strings.Join(lines, "\n"), f.w, t.Border)
}

func readEditor(f *IssueForm, path string, err error) tea.Msg {
	defer os.Remove(path)
	if err != nil {
		return formEditorMsg{owner: f, err: err}
	}
	b, rerr := os.ReadFile(path)
	return formEditorMsg{owner: f, text: strings.TrimRight(string(b), "\n"), err: rerr}
}
