package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
)

const sidebarWidth = 28

// destinations reachable from the palette and g-chords.
var navItems = []struct{ target, label, chord string }{
	{"inbox", "Inbox", "g i"},
	{"my", "My issues", "g m"},
	{"team", "Team issues", "g t"},
	{"board", "Team board", "g b"},
	{"cycles", "Cycles", "g c"},
	{"projects", "Projects", "g p"},
	{"views", "Views", "g v"},
	{"search", "Search", "g s"},
}

type rowKind int

const (
	rowGap rowKind = iota
	rowHeader
	rowItem
	rowTeam
)

// sideRow is one line of Linear's sidebar.
type sideRow struct {
	kind   rowKind
	target string
	team   *linear.Team
	icon   string
	label  string
	indent int
	count  int
}

type sidebar struct {
	cursor int
	rows   []sideRow
	// clickRows maps screen rows to indexes in rows.
	clickRows map[int]int
	// iconsY/iconsX locate the search and compose buttons in the header.
	searchX, composeX int
}

// build lays out the sidebar like Linear: personal items, the workspace
// section, then teams with the focused team expanded.
func (s *sidebar) build(ctx *common.Ctx, unread int) {
	s.rows = s.rows[:0]
	item := func(target, icon, label string, indent int, team *linear.Team) {
		s.rows = append(s.rows, sideRow{kind: rowItem, target: target, icon: icon, label: label, indent: indent, team: team})
	}
	item("inbox", "✉", "Inbox", 0, nil)
	s.rows[len(s.rows)-1].count = unread
	item("my", "◎", "My issues", 0, nil)
	s.rows = append(s.rows, sideRow{kind: rowGap}, sideRow{kind: rowHeader, label: "Workspace"})
	item("projects", "◆", "Projects", 0, nil)
	item("views", "≡", "Views", 0, nil)
	s.rows = append(s.rows, sideRow{kind: rowGap}, sideRow{kind: rowHeader, label: "Your teams"})
	for i := range ctx.Teams {
		team := &ctx.Teams[i]
		s.rows = append(s.rows, sideRow{kind: rowTeam, target: "team", team: team, label: team.Name})
		if ctx.Team == nil || ctx.Team.Id != team.Id {
			continue
		}
		item("team", "⧉", "Issues", 1, team)
		if team.CyclesEnabled {
			item("cycles", "↻", "Cycles", 1, team)
			item("cycle-current", "", "Current", 2, team)
			item("cycle-upcoming", "", "Upcoming", 2, team)
		}
		item("team-projects", "◆", "Projects", 1, team)
		item("team-views", "≡", "Views", 1, team)
	}
}

func (s *sidebar) selectable() []int {
	var out []int
	for i, r := range s.rows {
		if r.kind == rowItem || r.kind == rowTeam {
			out = append(out, i)
		}
	}
	return out
}

// selectRoot moves the cursor to the row for target.
func (s *sidebar) selectRoot(target string) {
	for i, idx := range s.selectable() {
		if s.rows[idx].target == target && s.rows[idx].kind == rowItem {
			s.cursor = i
			return
		}
	}
}

func (s *sidebar) current() *sideRow {
	sel := s.selectable()
	if s.cursor < 0 || s.cursor >= len(sel) {
		return nil
	}
	return &s.rows[sel[s.cursor]]
}

func (s *sidebar) view(ctx *common.Ctx, active string, focused bool, h int) string {
	t := ctx.Theme
	w := sidebarWidth - 2
	s.clickRows = map[int]int{}
	org := "Linear"
	if ctx.Viewer != nil {
		org = ctx.Viewer.Organization.Name
	}
	icons := t.Dim.Render("⌕") + "  " + t.Dim.Render("✎") + " "
	logo := ""
	if ctx.Viewer != nil {
		logo = ctx.Viewer.Organization.LogoUrl
	}
	head := t.Avatar(org, logo) + " " + t.Bold.Render(org) + t.Faint.Render(" ▾")
	s.composeX = 1 + w - 2
	s.searchX = s.composeX - 3
	lines := []string{"", " " + common.Fit(head, w-lipgloss.Width(icons)) + icons, ""}

	sel := s.selectable()
	for i, r := range s.rows {
		var text string
		switch r.kind {
		case rowGap:
			text = ""
		case rowHeader:
			text = "  " + t.Faint.Render(r.label+" ▾")
		case rowTeam:
			arrow := " ▸"
			if ctx.Team != nil && ctx.Team.Id == r.team.Id {
				arrow = " ▾"
			}
			icon := t.Fgc(lipgloss.Color(t.ColorFor(r.team.Key)), "■")
			text = "  " + icon + " " + t.App.Render(r.label) + t.Faint.Render(arrow)
		case rowItem:
			indent := strings.Repeat("  ", r.indent)
			icon := r.icon
			if icon == "" {
				icon = " "
			}
			label := t.App.Render(r.label)
			isActive := r.target == active && (r.team == nil || (ctx.Team != nil && r.team.Id == ctx.Team.Id))
			if isActive {
				label = t.Bold.Render(r.label)
			}
			text = "  " + indent + t.Dim.Render(icon) + " " + label
			if r.count > 0 {
				c := t.Dim.Render(fmt.Sprint(r.count)) + " "
				text = common.Fit(text, w-lipgloss.Width(c)) + c
			}
			if isActive {
				text = t.Select(text, w)
			}
		}
		if focused && s.cursor < len(sel) && sel[s.cursor] == i {
			text = t.Select(t.Cursor.Render("▎")+strings.TrimPrefix(text, " "), w)
		}
		if r.kind == rowItem || r.kind == rowTeam {
			s.clickRows[len(lines)] = i
		}
		lines = append(lines, " "+common.Fit(text, w)+" ")
	}
	body := common.FitLines(strings.Join(lines, "\n"), h-1)
	return body + "\n " + t.Faint.Render("?")
}
