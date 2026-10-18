package views

import (
	"fmt"
	"math"
	"slices"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
)

// Cycles lists a team's cycles with progress.
type Cycles struct {
	cursorList
	team   *linear.Team
	cycles []linear.Cycle
}

type cyclesLoadedMsg struct {
	owner  *Cycles
	reqID  int
	cycles []linear.Cycle
	err    error
}

func NewCycles(ctx *common.Ctx, team *linear.Team) *Cycles {
	return &Cycles{cursorList: newCursorList(ctx), team: team}
}

func (c *Cycles) Title() string {
	if c.team == nil {
		return "Cycles"
	}
	return c.team.Name + " cycles"
}

func (c *Cycles) Help() []key.Binding {
	k := c.ctx.Keys
	return []key.Binding{k.Enter, key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "board")), k.Refresh}
}

func (c *Cycles) Init() tea.Cmd { return tea.Batch(c.load(), c.spin.Tick) }

func (c *Cycles) load() tea.Cmd {
	if c.team == nil {
		c.loading = false
		return nil
	}
	c.reqID++
	req, teamID := c.reqID, c.team.Id
	return func() tea.Msg {
		cy, err := c.ctx.Svc.Cycles(bg(), teamID)
		return cyclesLoadedMsg{owner: c, reqID: req, cycles: cy, err: err}
	}
}

func (c *Cycles) current() *linear.Cycle {
	if c.cursor < len(c.cycles) {
		return &c.cycles[c.cursor]
	}
	return nil
}

func (c *Cycles) Update(msg tea.Msg) tea.Cmd {
	if cmd, ok := c.tick(msg); ok {
		return cmd
	}
	if c.nav(msg) {
		return nil
	}
	switch msg := msg.(type) {
	case cyclesLoadedMsg:
		if msg.owner != c || msg.reqID != c.reqID {
			return nil
		}
		c.loading, c.err = false, msg.err
		if msg.err != nil {
			return common.Err(msg.err)
		}
		c.cycles, c.n = msg.cycles, len(msg.cycles)
		for i, cy := range c.cycles {
			if cy.IsActive {
				c.cursor = i
			}
		}
	case common.RefreshMsg:
		return c.load()
	case common.TeamChangedMsg:
		c.team, c.cycles, c.n, c.loading, c.cursor = msg.Team, nil, 0, true, 0
		return c.load()
	case tea.KeyPressMsg:
		k := c.ctx.Keys
		cy := c.current()
		if cy == nil {
			return nil
		}
		name := CycleName(cy.Number, cy.Name)
		switch {
		case key.Matches(msg, k.Enter):
			l := NewIssueList(c.ctx, c.team.Key+" › "+name, linear.Filter{TeamID: c.team.Id, CycleID: cy.Id}, nil).WithGroup(GroupState)
			return common.Cmd(common.PushMsg{View: l})
		case msg.String() == "b":
			return common.Cmd(common.PushMsg{View: NewBoard(c.ctx, c.team, linear.Filter{CycleID: cy.Id}, name)})
		}
	}
	return nil
}

func (c *Cycles) Defaults() common.IssueDefaults {
	d := common.IssueDefaults{}
	if c.team != nil {
		d.TeamID = c.team.Id
	}
	if cy := c.current(); cy != nil {
		d.CycleID = cy.Id
	}
	return d
}

func last(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	return v[len(v)-1]
}

func (c *Cycles) View() string {
	t := c.ctx.Theme
	if c.team == nil {
		return " " + t.Dim.Render("No team selected. Use ctrl+k → Switch team.")
	}
	return c.render(nil, "This team has no cycles.", func(i int, _ bool) string {
		cy := c.cycles[i]
		status := t.Faint.Render("past    ")
		switch {
		case cy.IsActive:
			status = t.Success.Render("current ")
		case cy.IsNext:
			status = t.Cursor.Render("next    ")
		case cy.IsFuture:
			status = t.Dim.Render("upcoming")
		}
		dates := cy.StartsAt.Format("Jan 2") + " – " + cy.EndsAt.Format("Jan 2")
		extra := ""
		if cy.IsActive {
			days := int(math.Ceil(time.Until(cy.EndsAt).Hours() / 24))
			extra = t.Dim.Render(fmt.Sprintf(" · %dd left", max(0, days)))
		}
		scope := fmt.Sprintf("%g/%g", last(cy.CompletedScopeHistory), last(cy.ScopeHistory))
		return status + " " + common.Fit(t.Bold.Render(CycleName(cy.Number, cy.Name)), 18) + " " +
			common.Fit(t.Dim.Render(dates), 16) + " " + t.Progress(cy.Progress, 16) + " " + t.Dim.Render(scope) + extra
	})
}

// Projects lists workspace projects.
type Projects struct {
	cursorList
	projects []linear.Project
	team     *linear.Team
}

type projectsLoadedMsg struct {
	owner    *Projects
	reqID    int
	projects []linear.Project
	err      error
}

func NewProjects(ctx *common.Ctx) *Projects { return &Projects{cursorList: newCursorList(ctx)} }

// NewTeamProjects lists projects the team is part of.
func NewTeamProjects(ctx *common.Ctx, team *linear.Team) *Projects {
	return &Projects{cursorList: newCursorList(ctx), team: team}
}

func (p *Projects) Title() string {
	if p.team != nil {
		return p.team.Name + " › Projects"
	}
	return "Projects"
}
func (p *Projects) Help() []key.Binding {
	k := p.ctx.Keys
	return []key.Binding{k.Enter, k.Open, k.Refresh}
}
func (p *Projects) Init() tea.Cmd { return tea.Batch(p.load(), p.spin.Tick) }

func (p *Projects) load() tea.Cmd {
	p.reqID++
	req := p.reqID
	return func() tea.Msg {
		ps, err := p.ctx.Svc.Projects(bg())
		return projectsLoadedMsg{owner: p, reqID: req, projects: ps, err: err}
	}
}

func (p *Projects) Update(msg tea.Msg) tea.Cmd {
	if cmd, ok := p.tick(msg); ok {
		return cmd
	}
	if p.nav(msg) {
		return nil
	}
	switch msg := msg.(type) {
	case projectsLoadedMsg:
		if msg.owner != p || msg.reqID != p.reqID {
			return nil
		}
		p.loading, p.err = false, msg.err
		if msg.err != nil {
			return common.Err(msg.err)
		}
		p.projects = msg.projects
		if p.team != nil {
			p.projects = slices.DeleteFunc(p.projects, func(pr linear.Project) bool {
				return !slices.ContainsFunc(pr.Teams.Nodes, func(t linear.TeamRef) bool { return t.Id == p.team.Id })
			})
		}
		p.n = len(p.projects)
	case common.RefreshMsg:
		return p.load()
	case tea.KeyPressMsg:
		if p.cursor >= len(p.projects) {
			return nil
		}
		pr := p.projects[p.cursor]
		k := p.ctx.Keys
		switch {
		case key.Matches(msg, k.Enter):
			return common.Cmd(common.PushMsg{View: NewProjectDetail(p.ctx, pr)})
		case key.Matches(msg, k.Open):
			return common.OpenURL(pr.Url)
		}
	}
	return nil
}

func (p *Projects) Defaults() common.IssueDefaults {
	d := common.IssueDefaults{}
	if p.cursor < len(p.projects) {
		pr := p.projects[p.cursor]
		d.ProjectID = pr.Id
		if len(pr.Teams.Nodes) > 0 {
			d.TeamID = pr.Teams.Nodes[0].Id
		}
	}
	return d
}

func healthLabel(c *common.Ctx, h linear.ProjectUpdateHealthType) string {
	t := c.Theme
	switch h {
	case linear.ProjectUpdateHealthTypeOntrack:
		return t.Success.Render("on track")
	case linear.ProjectUpdateHealthTypeAtrisk:
		return t.Fgc(t.Yellow, "at risk")
	case linear.ProjectUpdateHealthTypeOfftrack:
		return t.Error.Render("off track")
	}
	return ""
}

func (p *Projects) View() string {
	t := p.ctx.Theme
	return p.render(nil, "No projects.", func(i int, _ bool) string {
		pr := p.projects[i]
		lead := ""
		if pr.Lead != nil {
			lead = "@" + pr.Lead.DisplayName
		}
		target := ""
		if pr.TargetDate != "" {
			target = "→ " + pr.TargetDate
		}
		row := t.Fgc(t.Hex(pr.Color), "◆") + " " + common.Fit(t.Bold.Render(pr.Name), 30) + " " +
			common.Fit(t.Fgc(t.Hex(pr.Status.Color), pr.Status.Name), 12) + " " + t.Progress(pr.Progress, 12) + " "
		if p.w >= 100 {
			row += common.Fit(t.Dim.Render(lead), 14) + " " + common.Fit(t.Dim.Render(target), 14) + " "
		}
		return row + healthLabel(p.ctx, pr.Health)
	})
}
