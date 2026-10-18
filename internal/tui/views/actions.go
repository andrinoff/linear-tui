// Package views holds the screens of the TUI.
package views

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/components"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

func bg() context.Context { return context.Background() }

func modal(m common.View) tea.Cmd { return common.Cmd(common.ModalMsg{Modal: m}) }

// IssueAction handles an issue-scoped key for one or more issues (bulk
// selection). It returns false when the key is not an issue action.
func IssueAction(ctx *common.Ctx, msg tea.KeyPressMsg, issues []linear.Issue) (tea.Cmd, bool) {
	if len(issues) == 0 {
		return nil, false
	}
	k := ctx.Keys
	iss := issues[0]
	switch {
	case key.Matches(msg, k.State):
		return statePicker(ctx, issues), true
	case key.Matches(msg, k.Assignee):
		return assigneePicker(ctx, issues), true
	case key.Matches(msg, k.AssignMe):
		me := ctx.Me()
		if me == nil {
			return nil, true
		}
		return Mutate(ctx, issues, linear.Input{"assigneeId": me.Id}, func(i *linear.Issue) { u := *me; i.Assignee = &u }), true
	case key.Matches(msg, k.Priority):
		return priorityPicker(ctx, issues), true
	case key.Matches(msg, k.Labels):
		return labelPicker(ctx, issues), true
	case key.Matches(msg, k.Cycle):
		return cyclePicker(ctx, issues), true
	case key.Matches(msg, k.Project):
		return projectPicker(ctx, issues), true
	case key.Matches(msg, k.Edit):
		return common.Cmd(common.EditIssueMsg{Issue: iss}), true
	case key.Matches(msg, k.Comment):
		return CommentComposer(ctx, iss.Id, ""), true
	case key.Matches(msg, k.CopyID):
		return common.Copy(iss.Identifier, iss.Identifier), true
	case key.Matches(msg, k.CopyURL):
		return common.Copy(iss.Url, "URL"), true
	case key.Matches(msg, k.CopyBranch):
		return common.Copy(iss.BranchName, "branch "+iss.BranchName), true
	case key.Matches(msg, k.Open):
		return common.OpenURL(iss.Url), true
	case key.Matches(msg, k.Checkout):
		return checkout(ctx, iss), true
	case key.Matches(msg, k.Archive):
		return confirmRemove(ctx, issues, "Archive", ctx.Svc.ArchiveIssue), true
	case key.Matches(msg, k.Delete):
		return confirmRemove(ctx, issues, "Delete", ctx.Svc.DeleteIssue), true
	case key.Matches(msg, k.Relation):
		return relationPicker(ctx, iss), true
	case key.Matches(msg, k.Link):
		return linkPrompt(ctx, iss), true
	}
	return nil, false
}

// Mutate applies an optimistic update locally and sends it to Linear. The
// app reverts the change if the request fails.
func Mutate(ctx *common.Ctx, issues []linear.Issue, in linear.Input, apply func(*linear.Issue)) tea.Cmd {
	var cmds []tea.Cmd
	for _, orig := range issues {
		mod := orig
		mod.Labels.Nodes = slices.Clone(orig.Labels.Nodes)
		apply(&mod)
		seq := ctx.NextSeq(orig.Id)
		cmds = append(cmds, common.Cmd(common.IssueUpdatedMsg{Issue: mod}), func() tea.Msg {
			res, err := ctx.Svc.UpdateIssue(bg(), orig.Id, in)
			return common.MutationMsg{Seq: seq, Orig: orig, Result: res, Err: err}
		})
	}
	return tea.Batch(cmds...)
}

func sameTeam(issues []linear.Issue) (string, error) {
	team := issues[0].Team.Id
	for _, i := range issues[1:] {
		if i.Team.Id != team {
			return "", errors.New("bulk change needs issues from a single team")
		}
	}
	return team, nil
}

func statePicker(ctx *common.Ctx, issues []linear.Issue) tea.Cmd {
	teamID, err := sameTeam(issues)
	if err != nil {
		return common.Err(err)
	}
	p := components.NewPicker(ctx, "Change status", nil, func(sel []components.Item) tea.Cmd {
		st := sel[0].Value.(linear.State)
		return Mutate(ctx, issues, linear.Input{"stateId": st.Id}, func(i *linear.Issue) { i.State = st })
	}).Loading(func() ([]components.Item, error) {
		team, err := ctx.Svc.Team(bg(), teamID)
		if err != nil {
			return nil, err
		}
		return StateItems(ctx.Theme, team.States.Nodes, issues[0].State.Id), nil
	})
	return modal(p)
}

func StateItems(t *theme.Theme, states []linear.State, current string) []components.Item {
	items := make([]components.Item, 0, len(states))
	for _, s := range states {
		it := components.Item{ID: s.Id, Prefix: t.StateIcon(s.Type, s.Color), Label: s.Name, Value: s}
		if s.Id == current {
			it.Detail = "current"
		}
		items = append(items, it)
	}
	return items
}

func memberItems(ctx *common.Ctx, teamID string, withNone bool) ([]components.Item, error) {
	team, err := ctx.Svc.Team(bg(), teamID)
	if err != nil {
		return nil, err
	}
	var items []components.Item
	if withNone {
		items = append(items, components.Item{Prefix: ctx.Theme.Dim.Render("∅"), Label: "Unassigned"})
	}
	for _, u := range team.Members.Nodes {
		if !u.Active {
			continue
		}
		it := components.Item{ID: u.Id, Prefix: ctx.Theme.Dim.Render("@"), Label: u.DisplayName, Detail: u.Name, Value: u}
		if u.IsMe {
			it.Detail = "me"
			items = slices.Insert(items, min(len(items), 1), it)
			continue
		}
		items = append(items, it)
	}
	return items, nil
}

func assigneePicker(ctx *common.Ctx, issues []linear.Issue) tea.Cmd {
	teamID, err := sameTeam(issues)
	if err != nil {
		return common.Err(err)
	}
	return modal(components.NewPicker(ctx, "Assign to", nil, func(sel []components.Item) tea.Cmd {
		if sel[0].ID == "" {
			return Mutate(ctx, issues, linear.Input{"assigneeId": nil}, func(i *linear.Issue) { i.Assignee = nil })
		}
		u := sel[0].Value.(linear.User)
		return Mutate(ctx, issues, linear.Input{"assigneeId": u.Id}, func(i *linear.Issue) { i.Assignee = &u })
	}).Loading(func() ([]components.Item, error) { return memberItems(ctx, teamID, true) }))
}

func PriorityItems(t *theme.Theme) []components.Item {
	var items []components.Item
	for _, p := range []int{0, 1, 2, 3, 4} {
		items = append(items, components.Item{ID: fmt.Sprint(p), Prefix: t.Priority(float64(p)), Label: theme.PriorityName(float64(p)), Value: p})
	}
	return items
}

func priorityPicker(ctx *common.Ctx, issues []linear.Issue) tea.Cmd {
	return modal(components.NewPicker(ctx, "Set priority", PriorityItems(ctx.Theme), func(sel []components.Item) tea.Cmd {
		p := sel[0].Value.(int)
		return Mutate(ctx, issues, linear.Input{"priority": p}, func(i *linear.Issue) {
			i.Priority = float64(p)
			i.PriorityLabel = theme.PriorityName(float64(p))
		})
	}))
}

func labelItems(ctx *common.Ctx, teamID string, selected []linear.Label) ([]components.Item, error) {
	labels, err := ctx.Svc.TeamLabels(bg(), teamID)
	if err != nil {
		return nil, err
	}
	items := make([]components.Item, 0, len(labels))
	for _, l := range labels {
		items = append(items, components.Item{
			ID: l.Id, Prefix: ctx.Theme.Fgc(ctx.Theme.Hex(l.Color), "●"), Label: l.Name, Value: l,
			Selected: slices.ContainsFunc(selected, func(s linear.Label) bool { return s.Id == l.Id }),
		})
	}
	return items, nil
}

func labelPicker(ctx *common.Ctx, issues []linear.Issue) tea.Cmd {
	teamID, err := sameTeam(issues)
	if err != nil {
		return common.Err(err)
	}
	single := len(issues) == 1
	title := "Labels (tab toggle, enter apply)"
	var current []linear.Label
	if single {
		current = issues[0].Labels.Nodes
	} else {
		title = "Add labels to selection"
	}
	return modal(components.NewPicker(ctx, title, nil, func(sel []components.Item) tea.Cmd {
		var ids []string
		var labels []linear.Label
		for _, it := range sel {
			ids = append(ids, it.ID)
			labels = append(labels, it.Value.(linear.Label))
		}
		if single {
			return Mutate(ctx, issues, linear.Input{"labelIds": nonNil(ids)}, func(i *linear.Issue) { i.Labels.Nodes = labels })
		}
		return Mutate(ctx, issues, linear.Input{"addedLabelIds": nonNil(ids)}, func(i *linear.Issue) {
			for _, l := range labels {
				if !slices.ContainsFunc(i.Labels.Nodes, func(x linear.Label) bool { return x.Id == l.Id }) {
					i.Labels.Nodes = append(i.Labels.Nodes, l)
				}
			}
		})
	}).Multi().Loading(func() ([]components.Item, error) { return labelItems(ctx, teamID, current) }))
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func cycleItems(ctx *common.Ctx, teamID string, withNone bool) ([]components.Item, error) {
	cycles, err := ctx.Svc.Cycles(bg(), teamID)
	if err != nil {
		return nil, err
	}
	var items []components.Item
	if withNone {
		items = append(items, components.Item{Prefix: ctx.Theme.Dim.Render("∅"), Label: "No cycle"})
	}
	for _, c := range cycles {
		if c.IsPast {
			continue
		}
		detail := c.StartsAt.Format("Jan 2") + " – " + c.EndsAt.Format("Jan 2")
		if c.IsActive {
			detail += " · current"
		}
		items = append(items, components.Item{ID: c.Id, Prefix: "↻", Label: CycleName(c.Number, c.Name), Detail: detail,
			Value: linear.CycleRef{Id: c.Id, Number: c.Number, Name: c.Name, StartsAt: c.StartsAt, EndsAt: c.EndsAt}})
	}
	return items, nil
}

func CycleName(number float64, name string) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("Cycle %d", int(number))
}

func cyclePicker(ctx *common.Ctx, issues []linear.Issue) tea.Cmd {
	teamID, err := sameTeam(issues)
	if err != nil {
		return common.Err(err)
	}
	return modal(components.NewPicker(ctx, "Move to cycle", nil, func(sel []components.Item) tea.Cmd {
		if sel[0].ID == "" {
			return Mutate(ctx, issues, linear.Input{"cycleId": nil}, func(i *linear.Issue) { i.Cycle = nil })
		}
		c := sel[0].Value.(linear.CycleRef)
		return Mutate(ctx, issues, linear.Input{"cycleId": c.Id}, func(i *linear.Issue) { i.Cycle = &c })
	}).Loading(func() ([]components.Item, error) { return cycleItems(ctx, teamID, true) }))
}

func projectItems(ctx *common.Ctx, withNone bool) ([]components.Item, error) {
	projects, err := ctx.Svc.Projects(bg())
	if err != nil {
		return nil, err
	}
	var items []components.Item
	if withNone {
		items = append(items, components.Item{Prefix: ctx.Theme.Dim.Render("∅"), Label: "No project"})
	}
	for _, p := range projects {
		items = append(items, components.Item{ID: p.Id, Prefix: ctx.Theme.Fgc(ctx.Theme.Hex(p.Color), "◆"), Label: p.Name, Detail: p.Status.Name,
			Value: linear.ProjectRef{Id: p.Id, Name: p.Name, Color: p.Color}})
	}
	return items, nil
}

func projectPicker(ctx *common.Ctx, issues []linear.Issue) tea.Cmd {
	return modal(components.NewPicker(ctx, "Move to project", nil, func(sel []components.Item) tea.Cmd {
		if sel[0].ID == "" {
			return Mutate(ctx, issues, linear.Input{"projectId": nil}, func(i *linear.Issue) { i.Project = nil; i.ProjectMilestone = nil })
		}
		p := sel[0].Value.(linear.ProjectRef)
		return Mutate(ctx, issues, linear.Input{"projectId": p.Id}, func(i *linear.Issue) { i.Project = &p; i.ProjectMilestone = nil })
	}).Loading(func() ([]components.Item, error) { return projectItems(ctx, true) }))
}

// CommentComposer opens the composer and posts a comment (or a reply when
// parentID is set).
func CommentComposer(ctx *common.Ctx, issueID, parentID string) tea.Cmd {
	title := "New comment"
	if parentID != "" {
		title = "Reply"
	}
	return modal(components.NewComposer(ctx, title, "", func(body string) tea.Cmd {
		return func() tea.Msg {
			c, err := ctx.Svc.CreateComment(bg(), issueID, body, parentID)
			if err != nil {
				return common.ErrMsg{Err: err}
			}
			return common.CommentAddedMsg{IssueID: issueID, Comment: *c}
		}
	}))
}

func checkout(ctx *common.Ctx, iss linear.Issue) tea.Cmd {
	cmd, err := ctx.Repo.CheckoutCmd(iss.BranchName)
	if err != nil {
		return common.Err(err)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return common.ErrMsg{Err: fmt.Errorf("git checkout %s: %w", iss.BranchName, err)}
		}
		return common.ToastMsg{Text: "Switched to " + iss.BranchName}
	})
}

func confirmRemove(ctx *common.Ctx, issues []linear.Issue, verb string, do func(context.Context, string) error) tea.Cmd {
	what := issues[0].Identifier
	if len(issues) > 1 {
		what = fmt.Sprintf("%d issues", len(issues))
	}
	return modal(components.NewConfirm(ctx, verb+" "+what+"?", func() tea.Msg {
		var cmds []tea.Cmd
		for _, i := range issues {
			if err := do(bg(), i.Id); err != nil {
				return common.ErrMsg{Err: err}
			}
			cmds = append(cmds, common.Cmd(common.IssueRemovedMsg{ID: i.Id}))
		}
		cmds = append(cmds, common.Toast(verb+"d "+what))
		return tea.BatchMsg(cmds)
	}))
}

var relationTypes = []components.Item{
	{ID: "blocks", Label: "Blocks…"},
	{ID: "blockedBy", Label: "Blocked by…"},
	{ID: "related", Label: "Related to…"},
	{ID: "duplicate", Label: "Duplicate of…"},
}

func relationPicker(ctx *common.Ctx, iss linear.Issue) tea.Cmd {
	return modal(components.NewPicker(ctx, "Add relation", relationTypes, func(sel []components.Item) tea.Cmd {
		kind := sel[0].ID
		return modal(components.NewPrompt(ctx, iss.Identifier+" "+sel[0].Label, "Issue identifier, e.g. ENG-42", func(ref string) tea.Cmd {
			return func() tea.Msg {
				other, err := ctx.Svc.Issue(bg(), ref)
				if err != nil {
					return common.ErrMsg{Err: err}
				}
				from, to, t := iss.Id, other.Id, kind
				if kind == "blockedBy" {
					from, to, t = other.Id, iss.Id, "blocks"
				}
				if err := ctx.Svc.CreateRelation(bg(), from, to, t); err != nil {
					return common.ErrMsg{Err: err}
				}
				return tea.BatchMsg{common.Toast("Relation added"), common.Cmd(common.ReloadIssueMsg{ID: iss.Id})}
			}
		}))
	}))
}

func linkPrompt(ctx *common.Ctx, iss linear.Issue) tea.Cmd {
	return modal(components.NewPrompt(ctx, "Link URL to "+iss.Identifier, "https://…", func(url string) tea.Cmd {
		return func() tea.Msg {
			if err := ctx.Svc.LinkURL(bg(), iss.Id, url, ""); err != nil {
				return common.ErrMsg{Err: err}
			}
			return tea.BatchMsg{common.Toast("Link added"), common.Cmd(common.ReloadIssueMsg{ID: iss.Id})}
		}
	}))
}

// snoozeOptions are offered for inbox notifications.
func snoozeOptions(now time.Time) []components.Item {
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, now.Location())
	days := (8 - int(now.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	nextWeek := time.Date(now.Year(), now.Month(), now.Day()+days, 9, 0, 0, 0, now.Location())
	return []components.Item{
		{ID: "1h", Label: "1 hour", Detail: now.Add(time.Hour).Format("15:04"), Value: now.Add(time.Hour)},
		{ID: "tomorrow", Label: "Tomorrow", Detail: tomorrow.Format("Mon 15:04"), Value: tomorrow},
		{ID: "week", Label: "Next week", Detail: nextWeek.Format("Mon Jan 2"), Value: nextWeek},
	}
}
