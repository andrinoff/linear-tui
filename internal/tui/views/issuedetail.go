package views

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andrinoff/linear-tui/internal/config"
	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/components"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

// IssueDetail shows one issue: metadata, description, sub-issues,
// relations, attachments, and comments.
type IssueDetail struct {
	ctx     *common.Ctx
	id      string
	preview *linear.Issue
	detail  *linear.IssueDetail
	err     error
	loading bool
	reqID   int

	vp    viewport.Model
	spin  spinner.Model
	md    markdown
	cmd   map[string]*markdown
	links []link
	focus int
	w, h  int
}

type linkKind int

const (
	linkIssue linkKind = iota
	linkURL
	linkComment
)

type link struct {
	kind    linkKind
	id      string
	url     string
	comment *linear.Comment
	line    int
}

type issueLoadedMsg struct {
	owner  *IssueDetail
	reqID  int
	detail *linear.IssueDetail
	err    error
}

// NewIssueDetail opens id (an id or identifier). preview, if set, is shown
// while the full issue loads.
func NewIssueDetail(ctx *common.Ctx, id string, preview *linear.Issue) *IssueDetail {
	return &IssueDetail{
		ctx: ctx, id: id, preview: preview, loading: true, focus: -1, cmd: map[string]*markdown{},
		vp:   viewport.New(),
		spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(ctx.Theme.Cursor)),
	}
}

func (d *IssueDetail) Init() tea.Cmd { return tea.Batch(d.load(), d.spin.Tick) }

func (d *IssueDetail) load() tea.Cmd {
	d.reqID++
	id, req := d.id, d.reqID
	return func() tea.Msg {
		det, err := d.ctx.Svc.Issue(bg(), id)
		return issueLoadedMsg{owner: d, reqID: req, detail: det, err: err}
	}
}

func (d *IssueDetail) Title() string {
	if i := d.issue(); i != nil {
		return i.Identifier
	}
	return d.id
}

func (d *IssueDetail) Capturing() bool { return false }

func (d *IssueDetail) SetSize(w, h int) {
	d.w, d.h = w, h
	d.vp.SetWidth(w)
	d.vp.SetHeight(h)
	d.renderContent()
}

func (d *IssueDetail) Help() []key.Binding {
	k := d.ctx.Keys
	return []key.Binding{
		key.NewBinding(key.WithKeys("n", "N"), key.WithHelp("n/N", "next/prev link")),
		k.Enter, k.Edit, k.State, k.Assignee, k.Priority, k.Comment, k.Open, k.CopyBranch, k.Checkout,
	}
}

func (d *IssueDetail) issue() *linear.Issue {
	if d.detail != nil {
		return &d.detail.IssueFields
	}
	return d.preview
}

func (d *IssueDetail) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !d.loading {
			return nil
		}
		var cmd tea.Cmd
		d.spin, cmd = d.spin.Update(msg)
		d.renderContent()
		return cmd
	case issueLoadedMsg:
		if msg.owner != d || msg.reqID != d.reqID {
			return nil
		}
		d.loading = false
		if msg.err != nil {
			d.err = msg.err
			d.renderContent()
			return common.Err(msg.err)
		}
		d.detail, d.err = msg.detail, nil
		d.id = d.detail.Id
		d.ctx.State.AddRecent(config.RecentIssue{ID: d.detail.Id, Identifier: d.detail.Identifier, Title: d.detail.Title})
		d.renderContent()
		return nil
	case common.IssueUpdatedMsg:
		if d.detail != nil && msg.Issue.Id == d.detail.Id {
			d.detail.IssueFields = msg.Issue
			d.renderContent()
		} else if d.detail != nil {
			for i := range d.detail.Children.Nodes {
				if d.detail.Children.Nodes[i].Id == msg.Issue.Id {
					d.detail.Children.Nodes[i] = msg.Issue
					d.renderContent()
				}
			}
		}
		return nil
	case common.IssueRemovedMsg:
		if msg.ID == d.id {
			return common.Cmd(common.PopMsg{})
		}
		return nil
	case common.CommentAddedMsg:
		if msg.IssueID == d.id {
			return d.load()
		}
		return nil
	case common.ReloadIssueMsg:
		if msg.ID == d.id {
			return d.load()
		}
		return nil
	case common.RefreshMsg:
		return d.load()
	case tea.KeyPressMsg:
		return d.handleKey(msg)
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		d.vp, cmd = d.vp.Update(msg)
		return cmd
	}
	return nil
}

func (d *IssueDetail) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := d.ctx.Keys
	iss := d.issue()
	switch {
	case msg.String() == "n" || msg.String() == "N":
		if len(d.links) == 0 {
			return nil
		}
		if msg.String() == "n" {
			d.focus = (d.focus + 1) % len(d.links)
		} else {
			d.focus = (d.focus - 1 + len(d.links)) % len(d.links)
		}
		d.renderContent()
		line := d.links[d.focus].line
		if line < d.vp.YOffset() || line >= d.vp.YOffset()+d.vp.Height() {
			d.vp.SetYOffset(max(0, line-2))
		}
		return nil
	case key.Matches(msg, k.Enter):
		if d.focus >= 0 && d.focus < len(d.links) {
			l := d.links[d.focus]
			switch l.kind {
			case linkIssue:
				return common.Cmd(common.PushMsg{View: NewIssueDetail(d.ctx, l.id, nil)})
			case linkURL:
				return common.OpenURL(l.url)
			case linkComment:
				return CommentComposer(d.ctx, d.id, l.comment.Id)
			}
		}
		return nil
	}
	if c := d.focusedComment(); c != nil && c.User != nil && c.User.IsMe {
		switch {
		case key.Matches(msg, k.Edit):
			return modal(components.NewComposer(d.ctx, "Edit comment", c.Body, func(body string) tea.Cmd {
				return func() tea.Msg {
					nc, err := d.ctx.Svc.UpdateComment(bg(), c.Id, body)
					if err != nil {
						return common.ErrMsg{Err: err}
					}
					return common.CommentAddedMsg{IssueID: d.id, Comment: *nc}
				}
			}))
		case key.Matches(msg, k.Delete):
			return modal(components.NewConfirm(d.ctx, "Delete this comment?", func() tea.Msg {
				if err := d.ctx.Svc.DeleteComment(bg(), c.Id); err != nil {
					return common.ErrMsg{Err: err}
				}
				return common.ReloadIssueMsg{ID: d.id}
			}))
		}
	}
	if iss != nil {
		if key.Matches(msg, k.Edit) && d.detail != nil {
			return common.Cmd(common.EditIssueMsg{Issue: *iss, Description: d.detail.Description})
		}
		if cmd, ok := IssueAction(d.ctx, msg, []linear.Issue{*iss}); ok {
			return cmd
		}
	}
	var cmd tea.Cmd
	d.vp, cmd = d.vp.Update(msg)
	return cmd
}

// Defaults makes new issues created from the detail view sub-issues.
func (d *IssueDetail) Defaults() common.IssueDefaults {
	if iss := d.issue(); iss != nil {
		return common.IssueDefaults{TeamID: iss.Team.Id, ParentID: iss.Id}
	}
	return common.IssueDefaults{}
}

func (d *IssueDetail) Selection() []linear.Issue {
	if iss := d.issue(); iss != nil {
		return []linear.Issue{*iss}
	}
	return nil
}

func (d *IssueDetail) focusedComment() *linear.Comment {
	if d.focus >= 0 && d.focus < len(d.links) && d.links[d.focus].kind == linkComment {
		return d.links[d.focus].comment
	}
	return nil
}

func (d *IssueDetail) renderContent() {
	if d.w == 0 {
		return
	}
	t := d.ctx.Theme
	w := min(d.w-2, 110)
	iss := d.issue()
	var b strings.Builder
	line := 0
	write := func(s string) {
		b.WriteString(s + "\n")
		line += strings.Count(s, "\n") + 1
	}
	d.links = d.links[:0]
	addLink := func(l link, text string) {
		l.line = line
		if len(d.links) == d.focus {
			text = t.Selected.Render(t.Cursor.Render("▌") + text)
		} else {
			text = " " + text
		}
		d.links = append(d.links, l)
		write(text)
	}

	if iss == nil {
		if d.err != nil {
			write(t.Error.Render(common.ErrText(d.err)))
			write(t.Dim.Render("Press r to retry."))
		} else {
			write(d.spin.View() + " Loading " + d.id + "…")
		}
		d.vp.SetContent(b.String())
		return
	}

	write(t.Dim.Render(iss.Team.Name+" › "+iss.Identifier) + "  " + t.StateIcon(iss.State.Type, iss.State.Color) + " " + iss.State.Name)
	write(t.Title.Width(w).Render(iss.Title))
	write("")
	write(d.metadata(iss, w))
	write("")

	if d.detail == nil {
		if d.err != nil {
			write(t.Error.Render(common.ErrText(d.err)))
		} else {
			write(d.spin.View() + t.Dim.Render(" Loading details…"))
		}
		d.vp.SetContent(b.String())
		return
	}
	det := d.detail
	if det.Parent != nil {
		addLink(link{kind: linkIssue, id: det.Parent.Id}, t.Dim.Render("Parent: ")+det.Parent.Identifier+" "+det.Parent.Title)
		write("")
	}
	if strings.TrimSpace(det.Description) != "" {
		write(d.md.render(t, det.Description, w))
	} else {
		write(t.Faint.Render("No description. Press e to edit."))
	}
	write("")

	if n := len(det.Children.Nodes); n > 0 {
		done := 0
		for _, c := range det.Children.Nodes {
			if c.State.Type == "completed" || c.State.Type == "canceled" {
				done++
			}
		}
		write(t.Header.Render(fmt.Sprintf("Sub-issues %d/%d", done, n)))
		for _, c := range det.Children.Nodes {
			addLink(link{kind: linkIssue, id: c.Id}, t.StateIcon(c.State.Type, c.State.Color)+" "+t.Dim.Render(c.Identifier)+" "+c.Title+assigneeSuffix(t, c.Assignee))
		}
		write("")
	}

	if len(det.Relations.Nodes)+len(det.InverseRelations.Nodes) > 0 {
		write(t.Header.Render("Relations"))
		for _, r := range det.Relations.Nodes {
			ri := r.RelatedIssue
			addLink(link{kind: linkIssue, id: ri.Id}, t.Dim.Render(relationLabel(r.Type, false))+" "+t.StateIcon(ri.State.Type, ri.State.Color)+" "+ri.Identifier+" "+ri.Title)
		}
		for _, r := range det.InverseRelations.Nodes {
			ri := r.Issue
			addLink(link{kind: linkIssue, id: ri.Id}, t.Dim.Render(relationLabel(r.Type, true))+" "+t.StateIcon(ri.State.Type, ri.State.Color)+" "+ri.Identifier+" "+ri.Title)
		}
		write("")
	}

	if len(det.Attachments.Nodes) > 0 {
		write(t.Header.Render("Links"))
		for _, a := range det.Attachments.Nodes {
			text := t.Link.Render(a.Title)
			if a.Subtitle != "" {
				text += " " + t.Dim.Render(a.Subtitle)
			}
			addLink(link{kind: linkURL, url: a.Url}, "↗ "+text)
		}
		write("")
	}

	write(t.Header.Render(fmt.Sprintf("Comments (%d)", len(det.Comments.Nodes))))
	if len(det.Comments.Nodes) == 0 {
		write(t.Faint.Render("No comments yet. Press m to add one."))
	}
	for i := range det.Comments.Nodes {
		c := &det.Comments.Nodes[i]
		author := "Someone"
		if c.User != nil {
			author = c.User.DisplayName
		}
		indent := ""
		if c.Parent != nil {
			indent = "  ↳ "
		}
		meta := indent + t.Bold.Render(author) + " " + t.Dim.Render(common.Ago(c.CreatedAt)+" ago")
		if !c.EditedAt.IsZero() {
			meta += t.Faint.Render(" (edited)")
		}
		addLink(link{kind: linkComment, comment: c}, meta)
		md := d.cmd[c.Id]
		if md == nil {
			md = &markdown{}
			d.cmd[c.Id] = md
		}
		body := md.render(t, c.Body, w-4)
		write(lipgloss.NewStyle().PaddingLeft(len(indent) + 2).Render(body))
		write("")
	}
	if d.loading {
		write(d.spin.View() + t.Dim.Render(" refreshing…"))
	}
	d.vp.SetContent(b.String())
}

func assigneeSuffix(t *theme.Theme, u *linear.User) string {
	if u == nil {
		return ""
	}
	return t.Dim.Render("  @" + u.DisplayName)
}

func relationLabel(kind string, inverse bool) string {
	switch kind {
	case "blocks":
		if inverse {
			return "blocked by"
		}
		return "blocks    "
	case "duplicate":
		if inverse {
			return "duplicated by"
		}
		return "duplicate of"
	}
	return "related   "
}

func (d *IssueDetail) metadata(iss *linear.Issue, w int) string {
	t := d.ctx.Theme
	field := func(label, value string) string {
		return t.Dim.Render(common.Fit(label, 10)) + value
	}
	assignee := t.Faint.Render("Unassigned")
	if iss.Assignee != nil {
		assignee = iss.Assignee.DisplayName
	}
	labels := t.Faint.Render("None")
	if len(iss.Labels.Nodes) > 0 {
		labels = labelsInline(t, iss.Labels.Nodes)
	}
	left := []string{
		field("Status", t.StateIcon(iss.State.Type, iss.State.Color)+" "+iss.State.Name),
		field("Priority", t.Priority(iss.Priority)+" "+theme.PriorityName(iss.Priority)),
		field("Assignee", assignee),
		field("Labels", labels),
	}
	right := []string{}
	if iss.Cycle != nil {
		right = append(right, field("Cycle", CycleName(iss.Cycle.Number, iss.Cycle.Name)))
	}
	if iss.Project != nil {
		p := t.Fgc(t.Hex(iss.Project.Color), "◆") + " " + iss.Project.Name
		if iss.ProjectMilestone != nil {
			p += t.Dim.Render(" › " + iss.ProjectMilestone.Name)
		}
		right = append(right, field("Project", p))
	}
	if iss.Estimate != 0 {
		right = append(right, field("Estimate", fmt.Sprintf("%g", iss.Estimate)))
	}
	if iss.DueDate != "" {
		right = append(right, field("Due", dueShort(t, iss.DueDate)))
	}
	if d.detail != nil && d.detail.Creator != nil {
		right = append(right, field("Creator", d.detail.Creator.DisplayName))
	}
	right = append(right, field("Updated", common.Ago(iss.UpdatedAt)+" ago"))
	if w < 80 {
		return strings.Join(append(left, right...), "\n")
	}
	col := lipgloss.NewStyle().Width(w / 2)
	return lipgloss.JoinHorizontal(lipgloss.Top, col.Render(strings.Join(left, "\n")), col.Render(strings.Join(right, "\n")))
}

func (d *IssueDetail) View() string {
	return d.vp.View()
}
