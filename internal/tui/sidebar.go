package tui

import (
	"fmt"
	"strings"

	"github.com/andrinoff/linear-tui/internal/tui/common"
)

const sidebarWidth = 24

type navItem struct {
	target, icon, label, chord string
}

var navItems = []navItem{
	{"inbox", "✉", "Inbox", "g i"},
	{"my", "◉", "My issues", "g m"},
	{"team", "▤", "Team issues", "g t"},
	{"board", "▥", "Board", "g b"},
	{"cycles", "↻", "Cycles", "g c"},
	{"projects", "◆", "Projects", "g p"},
	{"views", "≡", "Views", "g v"},
	{"search", "⌕", "Search", "g s"},
}

type sidebar struct {
	cursor int
	// clickRows maps rendered rows to nav indexes (or -2 for the team row).
	clickRows map[int]int
}

func (s *sidebar) view(ctx *common.Ctx, active string, focused bool, unread, h int) string {
	t := ctx.Theme
	w := sidebarWidth - 1
	s.clickRows = map[int]int{}
	var lines []string
	org := "Linear"
	if ctx.Viewer != nil {
		org = ctx.Viewer.Organization.Name
	}
	lines = append(lines, " "+t.Fgc(t.Accent, "◢◤")+" "+t.Title.Render(common.Fit(org, w-5)), "")
	team := "No team"
	if ctx.Team != nil {
		team = ctx.Team.Name
	}
	s.clickRows[len(lines)] = -2
	lines = append(lines, " "+t.Dim.Render("Team ")+common.Fit(t.Bold.Render(team), w-7), "")
	for i, it := range navItems {
		label := it.icon + " " + it.label
		badge := ""
		if it.target == "inbox" && unread > 0 {
			badge = t.Badge.Render(fmt.Sprint(unread))
		}
		var text string
		switch {
		case focused && i == s.cursor:
			text = t.Selected.Render(common.Fit(t.Cursor.Render("▌")+label+" "+badge, w))
		case it.target == active:
			text = common.Fit(t.Cursor.Render("▌")+t.Bold.Render(label)+" "+badge, w)
		default:
			text = common.Fit(" "+t.Dim.Render(label)+" "+badge, w)
		}
		s.clickRows[len(lines)] = i
		lines = append(lines, text)
	}
	lines = append(lines, "")
	if ctx.Viewer != nil {
		lines = append(lines, " "+t.Dim.Render("@"+ctx.Viewer.Viewer.DisplayName))
	}
	body := common.FitLines(strings.Join(lines, "\n"), h)
	border := t.Border
	if focused {
		border = t.Accent
	}
	var out []string
	for _, l := range strings.Split(body, "\n") {
		out = append(out, common.Fit(l, w)+t.Fgc(border, "│"))
	}
	return strings.Join(out, "\n")
}
