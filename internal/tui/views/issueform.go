package views

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

// IssueForm creates or edits an issue with a huh form.
type IssueForm struct {
	ctx  *common.Ctx
	orig *linear.Issue
	desc string
	v    formValues

	teams    []linear.Team
	labels   []linear.Label
	projects []linear.Project
	cyclesMu sync.Mutex
	cycles   map[string][]linear.Cycle

	form    *huh.Form
	spin    spinner.Model
	loading bool
	saving  bool
	err     error
	w, h    int
}

type formValues struct {
	TeamID, Title, Description, StateID, AssigneeID, CycleID, ProjectID, Due, Parent string
	Priority                                                                         int
	Estimate                                                                         int
	Labels                                                                           []string
}

type formDataMsg struct {
	owner    *IssueForm
	teams    []linear.Team
	labels   []linear.Label
	projects []linear.Project
	cycles   []linear.Cycle
	desc     *string
	parent   string
	err      error
}

type formSavedMsg struct {
	owner *IssueForm
	issue *linear.Issue
	err   error
}

// NewIssueForm opens a create form with defaults.
func NewIssueForm(ctx *common.Ctx, d common.IssueDefaults) *IssueForm {
	f := newForm(ctx)
	f.v = formValues{TeamID: d.TeamID, StateID: d.StateID, CycleID: d.CycleID, ProjectID: d.ProjectID, AssigneeID: d.Assignee, Estimate: -1}
	if d.ParentID != "" {
		f.v.Parent = d.ParentID
	}
	if f.v.TeamID == "" && ctx.Team != nil {
		f.v.TeamID = ctx.Team.Id
	}
	return f
}

// EditIssueForm edits iss; desc is the current description if known.
func EditIssueForm(ctx *common.Ctx, iss linear.Issue, desc string) *IssueForm {
	f := newForm(ctx)
	f.orig, f.desc = &iss, desc
	f.v = formValues{
		TeamID: iss.Team.Id, Title: iss.Title, Description: desc, StateID: iss.State.Id,
		Priority: int(iss.Priority), Estimate: -1, Due: iss.DueDate,
	}
	if iss.Estimate != 0 {
		f.v.Estimate = int(iss.Estimate)
	}
	if iss.Assignee != nil {
		f.v.AssigneeID = iss.Assignee.Id
	}
	if iss.Cycle != nil {
		f.v.CycleID = iss.Cycle.Id
	}
	if iss.Project != nil {
		f.v.ProjectID = iss.Project.Id
	}
	for _, l := range iss.Labels.Nodes {
		f.v.Labels = append(f.v.Labels, l.Id)
	}
	return f
}

func newForm(ctx *common.Ctx) *IssueForm {
	return &IssueForm{ctx: ctx, loading: true, cycles: map[string][]linear.Cycle{},
		spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(ctx.Theme.Cursor))}
}

func (f *IssueForm) Title() string {
	if f.orig != nil {
		return "Edit " + f.orig.Identifier
	}
	return "New issue"
}

func (f *IssueForm) Capturing() bool { return true }

func (f *IssueForm) SetSize(w, h int) {
	f.w, f.h = w, h
	if f.form != nil {
		f.form.WithWidth(min(w, 100)).WithHeight(h - 2)
	}
}

func (f *IssueForm) Help() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
		key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "$EDITOR")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

func (f *IssueForm) Init() tea.Cmd {
	needDesc := f.orig != nil && f.desc == ""
	id := ""
	if f.orig != nil {
		id = f.orig.Id
	}
	teamID, parent := f.v.TeamID, f.v.Parent
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		msg := formDataMsg{owner: f}
		svc := f.ctx.Svc
		if msg.teams, msg.err = svc.Teams(bg()); msg.err != nil {
			return msg
		}
		if teamID == "" && len(msg.teams) > 0 {
			teamID = msg.teams[0].Id
		}
		if msg.labels, msg.err = svc.Labels(bg()); msg.err != nil {
			return msg
		}
		if msg.projects, msg.err = svc.Projects(bg()); msg.err != nil {
			return msg
		}
		if teamID != "" {
			msg.cycles, _ = svc.Cycles(bg(), teamID)
			_, _ = svc.Members(bg(), teamID)
		}
		if needDesc {
			d, err := svc.Issue(bg(), id)
			if err != nil {
				msg.err = err
				return msg
			}
			msg.desc = &d.Description
		}
		if parent != "" && id == "" {
			if d, err := svc.Issue(bg(), parent); err == nil {
				msg.parent = d.Identifier
			}
		}
		return msg
	})
}

func (f *IssueForm) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !f.loading && !f.saving {
			return nil
		}
		var cmd tea.Cmd
		f.spin, cmd = f.spin.Update(msg)
		return cmd
	case formDataMsg:
		if msg.owner != f {
			return nil
		}
		f.loading = false
		if msg.err != nil {
			f.err = msg.err
			return nil
		}
		f.teams, f.labels, f.projects = msg.teams, msg.labels, msg.projects
		if f.v.TeamID == "" && len(f.teams) > 0 {
			f.v.TeamID = f.teams[0].Id
		}
		f.cycles[f.v.TeamID] = msg.cycles
		if msg.desc != nil {
			f.desc, f.v.Description = *msg.desc, *msg.desc
		}
		if f.orig == nil {
			f.v.Parent = msg.parent
		}
		return f.build()
	case formSavedMsg:
		if msg.owner != f {
			return nil
		}
		f.saving = false
		if msg.err != nil {
			f.err = msg.err
			return f.build()
		}
		if f.orig != nil {
			return tea.Batch(common.Cmd(common.PopMsg{}), common.Cmd(common.IssueUpdatedMsg{Issue: *msg.issue}),
				common.Cmd(common.ReloadIssueMsg{ID: msg.issue.Id}), common.Toast("Updated "+msg.issue.Identifier))
		}
		return tea.Batch(common.Cmd(common.PopMsg{}), common.Cmd(common.IssueCreatedMsg{Issue: *msg.issue}))
	case tea.KeyPressMsg:
		if msg.String() == "esc" {
			return common.Cmd(common.PopMsg{})
		}
		if f.form == nil {
			return nil
		}
	}
	if f.form == nil || f.saving {
		return nil
	}
	m, cmd := f.form.Update(msg)
	f.form = m.(*huh.Form)
	switch f.form.State {
	case huh.StateCompleted:
		f.form = nil
		return tea.Batch(cmd, f.save())
	case huh.StateAborted:
		return common.Cmd(common.PopMsg{})
	}
	return cmd
}

func (f *IssueForm) team() *linear.Team {
	for i := range f.teams {
		if f.teams[i].Id == f.v.TeamID {
			return &f.teams[i]
		}
	}
	return nil
}

// teamMembers is served from the service cache, which Init warms for the
// initial team.
func (f *IssueForm) teamMembers() []linear.User {
	m, _ := f.ctx.Svc.Members(bg(), f.v.TeamID)
	return m
}

func (f *IssueForm) teamCycles() []linear.Cycle {
	f.cyclesMu.Lock()
	c, ok := f.cycles[f.v.TeamID]
	f.cyclesMu.Unlock()
	if ok {
		return c
	}
	c, _ = f.ctx.Svc.Cycles(bg(), f.v.TeamID)
	f.cyclesMu.Lock()
	f.cycles[f.v.TeamID] = c
	f.cyclesMu.Unlock()
	return c
}

func (f *IssueForm) build() tea.Cmd {
	var teamOpts []huh.Option[string]
	for _, t := range f.teams {
		teamOpts = append(teamOpts, huh.NewOption(t.Name+" ("+t.Key+")", t.Id))
	}
	var first []huh.Field
	if f.orig == nil {
		first = append(first, huh.NewSelect[string]().Title("Team").Options(teamOpts...).Value(&f.v.TeamID))
	}
	first = append(first,
		huh.NewInput().Title("Title").Placeholder("Issue title").Value(&f.v.Title).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("title is required")
				}
				return nil
			}),
		huh.NewText().Title("Description").Description("markdown · ctrl+e opens $EDITOR").Lines(8).
			ExternalEditor(true).Editor(strings.Fields(f.ctx.Cfg.EditorCommand())...).EditorExtension("md").
			Value(&f.v.Description),
	)

	states := huh.NewSelect[string]().Title("Status").Value(&f.v.StateID).OptionsFunc(func() []huh.Option[string] {
		opts := []huh.Option[string]{huh.NewOption("Default", "")}
		if t := f.team(); t != nil {
			for _, s := range t.States.Nodes {
				opts = append(opts, huh.NewOption(s.Name, s.Id))
			}
		}
		return opts
	}, &f.v.TeamID)

	var prioOpts []huh.Option[int]
	for p := range 5 {
		prioOpts = append(prioOpts, huh.NewOption(theme.PriorityName(float64(p)), p))
	}

	assignee := huh.NewSelect[string]().Title("Assignee").Value(&f.v.AssigneeID).OptionsFunc(func() []huh.Option[string] {
		opts := []huh.Option[string]{huh.NewOption("Unassigned", "")}
		for _, u := range f.teamMembers() {
			if u.Active {
				name := u.DisplayName
				if u.IsMe {
					name += " (me)"
				}
				opts = append(opts, huh.NewOption(name, u.Id))
			}
		}
		return opts
	}, &f.v.TeamID)

	labels := huh.NewMultiSelect[string]().Title("Labels").Filterable(true).Height(8).Value(&f.v.Labels).OptionsFunc(func() []huh.Option[string] {
		var opts []huh.Option[string]
		for _, l := range f.labels {
			if l.Team == nil || l.Team.Id == f.v.TeamID {
				opts = append(opts, huh.NewOption(l.Name, l.Id).Selected(slices.Contains(f.v.Labels, l.Id)))
			}
		}
		return opts
	}, &f.v.TeamID)

	estimate := huh.NewSelect[int]().Title("Estimate").Value(&f.v.Estimate).OptionsFunc(func() []huh.Option[int] {
		return estimateOptions(f.team())
	}, &f.v.TeamID)

	cycle := huh.NewSelect[string]().Title("Cycle").Value(&f.v.CycleID).OptionsFunc(func() []huh.Option[string] {
		opts := []huh.Option[string]{huh.NewOption("No cycle", "")}
		for _, c := range f.teamCycles() {
			if !c.IsPast || c.Id == f.v.CycleID {
				opts = append(opts, huh.NewOption(CycleName(c.Number, c.Name), c.Id))
			}
		}
		return opts
	}, &f.v.TeamID)

	projOpts := []huh.Option[string]{huh.NewOption("No project", "")}
	for _, p := range f.projects {
		projOpts = append(projOpts, huh.NewOption(p.Name, p.Id))
	}

	props := []huh.Field{
		states,
		huh.NewSelect[int]().Title("Priority").Options(prioOpts...).Value(&f.v.Priority),
		assignee,
		labels,
		estimate,
		cycle,
		huh.NewSelect[string]().Title("Project").Options(projOpts...).Height(8).Value(&f.v.ProjectID),
		huh.NewInput().Title("Due date").Placeholder("YYYY-MM-DD").Value(&f.v.Due).Validate(func(s string) error {
			if s == "" {
				return nil
			}
			_, err := time.Parse(time.DateOnly, s)
			if err != nil {
				return errors.New("use YYYY-MM-DD")
			}
			return nil
		}),
		huh.NewInput().Title("Parent issue").Placeholder("e.g. ENG-12 (optional)").Value(&f.v.Parent),
	}

	f.form = huh.NewForm(
		huh.NewGroup(first...).Title(f.Title()),
		huh.NewGroup(props...).Title("Properties"),
	).WithTheme(huh.ThemeFunc(huh.ThemeCharm)).WithShowHelp(true)
	if f.w > 0 {
		f.form.WithWidth(min(f.w, 100)).WithHeight(f.h - 2)
	}
	return f.form.Init()
}

func estimateOptions(t *linear.Team) []huh.Option[int] {
	opts := []huh.Option[int]{huh.NewOption("No estimate", -1)}
	if t == nil {
		return opts
	}
	var vals []int
	names := map[int]string{}
	switch t.IssueEstimationType {
	case "notUsed":
		return opts
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
		opts = append(opts, huh.NewOption(name, v))
	}
	return opts
}

// input builds the mutation payload. For edits only changed fields are sent,
// with explicit nulls for cleared values.
func (f *IssueForm) input() (linear.Input, error) {
	v := f.v
	in := linear.Input{}
	team := f.team()
	stateOK := team != nil && slices.ContainsFunc(team.States.Nodes, func(s linear.State) bool { return s.Id == v.StateID })
	memberOK := slices.ContainsFunc(f.teamMembers(), func(u linear.User) bool { return u.Id == v.AssigneeID })
	cycleOK := slices.ContainsFunc(f.teamCycles(), func(c linear.Cycle) bool { return c.Id == v.CycleID })
	var labels []string
	for _, id := range v.Labels {
		if slices.ContainsFunc(f.labels, func(l linear.Label) bool { return l.Id == id && (l.Team == nil || l.Team.Id == v.TeamID) }) {
			labels = append(labels, id)
		}
	}
	nullable := func(s string, ok bool) any {
		if s == "" || !ok {
			return nil
		}
		return s
	}
	var parentID any
	if p := strings.TrimSpace(v.Parent); p != "" {
		d, err := f.ctx.Svc.Issue(bg(), p)
		if err != nil {
			return nil, fmt.Errorf("parent %s: %s", p, common.ErrText(err))
		}
		parentID = d.Id
	}
	estimate := any(nil)
	if v.Estimate >= 0 {
		estimate = v.Estimate
	}

	if f.orig == nil {
		in["teamId"] = v.TeamID
		in["title"] = strings.TrimSpace(v.Title)
		set := func(k string, val any) {
			if val != nil {
				in[k] = val
			}
		}
		set("description", nullable(v.Description, true))
		set("stateId", nullable(v.StateID, stateOK))
		set("assigneeId", nullable(v.AssigneeID, memberOK))
		set("cycleId", nullable(v.CycleID, cycleOK))
		set("projectId", nullable(v.ProjectID, true))
		set("dueDate", nullable(v.Due, true))
		set("parentId", parentID)
		set("estimate", estimate)
		if v.Priority != 0 {
			in["priority"] = v.Priority
		}
		if len(labels) > 0 {
			in["labelIds"] = labels
		}
		return in, nil
	}

	o := f.orig
	if t := strings.TrimSpace(v.Title); t != o.Title {
		in["title"] = t
	}
	if v.Description != f.desc {
		in["description"] = v.Description
	}
	if v.StateID != o.State.Id && stateOK {
		in["stateId"] = v.StateID
	}
	if v.Priority != int(o.Priority) {
		in["priority"] = v.Priority
	}
	origAssignee := ""
	if o.Assignee != nil {
		origAssignee = o.Assignee.Id
	}
	if v.AssigneeID != origAssignee {
		in["assigneeId"] = nullable(v.AssigneeID, memberOK)
	}
	origCycle, origProject := "", ""
	if o.Cycle != nil {
		origCycle = o.Cycle.Id
	}
	if o.Project != nil {
		origProject = o.Project.Id
	}
	if v.CycleID != origCycle {
		in["cycleId"] = nullable(v.CycleID, cycleOK)
	}
	if v.ProjectID != origProject {
		in["projectId"] = nullable(v.ProjectID, true)
	}
	if v.Due != o.DueDate {
		in["dueDate"] = nullable(v.Due, true)
	}
	origEstimate := -1
	if o.Estimate != 0 {
		origEstimate = int(o.Estimate)
	}
	if v.Estimate != origEstimate {
		in["estimate"] = estimate
	}
	var origLabels []string
	for _, l := range o.Labels.Nodes {
		origLabels = append(origLabels, l.Id)
	}
	slices.Sort(origLabels)
	slices.Sort(labels)
	if !slices.Equal(origLabels, labels) {
		in["labelIds"] = nonNil(labels)
	}
	if parentID != nil {
		in["parentId"] = parentID
	}
	return in, nil
}

func (f *IssueForm) save() tea.Cmd {
	f.saving, f.err = true, nil
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		in, err := f.input()
		if err != nil {
			return formSavedMsg{owner: f, err: err}
		}
		var iss *linear.Issue
		if f.orig == nil {
			iss, err = f.ctx.Svc.CreateIssue(bg(), in)
		} else if len(in) == 0 {
			iss = f.orig
		} else {
			iss, err = f.ctx.Svc.UpdateIssue(bg(), f.orig.Id, in)
		}
		return formSavedMsg{owner: f, issue: iss, err: err}
	})
}

func (f *IssueForm) View() string {
	t := f.ctx.Theme
	switch {
	case f.loading:
		return " " + f.spin.View() + " Loading form…"
	case f.saving:
		return " " + f.spin.View() + " Saving…"
	case f.form == nil && f.err != nil:
		return " " + t.Error.Render(common.ErrText(f.err)) + "\n " + t.Dim.Render("esc to go back")
	case f.form == nil:
		return ""
	}
	out := f.form.View()
	if f.err != nil {
		out = t.Error.Render(" "+common.ErrText(f.err)) + "\n" + out
	}
	return out
}
