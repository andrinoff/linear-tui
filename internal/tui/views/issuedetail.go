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
	"github.com/andrinoff/linear-tui/internal/github"
	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/components"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

// IssueDetail shows one issue: metadata, description, sub-issues,
// relations, attachments, and comments.
type IssueDetail struct {
	ctx *common.Ctx
	// Embedded is set when the detail is a preview pane inside another
	// view, so removing the issue doesn't pop the screen.
	Embedded bool
	id       string
	preview  *linear.Issue
	detail   *linear.IssueDetail
	err      error
	loading  bool
	reqID    int

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

// propsW is the width of Linear's properties pane, shown on wide screens.
const propsW = 34

func (d *IssueDetail) paneW() int {
	if d.w >= 80 {
		return propsW
	}
	return 0
}

func (d *IssueDetail) SetSize(w, h int) {
	d.w, d.h = w, h
	d.vp.SetWidth(w - d.paneW())
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
		if msg.ID == d.id && !d.Embedded {
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
				if _, ok := github.ParseURL(l.url); ok {
					return common.Cmd(common.PushMsg{View: NewPullRequest(d.ctx, l.url, "", "")})
				}
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
	w := min(d.w-d.paneW()-4, 100)
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
			text = t.Select(" "+text, w)
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

	write("")
	write(t.Title.Width(w).Render(iss.Title))
	write("")
	if d.paneW() == 0 {
		write(d.metadata(iss, w))
		write("")
	}

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

	write(t.Fgc(t.Border, strings.Repeat("─", w)))
	count := fmt.Sprintf("  %d comments", len(det.Comments.Nodes))
	if len(det.Comments.Nodes) == 1 {
		count = "  1 comment"
	}
	write(t.Bold.Render("Activity") + t.Dim.Render(count))
	write("")
	if len(det.Comments.Nodes) == 0 {
		write(t.Faint.Render("No comments yet. Press m to leave a comment."))
	}
	for i := range det.Comments.Nodes {
		c := &det.Comments.Nodes[i]
		author, avatarURL := "Someone", ""
		if c.User != nil {
			author, avatarURL = c.User.DisplayName, c.User.AvatarUrl
		}
		indent := ""
		if c.Parent != nil {
			indent = "  ↳ "
		}
		meta := indent + t.Avatar(author, avatarURL) + " " + t.Bold.Render(author) + " " + t.Dim.Render(common.Ago(c.CreatedAt)+" ago")
		if !c.EditedAt.IsZero() {
			meta += t.Faint.Render(" (edited)")
		}
		addLink(link{kind: linkComment, comment: c}, meta)
		md := d.cmd[c.Id]
		if md == nil {
			md = &markdown{}
			d.cmd[c.Id] = md
		}
		cw := w - len(indent) - 2
		body := theme.Surface(md.render(t, c.Body, cw-4), cw, t.CardBg)
		edge := func(g string) string {
			return strings.Repeat(" ", len(indent)+1) + t.Fgc(t.CardBg, strings.Repeat(g, cw))
		}
		write(edge("▄"))
		write(lipgloss.NewStyle().PaddingLeft(len(indent) + 1).Render(body))
		write(edge("▀"))
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
	if d.paneW() == 0 {
		return d.vp.View()
	}
	t := d.ctx.Theme
	sep := strings.TrimSuffix(strings.Repeat(t.Fgc(t.Border, "│")+"\n", d.h), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, common.FitLines(d.vp.View(), d.h), sep, d.properties())
}

// properties renders Linear's issue sidebar: one labeled row per property.
func (d *IssueDetail) properties() string {
	t := d.ctx.Theme
	iss := d.issue()
	w := propsW - 3
	if iss == nil {
		return ""
	}
	row := func(label, value string) string {
		return "  " + common.Fit(t.Dim.Render(label), 11) + common.Fit(value, w-11)
	}
	assignee := t.Faint.Render("◌ ") + t.Dim.Render("Assign")
	if iss.Assignee != nil {
		assignee = t.Avatar(iss.Assignee.DisplayName, iss.Assignee.AvatarUrl) + " " + t.App.Render(iss.Assignee.DisplayName)
	}
	lines := []string{
		"",
		"  " + t.Dim.Render(iss.Identifier) + "  " + t.Faint.Render("y ⎘  b ⑂  o ↗"),
		"",
		t.Faint.Render("  Properties"),
		"",
		row("Status", t.StateIcon(iss.State.Type, iss.State.Color)+" "+t.App.Render(iss.State.Name)),
		row("Priority", t.Priority(iss.Priority)+" "+t.App.Render(theme.PriorityName(iss.Priority))),
		row("Assignee", assignee),
	}
	if iss.Estimate != 0 {
		lines = append(lines, row("Estimate", t.App.Render(fmt.Sprintf("%g points", iss.Estimate))))
	}
	if iss.DueDate != "" {
		lines = append(lines, row("Due date", dueShort(t, iss.DueDate)))
	}
	lines = append(lines, "", t.Faint.Render("  Labels"), "")
	if len(iss.Labels.Nodes) == 0 {
		lines = append(lines, "  "+t.Dim.Render("+ Add label"))
	}
	var chips []string
	for _, l := range iss.Labels.Nodes {
		chips = append(chips, theme.Pill(t.Fgc(t.Hex(l.Color), "●")+" "+t.App.Render(l.Name), t.ChipBg))
	}
	for _, r := range layoutChips(chips, w-2, 4) {
		lines = append(lines, "  "+r)
	}
	lines = append(lines, "", t.Faint.Render("  Cycle"), "")
	if iss.Cycle != nil {
		lines = append(lines, "  "+t.Fgc(t.Accent, "↻")+" "+t.App.Render(CycleName(iss.Cycle.Number, iss.Cycle.Name)))
	} else {
		lines = append(lines, "  "+t.Dim.Render("+ Add to cycle"))
	}
	lines = append(lines, "", t.Faint.Render("  Project"), "")
	if iss.Project != nil {
		lines = append(lines, "  "+t.Fgc(t.Hex(iss.Project.Color), "◆")+" "+t.App.Render(iss.Project.Name))
		if iss.ProjectMilestone != nil {
			lines = append(lines, "    "+t.Dim.Render("◇ "+iss.ProjectMilestone.Name))
		}
	} else {
		lines = append(lines, "  "+t.Dim.Render("+ Add to project"))
	}
	lines = append(lines, "")
	if d.detail != nil && d.detail.Creator != nil {
		lines = append(lines, "  "+t.Faint.Render("Created by "+d.detail.Creator.DisplayName))
	}
	lines = append(lines, "  "+t.Faint.Render("Created "+shortDate(iss.CreatedAt)+" · updated "+common.Ago(iss.UpdatedAt)+" ago"))
	for i, l := range lines {
		lines[i] = common.Fit(l, propsW-1)
	}
	return common.FitLines(strings.Join(lines, "\n"), d.h)
}
