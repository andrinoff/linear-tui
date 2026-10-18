package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/components"
	"github.com/andrinoff/linear-tui/internal/tui/views"
)

type selectable interface{ Selection() []linear.Issue }

// keyPress synthesizes the key press for a binding's first key so palette
// commands reuse the exact same code path as shortcuts.
func keyPress(b key.Binding) tea.KeyPressMsg {
	k := b.Keys()[0]
	r := []rune(k)
	return tea.KeyPressMsg{Text: k, Code: r[0]}
}

func (a *App) palette() *components.Picker {
	ctx := a.ctx
	k := ctx.Keys
	cmd := func(c tea.Cmd) any { return c }
	var items []components.Item
	add := func(label, detail string, c tea.Cmd) {
		items = append(items, components.Item{Label: label, Detail: detail, Value: cmd(c)})
	}

	for _, it := range navItems {
		add("Go to "+it.label, it.chord, common.Cmd(common.NavigateMsg{Target: it.target}))
	}
	add("Create issue", k.Create.Help().Key, common.Cmd(a.createMsg()))
	add("Switch team", "", func() tea.Msg { return common.ModalMsg{Modal: a.teamPicker()} })
	add("Refresh", k.Refresh.Help().Key, common.Cmd(common.RefreshMsg{}))
	add("Toggle theme", "light/dark", common.Cmd(common.ToggleThemeMsg{}))
	add("Toggle sidebar", k.ToggleSidebar.Help().Key, common.Cmd(toggleSidebarMsg{}))
	add("Keyboard shortcuts", k.Help.Help().Key, common.Cmd(common.ShowHelpMsg{}))
	if s, ok := a.top().(selectable); ok {
		if sel := s.Selection(); len(sel) > 0 {
			id := sel[0].Identifier
			if len(sel) > 1 {
				id = "selection"
			}
			for _, b := range ctx.Keys.IssueHelp() {
				issues, binding := sel, b
				add(capitalize(b.Help().Desc)+" ("+id+")", b.Help().Key, func() tea.Msg {
					c, _ := views.IssueAction(ctx, keyPress(binding), issues)
					if c == nil {
						return nil
					}
					return c()
				})
			}
		}
	}
	for _, r := range ctx.State.Recent {
		add("Open "+r.Identifier+" "+r.Title, "recent", common.Cmd(common.OpenIssueMsg{ID: r.ID}))
	}
	add("Log out", "", common.Cmd(common.LogoutMsg{}))
	add("Quit", "ctrl+c", tea.Quit)

	return components.NewPicker(ctx, "Command palette", items, func(sel []components.Item) tea.Cmd {
		if c, ok := sel[0].Value.(tea.Cmd); ok {
			return c
		}
		return nil
	})
}

func (a *App) teamPicker() *components.Picker {
	ctx := a.ctx
	return components.NewPicker(ctx, "Switch team", nil, func(sel []components.Item) tea.Cmd {
		t := sel[0].Value.(*linear.Team)
		return common.Cmd(common.TeamChangedMsg{Team: t})
	}).Loading(func() ([]components.Item, error) {
		teams, err := ctx.Svc.Teams(bg())
		if err != nil {
			return nil, err
		}
		var items []components.Item
		for i := range teams {
			t := &teams[i]
			detail := t.Key
			if ctx.Team != nil && ctx.Team.Id == t.Id {
				detail += " · current"
			}
			items = append(items, components.Item{ID: t.Id, Label: t.Name, Detail: detail, Value: t})
		}
		return items, nil
	})
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}
