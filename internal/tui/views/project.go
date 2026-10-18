package views

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
)

// ProjectDetail shows a project's overview and its issues grouped by
// milestone. "t" toggles between the two.
type ProjectDetail struct {
	ctx      *common.Ctx
	project  linear.Project
	detail   *linear.ProjectDetail
	err      error
	issues   *IssueList
	vp       viewport.Model
	md       markdown
	overview bool
	w, h     int
}

type projectLoadedMsg struct {
	owner  *ProjectDetail
	detail *linear.ProjectDetail
	err    error
}

var toggleKey = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "overview/issues"))

func NewProjectDetail(ctx *common.Ctx, p linear.Project) *ProjectDetail {
	l := NewIssueList(ctx, p.Name, linear.Filter{ProjectID: p.Id}, nil).WithGroup(GroupMilestone)
	if len(p.Teams.Nodes) > 0 {
		l.defaults.TeamID = p.Teams.Nodes[0].Id
	}
	return &ProjectDetail{ctx: ctx, project: p, issues: l, vp: viewport.New()}
}

func (p *ProjectDetail) Init() tea.Cmd {
	return tea.Batch(p.issues.Init(), p.load())
}

func (p *ProjectDetail) load() tea.Cmd {
	id := p.project.Id
	return func() tea.Msg {
		d, err := p.ctx.Svc.Project(bg(), id)
		return projectLoadedMsg{owner: p, detail: d, err: err}
	}
}

func (p *ProjectDetail) Title() string   { return p.project.Name }
func (p *ProjectDetail) Capturing() bool { return !p.overview && p.issues.Capturing() }

func (p *ProjectDetail) SetSize(w, h int) {
	p.w, p.h = w, h
	p.issues.SetSize(w, h-2)
	p.vp.SetWidth(w)
	p.vp.SetHeight(h - 2)
	p.renderOverview()
}

func (p *ProjectDetail) Help() []key.Binding {
	if p.overview {
		return []key.Binding{toggleKey, p.ctx.Keys.Open}
	}
	return append([]key.Binding{toggleKey}, p.issues.Help()...)
}

func (p *ProjectDetail) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case projectLoadedMsg:
		if msg.owner != p {
			return nil
		}
		p.detail, p.err = msg.detail, msg.err
		if msg.detail != nil {
			p.project = msg.detail.ProjectFields
		}
		p.renderOverview()
		return nil
	case common.RefreshMsg:
		return tea.Batch(p.load(), p.issues.Update(msg))
	case tea.KeyPressMsg:
		if key.Matches(msg, toggleKey) && !p.issues.Capturing() {
			p.overview = !p.overview
			return nil
		}
		if p.overview {
			if key.Matches(msg, p.ctx.Keys.Open) {
				return common.OpenURL(p.project.Url)
			}
			var cmd tea.Cmd
			p.vp, cmd = p.vp.Update(msg)
			return cmd
		}
	case common.ClickMsg:
		msg.Y -= 2
		return p.issues.Update(msg)
	}
	return p.issues.Update(msg)
}

func (p *ProjectDetail) Defaults() common.IssueDefaults { return p.issues.Defaults() }
func (p *ProjectDetail) Selection() []linear.Issue      { return p.issues.Selection() }
func (p *ProjectDetail) HandlesEsc() bool               { return !p.overview && p.issues.HandlesEsc() }

func (p *ProjectDetail) renderOverview() {
	if p.w == 0 {
		return
	}
	t := p.ctx.Theme
	pr := p.project
	w := min(p.w-2, 110)
	var b strings.Builder
	b.WriteString(t.Fgc(t.Hex(pr.Color), "◆ ") + t.Title.Render(pr.Name) + "  " + t.Fgc(t.Hex(pr.Status.Color), pr.Status.Name) + " " + healthLabel(p.ctx, pr.Health) + "\n")
	if pr.Description != "" {
		b.WriteString(t.Dim.Render(pr.Description) + "\n")
	}
	b.WriteString("\n" + t.Progress(pr.Progress, 30) + "\n")
	meta := []string{}
	if pr.Lead != nil {
		meta = append(meta, "Lead @"+pr.Lead.DisplayName)
	}
	if pr.StartDate != "" {
		meta = append(meta, "Start "+pr.StartDate)
	}
	if pr.TargetDate != "" {
		meta = append(meta, "Target "+pr.TargetDate)
	}
	var teams []string
	for _, tm := range pr.Teams.Nodes {
		teams = append(teams, tm.Key)
	}
	if len(teams) > 0 {
		meta = append(meta, "Teams "+strings.Join(teams, ", "))
	}
	b.WriteString(t.Dim.Render(strings.Join(meta, " · ")) + "\n\n")
	switch {
	case p.err != nil:
		b.WriteString(t.Error.Render(common.ErrText(p.err)))
	case p.detail == nil:
		b.WriteString(t.Dim.Render("Loading…"))
	default:
		if ms := p.detail.ProjectMilestones.Nodes; len(ms) > 0 {
			b.WriteString(t.Header.Render("Milestones") + "\n")
			for _, m := range ms {
				target := ""
				if m.TargetDate != "" {
					target = t.Dim.Render(" → " + m.TargetDate)
				}
				b.WriteString(fmt.Sprintf("  %s %s%s\n", t.Progress(m.Progress, 12), m.Name, target))
			}
			b.WriteString("\n")
		}
		if strings.TrimSpace(p.detail.Content) != "" {
			b.WriteString(p.md.render(t, p.detail.Content, w))
		}
	}
	p.vp.SetContent(b.String())
}

func (p *ProjectDetail) View() string {
	t := p.ctx.Theme
	tab := func(name string, active bool) string {
		if active {
			return t.Badge.Render(name)
		}
		return t.Dim.Render(" " + name + " ")
	}
	header := tab("Issues", !p.overview) + " " + tab("Overview", p.overview) + "  " + t.Progress(p.project.Progress, 20) + "\n"
	if p.overview {
		return header + "\n" + p.vp.View()
	}
	return header + "\n" + p.issues.View()
}
