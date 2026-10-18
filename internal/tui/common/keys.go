package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
)

// Keys are the user-configurable bindings. Config overrides use the action
// name, for example `keys.create = ["n"]`.
type Keys struct {
	Quit, Back, Help, Palette, Search, Filter, Refresh, Create, FocusNext, FocusPrev, ToggleSidebar key.Binding
	Up, Down, Left, Right, Top, Bottom, PageUp, PageDown, Enter                                     key.Binding

	Edit, State, Assignee, AssignMe, Priority, Labels, Cycle, Project, Comment, CopyID, CopyURL key.Binding
	CopyBranch, Open, Checkout, Archive, Delete, Select, Relation, Link                         key.Binding

	Group, Sort, FilterBar, ClearFilter, MoveLeft, MoveRight, ToggleDone key.Binding
	MarkRead, Snooze, MarkAllRead, Reply                                 key.Binding
}

func bind(keys []string, help, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
}

func DefaultKeys() *Keys {
	return &Keys{
		Quit:          bind([]string{"ctrl+c"}, "ctrl+c", "quit"),
		Back:          bind([]string{"q", "esc"}, "q/esc", "back"),
		Help:          bind([]string{"?"}, "?", "help"),
		Palette:       bind([]string{"ctrl+k", ":"}, "ctrl+k", "commands"),
		Search:        bind([]string{"ctrl+f"}, "ctrl+f", "search"),
		Filter:        bind([]string{"/"}, "/", "filter"),
		Refresh:       bind([]string{"r"}, "r", "refresh"),
		Create:        bind([]string{"c"}, "c", "new issue"),
		FocusNext:     bind([]string{"tab"}, "tab", "focus"),
		FocusPrev:     bind([]string{"shift+tab"}, "shift+tab", "focus"),
		ToggleSidebar: bind([]string{"["}, "[", "sidebar"),

		Up:       bind([]string{"k", "up"}, "↑/k", "up"),
		Down:     bind([]string{"j", "down"}, "↓/j", "down"),
		Left:     bind([]string{"h", "left"}, "←/h", "left"),
		Right:    bind([]string{"l", "right"}, "→/l", "right"),
		Top:      bind([]string{"home"}, "home", "top"),
		Bottom:   bind([]string{"end", "G"}, "end", "bottom"),
		PageUp:   bind([]string{"pgup", "ctrl+u"}, "ctrl+u", "page up"),
		PageDown: bind([]string{"pgdown", "ctrl+d"}, "ctrl+d", "page down"),
		Enter:    bind([]string{"enter"}, "enter", "open"),

		Edit:       bind([]string{"e"}, "e", "edit"),
		State:      bind([]string{"s"}, "s", "status"),
		Assignee:   bind([]string{"a"}, "a", "assignee"),
		AssignMe:   bind([]string{"A"}, "A", "assign me"),
		Priority:   bind([]string{"p"}, "p", "priority"),
		Labels:     bind([]string{"L"}, "L", "labels"),
		Cycle:      bind([]string{"C"}, "C", "cycle"),
		Project:    bind([]string{"P"}, "P", "project"),
		Comment:    bind([]string{"m"}, "m", "comment"),
		CopyID:     bind([]string{"y"}, "y", "copy id"),
		CopyURL:    bind([]string{"Y"}, "Y", "copy url"),
		CopyBranch: bind([]string{"b"}, "b", "copy branch"),
		Open:       bind([]string{"o"}, "o", "open in browser"),
		Checkout:   bind([]string{"B"}, "B", "git checkout"),
		Archive:    bind([]string{"#"}, "#", "archive"),
		Delete:     bind([]string{"D"}, "D", "delete"),
		Select:     bind([]string{"x", "space"}, "x", "select"),
		Relation:   bind([]string{"R"}, "R", "add relation"),
		Link:       bind([]string{"U"}, "U", "link url"),

		Group:       bind([]string{"v"}, "v", "group by"),
		Sort:        bind([]string{"S"}, "S", "sort"),
		FilterBar:   bind([]string{"f"}, "f", "filters"),
		ClearFilter: bind([]string{"F"}, "F", "clear filters"),
		MoveLeft:    bind([]string{"<", "H"}, "</H", "move left"),
		MoveRight:   bind([]string{">"}, ">", "move right"),
		ToggleDone:  bind([]string{"."}, ".", "toggle done"),

		MarkRead:    bind([]string{"u"}, "u", "read/unread"),
		Snooze:      bind([]string{"z"}, "z", "snooze"),
		MarkAllRead: bind([]string{"M"}, "M", "mark all read"),
		Reply:       bind([]string{"m"}, "m", "reply"),
	}
}

// Apply overrides bindings by action name (case-insensitive field name).
func (k *Keys) Apply(overrides map[string][]string) {
	for name, keys := range overrides {
		if b := k.byName(name); b != nil && len(keys) > 0 {
			help := b.Help()
			*b = bind(keys, strings.Join(keys, "/"), help.Desc)
		}
	}
}

func (k *Keys) byName(name string) *key.Binding {
	m := map[string]*key.Binding{
		"quit": &k.Quit, "back": &k.Back, "help": &k.Help, "palette": &k.Palette, "search": &k.Search,
		"filter": &k.Filter, "refresh": &k.Refresh, "create": &k.Create, "focus_next": &k.FocusNext,
		"focus_prev": &k.FocusPrev, "toggle_sidebar": &k.ToggleSidebar, "up": &k.Up, "down": &k.Down,
		"left": &k.Left, "right": &k.Right, "top": &k.Top, "bottom": &k.Bottom, "page_up": &k.PageUp,
		"page_down": &k.PageDown, "enter": &k.Enter, "edit": &k.Edit, "state": &k.State,
		"assignee": &k.Assignee, "assign_me": &k.AssignMe, "priority": &k.Priority, "labels": &k.Labels,
		"cycle": &k.Cycle, "project": &k.Project, "comment": &k.Comment, "copy_id": &k.CopyID,
		"copy_url": &k.CopyURL, "copy_branch": &k.CopyBranch, "open": &k.Open, "checkout": &k.Checkout,
		"archive": &k.Archive, "delete": &k.Delete, "select": &k.Select, "relation": &k.Relation,
		"link": &k.Link, "group": &k.Group, "sort": &k.Sort, "filter_bar": &k.FilterBar,
		"clear_filter": &k.ClearFilter, "move_left": &k.MoveLeft, "move_right": &k.MoveRight,
		"toggle_done": &k.ToggleDone, "mark_read": &k.MarkRead, "snooze": &k.Snooze,
		"mark_all_read": &k.MarkAllRead, "reply": &k.Reply,
	}
	return m[strings.ToLower(name)]
}

// IssueHelp lists the issue-scoped bindings for full help.
func (k *Keys) IssueHelp() []key.Binding {
	return []key.Binding{k.Edit, k.State, k.Assignee, k.AssignMe, k.Priority, k.Labels, k.Cycle, k.Project,
		k.Comment, k.CopyID, k.CopyURL, k.CopyBranch, k.Open, k.Checkout, k.Relation, k.Link, k.Archive, k.Delete}
}

func (k *Keys) GlobalHelp() []key.Binding {
	return []key.Binding{k.Palette, k.Search, k.Create, k.Refresh, k.FocusNext, k.ToggleSidebar, k.Help, k.Back, k.Quit,
		bind([]string{"g"}, "g i/m/t/b/c/p/v/s", "go to inbox/my/team/board/cycles/projects/views/search")}
}
