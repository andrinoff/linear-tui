// Package tui is the root Bubble Tea model: layout, routing, modals, and
// global keys.
package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andrinoff/linear-tui/internal/auth"
	"github.com/andrinoff/linear-tui/internal/config"
	"github.com/andrinoff/linear-tui/internal/git"
	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/components"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
	"github.com/andrinoff/linear-tui/internal/tui/views"
)

const (
	minWidth  = 60
	minHeight = 15
	toastTTL  = 4 * time.Second
)

func bg() context.Context { return context.Background() }

type Options struct {
	Cfg      config.Config
	Store    *auth.Store
	Endpoint string
	OAuth    *auth.OAuth
	// OpenIssue opens an issue (id or identifier) at startup.
	OpenIssue string
	// Service overrides the API client (tests).
	Service *linear.Service
}

type toast struct {
	id   int
	text string
	err  bool
}

type (
	bootMsg struct {
		viewer *linear.Viewer
		teams  []linear.Team
		err    error
	}
	toastExpiredMsg  struct{ id int }
	refreshTickMsg   struct{}
	toggleSidebarMsg struct{}
	branchIssueMsg   struct{ issue *linear.Issue }
)

// App is the root model.
type App struct {
	ctx  *common.Ctx
	opts Options

	stack       []common.View
	root        string
	modal       common.View
	side        sidebar
	sidebarOpen bool
	focusSide   bool
	pendingG    bool

	booting  bool
	loggedIn bool
	toasts   []toast
	toastSeq int
	unread   int
	lastSync time.Time
	created  *linear.Issue
	spin     spinner.Model
	help     help.Model
	w, h     int
}

func New(opts Options) *App {
	keys := common.DefaultKeys()
	keys.Apply(opts.Cfg.Keys)
	cwd, _ := os.Getwd()
	dark := opts.Cfg.Theme != "light"
	ctx := &common.Ctx{
		Cfg:   opts.Cfg,
		State: config.LoadState(),
		Theme: theme.New(dark),
		Keys:  keys,
		Repo:  git.Repo{Dir: cwd},
	}
	a := &App{ctx: ctx, opts: opts, sidebarOpen: true, help: help.New(),
		spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(ctx.Theme.Cursor))}
	a.help.Styles = help.DefaultStyles(dark)
	return a
}

// Ctx exposes the shared context (tests).
func (a *App) Ctx() *common.Ctx { return a.ctx }

func (a *App) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, a.spin.Tick, a.connect())
}

// connect loads credentials and bootstraps the session, or shows login.
func (a *App) connect() tea.Cmd {
	if a.opts.Service != nil {
		a.ctx.Svc = a.opts.Service
		return a.boot()
	}
	creds, err := a.opts.Store.Load()
	if err != nil {
		notice := ""
		if !errors.Is(err, auth.ErrNoCredentials) {
			notice = err.Error()
		}
		return a.showLogin(notice)
	}
	rt, err := a.opts.Store.Transport(bg(), creds, a.opts.OAuth)
	if err != nil {
		return a.showLogin(err.Error())
	}
	a.ctx.Svc = linear.NewService(linear.NewClient(a.opts.Endpoint, rt))
	return a.boot()
}

func (a *App) boot() tea.Cmd {
	a.booting = true
	svc := a.ctx.Svc
	return func() tea.Msg {
		v, err := svc.Viewer(bg())
		if err != nil {
			return bootMsg{err: err}
		}
		teams, err := svc.Teams(bg())
		return bootMsg{viewer: v, teams: teams, err: err}
	}
}

func (a *App) showLogin(notice string) tea.Cmd {
	a.loggedIn, a.booting, a.modal = false, false, nil
	l := views.NewLogin(a.ctx, a.opts.Store, a.opts.Endpoint, a.opts.OAuth, notice)
	a.stack = []common.View{l}
	a.resize()
	return l.Init()
}

func (a *App) top() common.View {
	if len(a.stack) == 0 {
		return nil
	}
	return a.stack[len(a.stack)-1]
}

func (a *App) mainSize() (int, int) {
	w := a.w
	if a.sidebarOpen && a.loggedIn {
		w -= sidebarWidth
	}
	h := a.h - 2
	if !a.loggedIn {
		return a.w, a.h
	}
	return max(1, w), max(1, h)
}

func (a *App) resize() {
	w, h := a.mainSize()
	for _, v := range a.stack {
		v.SetSize(w, h)
	}
	if a.modal != nil {
		a.modal.SetSize(a.w-4, a.h-4)
	}
	a.help.SetWidth(a.w)
}

func (a *App) push(v common.View) tea.Cmd {
	a.stack = append(a.stack, v)
	w, h := a.mainSize()
	v.SetSize(w, h)
	a.focusSide = false
	return v.Init()
}

func (a *App) navigate(target string) tea.Cmd {
	v := a.rootView(target)
	if v == nil {
		return nil
	}
	a.root = target
	a.stack = nil
	a.modal = nil
	for i, it := range navItems {
		if it.target == target {
			a.side.cursor = i
		}
	}
	return a.push(v)
}

func (a *App) rootView(target string) common.View {
	ctx := a.ctx
	team := ctx.Team
	switch target {
	case "inbox":
		return views.NewInbox(ctx)
	case "my":
		return views.NewIssueList(ctx, "My issues", linear.Filter{AssignedToMe: true}, nil).ActiveOnly().WithGroup(views.GroupState)
	case "team":
		if team == nil {
			return views.NewIssueList(ctx, "All issues", linear.Filter{}, nil).ActiveOnly()
		}
		return views.NewIssueList(ctx, team.Name, linear.Filter{TeamID: team.Id}, nil).ActiveOnly().WithGroup(views.GroupState)
	case "board":
		return views.NewBoard(ctx, team, "", "")
	case "cycles":
		return views.NewCycles(ctx, team)
	case "projects":
		return views.NewProjects(ctx)
	case "views":
		return views.NewCustomViews(ctx)
	case "search":
		return views.NewSearch(ctx, "")
	}
	return nil
}

func (a *App) createMsg() tea.Msg {
	d := common.IssueDefaults{}
	if t, ok := a.top().(interface{ Defaults() common.IssueDefaults }); ok {
		d = t.Defaults()
	}
	if d.TeamID == "" && a.ctx.Team != nil {
		d.TeamID = a.ctx.Team.Id
	}
	return common.CreateIssueMsg{Defaults: d}
}

func (a *App) addToast(text string, isErr bool) tea.Cmd {
	a.toastSeq++
	id := a.toastSeq
	a.toasts = append(a.toasts, toast{id: id, text: text, err: isErr})
	if len(a.toasts) > 3 {
		a.toasts = a.toasts[len(a.toasts)-3:]
	}
	return tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpiredMsg{id: id} })
}

func (a *App) scheduleRefresh() tea.Cmd {
	d := a.ctx.Cfg.Refresh()
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

func (a *App) fetchUnread() tea.Cmd {
	svc := a.ctx.Svc
	return func() tea.Msg {
		n, err := svc.UnreadCount(bg())
		if err != nil {
			return nil
		}
		return common.UnreadCountMsg{N: n}
	}
}

// broadcast sends msg to every view in the stack and the modal, so async
// results reach their owner even when it is not on top.
func (a *App) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, v := range a.stack {
		cmds = append(cmds, v.Update(msg))
	}
	if a.modal != nil {
		cmds = append(cmds, a.modal.Update(msg))
	}
	return tea.Batch(cmds...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.resize()
		return a, a.broadcast(msg)
	case tea.BackgroundColorMsg:
		if a.ctx.Cfg.Theme == "" || a.ctx.Cfg.Theme == "auto" {
			a.setTheme(msg.IsDark())
		}
		return a, a.broadcast(msg)
	case spinner.TickMsg:
		if msg.ID == a.spin.ID() {
			var cmd tea.Cmd
			a.spin, cmd = a.spin.Update(msg)
			return a, cmd
		}
		return a, a.broadcast(msg)
	case bootMsg:
		a.booting = false
		if msg.err != nil {
			if common.IsAuthErr(msg.err) {
				return a, a.showLogin("Your session is no longer valid. Please log in again.")
			}
			return a, a.showLogin("Could not connect to Linear: " + common.ErrText(msg.err))
		}
		return a, a.afterBoot(msg)
	case common.LoggedInMsg:
		a.stack = nil
		return a, a.connect()
	case common.LogoutMsg:
		if err := a.opts.Store.Delete(); err != nil {
			return a, a.addToast(err.Error(), true)
		}
		a.ctx.Viewer, a.ctx.Team = nil, nil
		return a, a.showLogin("Logged out.")
	case toastExpiredMsg:
		for i, t := range a.toasts {
			if t.id == msg.id {
				a.toasts = append(a.toasts[:i], a.toasts[i+1:]...)
				break
			}
		}
		return a, nil
	case common.ToastMsg:
		return a, a.addToast(msg.Text, msg.Err)
	case common.ErrMsg:
		if common.IsAuthErr(msg.Err) && a.opts.Service == nil {
			return a, a.showLogin("Authentication failed. Please log in again.")
		}
		return a, a.addToast(common.ErrText(msg.Err), true)
	case refreshTickMsg:
		cmds := []tea.Cmd{a.scheduleRefresh()}
		if a.loggedIn && a.modal == nil {
			cmds = append(cmds, a.fetchUnread())
			if top := a.top(); top != nil && !top.Capturing() {
				a.lastSync = time.Now()
				cmds = append(cmds, top.Update(common.RefreshMsg{Silent: true}))
			}
		}
		return a, tea.Batch(cmds...)
	case common.RefreshMsg:
		a.ctx.Svc.Invalidate()
		a.lastSync = time.Now()
		var cmd tea.Cmd
		if top := a.top(); top != nil {
			cmd = top.Update(msg)
		}
		return a, tea.Batch(cmd, a.fetchUnread())
	case common.UnreadCountMsg:
		a.unread = msg.N
		return a, nil
	case common.PushMsg:
		return a, a.push(msg.View)
	case common.PopMsg:
		return a, a.pop()
	case common.NavigateMsg:
		return a, a.navigate(msg.Target)
	case common.ModalMsg:
		a.modal = msg.Modal
		a.modal.SetSize(a.w-4, a.h-4)
		return a, a.modal.Init()
	case common.CloseModalMsg:
		a.modal = nil
		return a, nil
	case common.ShowHelpMsg:
		var vh []key.Binding
		if top := a.top(); top != nil {
			vh = top.Help()
		}
		return a, a.openModal(components.NewHelpOverlay(a.ctx, vh))
	case common.ToggleThemeMsg:
		a.setTheme(!a.ctx.Theme.Dark)
		return a, nil
	case toggleSidebarMsg:
		a.sidebarOpen = !a.sidebarOpen
		a.resize()
		return a, nil
	case common.TeamChangedMsg:
		a.ctx.Team = msg.Team
		a.ctx.State.LastTeam = msg.Team.Id
		return a, tea.Batch(a.navigate(a.root), a.addToast("Switched to "+msg.Team.Name, false))
	case common.OpenIssueMsg:
		return a, a.push(views.NewIssueDetail(a.ctx, msg.ID, nil))
	case common.CreateIssueMsg:
		return a, a.push(views.NewIssueForm(a.ctx, msg.Defaults))
	case common.EditIssueMsg:
		return a, a.push(views.EditIssueForm(a.ctx, msg.Issue, msg.Description))
	case common.IssueCreatedMsg:
		iss := msg.Issue
		a.created = &iss
		return a, tea.Batch(a.broadcast(msg), a.addToast("Created "+iss.Identifier+" · ctrl+o to open", false))
	case common.MutationMsg:
		return a, a.mutationResult(msg)
	case branchIssueMsg:
		if msg.issue != nil {
			return a, tea.Batch(a.push(views.NewIssueDetail(a.ctx, msg.issue.Id, msg.issue)),
				a.addToast("Opened "+msg.issue.Identifier+" from current git branch", false))
		}
		return a, nil
	case tea.KeyPressMsg:
		return a, a.handleKey(msg)
	case tea.PasteMsg:
		if a.modal != nil {
			return a, a.modal.Update(msg)
		}
		if top := a.top(); top != nil {
			return a, top.Update(msg)
		}
		return a, nil
	case tea.MouseClickMsg:
		return a, a.handleClick(msg.Mouse())
	case tea.MouseWheelMsg:
		if a.modal != nil {
			return a, nil
		}
		k := tea.KeyPressMsg{Code: tea.KeyDown}
		if msg.Mouse().Button == tea.MouseWheelUp {
			k = tea.KeyPressMsg{Code: tea.KeyUp}
		}
		if top := a.top(); top != nil && !top.Capturing() {
			return a, top.Update(k)
		}
		return a, nil
	}
	return a, a.broadcast(msg)
}

func (a *App) openModal(m common.View) tea.Cmd {
	a.modal = m
	m.SetSize(a.w-4, a.h-4)
	return m.Init()
}

func (a *App) setTheme(dark bool) {
	a.ctx.Theme = theme.New(dark)
	a.help.Styles = help.DefaultStyles(dark)
	a.spin.Style = a.ctx.Theme.Cursor
}

func (a *App) pop() tea.Cmd {
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
		a.resize()
	}
	return nil
}

func (a *App) afterBoot(msg bootMsg) tea.Cmd {
	ctx := a.ctx
	ctx.Viewer = msg.viewer
	a.loggedIn = true
	a.lastSync = time.Now()
	ctx.Team = pickTeam(msg.teams, ctx.State.LastTeam, ctx.Cfg.DefaultTeam)
	target := ctx.State.LastView
	if a.rootView(target) == nil {
		target = "my"
	}
	cmds := []tea.Cmd{a.navigate(target), a.fetchUnread(), a.scheduleRefresh()}
	switch {
	case a.opts.OpenIssue != "":
		cmds = append(cmds, a.push(views.NewIssueDetail(ctx, a.opts.OpenIssue, nil)))
	case ctx.Cfg.Git.OpenFromBranch:
		cmds = append(cmds, a.issueFromBranch())
	}
	return tea.Batch(cmds...)
}

func pickTeam(teams []linear.Team, last, def string) *linear.Team {
	for _, want := range []string{last, def} {
		if want == "" {
			continue
		}
		for i := range teams {
			t := &teams[i]
			if t.Id == want || strings.EqualFold(t.Key, want) || strings.EqualFold(t.Name, want) {
				return t
			}
		}
	}
	if len(teams) > 0 {
		return &teams[0]
	}
	return nil
}

func (a *App) issueFromBranch() tea.Cmd {
	repo, svc := a.ctx.Repo, a.ctx.Svc
	return func() tea.Msg {
		branch, err := repo.CurrentBranch()
		if err != nil || branch == "" || branch == "HEAD" {
			return nil
		}
		if iss, err := svc.IssueByBranch(bg(), branch); err == nil && iss != nil {
			return branchIssueMsg{issue: iss}
		}
		if id, ok := linear.ParseIdentifier(branch); ok {
			if d, err := svc.Issue(bg(), id); err == nil {
				return branchIssueMsg{issue: &d.IssueFields}
			}
		}
		return nil
	}
}

func (a *App) mutationResult(msg common.MutationMsg) tea.Cmd {
	stale := msg.Seq != a.ctx.CurrentSeq(msg.Orig.Id)
	if msg.Err != nil {
		var cmds []tea.Cmd
		if !stale {
			cmds = append(cmds, a.broadcast(common.IssueUpdatedMsg{Issue: msg.Orig}))
		}
		cmds = append(cmds, a.addToast(msg.Orig.Identifier+": "+common.ErrText(msg.Err), true))
		return tea.Batch(cmds...)
	}
	if stale || msg.Result == nil {
		return nil
	}
	return a.broadcast(common.IssueUpdatedMsg{Issue: *msg.Result})
}

func (a *App) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := a.ctx.Keys
	if key.Matches(msg, k.Quit) {
		return tea.Quit
	}
	if a.modal != nil {
		return a.modal.Update(msg)
	}
	top := a.top()
	if top == nil {
		return nil
	}
	if !a.loggedIn || top.Capturing() {
		return top.Update(msg)
	}
	if a.pendingG {
		a.pendingG = false
		targets := map[string]string{"i": "inbox", "m": "my", "t": "team", "b": "board", "c": "cycles", "p": "projects", "v": "views", "s": "search"}
		if t, ok := targets[msg.String()]; ok {
			return a.navigate(t)
		}
		if msg.String() == "g" {
			return top.Update(keyPress(k.Top))
		}
		return nil
	}
	switch {
	case msg.String() == "g":
		a.pendingG = true
		return nil
	case key.Matches(msg, k.Palette):
		return a.openModal(a.palette())
	case key.Matches(msg, k.Search):
		return a.push(views.NewSearch(a.ctx, ""))
	case key.Matches(msg, k.Help):
		return common.Cmd(common.ShowHelpMsg{})
	case key.Matches(msg, k.Refresh):
		return common.Cmd(common.RefreshMsg{})
	case key.Matches(msg, k.Create):
		return common.Cmd(a.createMsg())
	case key.Matches(msg, k.ToggleSidebar):
		a.sidebarOpen = !a.sidebarOpen
		a.focusSide = a.focusSide && a.sidebarOpen
		a.resize()
		return nil
	case key.Matches(msg, k.FocusNext, k.FocusPrev):
		if a.sidebarOpen {
			a.focusSide = !a.focusSide
		}
		return nil
	case msg.String() == "ctrl+o" && a.created != nil:
		return a.push(views.NewIssueDetail(a.ctx, a.created.Id, a.created))
	}
	if a.focusSide {
		return a.sidebarKey(msg)
	}
	if key.Matches(msg, k.Back) {
		if h, ok := top.(interface{ HandlesEsc() bool }); ok && msg.String() == "esc" && h.HandlesEsc() {
			return top.Update(msg)
		}
		if len(a.stack) > 1 {
			return a.pop()
		}
		if msg.String() == "q" {
			return tea.Quit
		}
		return nil
	}
	return top.Update(msg)
}

func (a *App) sidebarKey(msg tea.KeyPressMsg) tea.Cmd {
	k := a.ctx.Keys
	switch {
	case key.Matches(msg, k.Down):
		a.side.cursor = min(len(navItems)-1, a.side.cursor+1)
	case key.Matches(msg, k.Up):
		a.side.cursor = max(0, a.side.cursor-1)
	case key.Matches(msg, k.Enter), key.Matches(msg, k.Right):
		a.focusSide = false
		return a.navigate(navItems[a.side.cursor].target)
	case key.Matches(msg, k.Back):
		a.focusSide = false
	}
	return nil
}

func (a *App) handleClick(m tea.Mouse) tea.Cmd {
	if a.modal != nil || !a.loggedIn {
		return nil
	}
	if a.sidebarOpen && m.X < sidebarWidth {
		if i, ok := a.side.clickRows[m.Y]; ok {
			if i == -2 {
				return a.openModal(a.teamPicker())
			}
			return a.navigate(navItems[i].target)
		}
		return nil
	}
	a.focusSide = false
	x := m.X
	if a.sidebarOpen {
		x -= sidebarWidth
	}
	if top := a.top(); top != nil {
		return top.Update(common.ClickMsg{X: x, Y: m.Y - 1})
	}
	return nil
}

// Save persists UI state; call after the program exits.
func (a *App) Save() error {
	if a.root != "" {
		a.ctx.State.LastView = a.root
	}
	return a.ctx.State.Save()
}

func (a *App) View() tea.View {
	v := tea.NewView(a.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "linear-tui"
	return v
}

func (a *App) render() string {
	t := a.ctx.Theme
	if a.w == 0 {
		return ""
	}
	if a.w < minWidth || a.h < minHeight {
		return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center,
			t.Dim.Render("Terminal too small.\nNeed at least 60×15."))
	}
	if a.booting {
		return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, a.spin.View()+" Connecting to Linear…")
	}
	top := a.top()
	var base string
	if !a.loggedIn {
		if top != nil {
			base = top.View()
		}
	} else {
		mw, mh := a.mainSize()
		var crumbs []string
		for _, v := range a.stack {
			crumbs = append(crumbs, v.Title())
		}
		bar := t.Title.Render(" " + strings.Join(crumbs, t.Dim.Render(" › ")))
		if a.pendingG {
			bar += t.Dim.Render("   g…")
		}
		content := ""
		if top != nil {
			content = top.View()
		}
		main := common.Fit(bar, mw) + "\n" + common.FitLines(content, mh)
		if a.sidebarOpen {
			side := a.side.view(a.ctx, a.root, a.focusSide, a.unread, a.h-1)
			base = lipgloss.JoinHorizontal(lipgloss.Top, side, main)
		} else {
			base = main
		}
		base = common.FitLines(base, a.h-1) + "\n" + a.statusBar()
	}
	layers := []*lipgloss.Layer{lipgloss.NewLayer(base)}
	if a.modal != nil {
		m := a.modal.View()
		mw, mh := lipgloss.Size(m)
		layers = append(layers, lipgloss.NewLayer(m).X(max(0, (a.w-mw)/2)).Y(max(0, (a.h-mh)/3)).Z(1))
	}
	y := a.h - 2
	for i := len(a.toasts) - 1; i >= 0; i-- {
		tt := a.toasts[i]
		style := t.Toast
		if tt.err {
			style = t.ToastErr
		}
		box := style.MaxWidth(min(60, a.w-4)).Render(tt.text)
		bw, bh := lipgloss.Size(box)
		y -= bh
		layers = append(layers, lipgloss.NewLayer(box).X(max(0, a.w-bw-1)).Y(max(0, y)).Z(2))
	}
	if len(layers) == 1 {
		return base
	}
	return lipgloss.NewCompositor(layers...).Render()
}

func (a *App) statusBar() string {
	t := a.ctx.Theme
	var left []string
	if v := a.ctx.Viewer; v != nil {
		left = append(left, v.Organization.Name)
	}
	if a.ctx.Team != nil {
		left = append(left, a.ctx.Team.Key)
	}
	if !a.lastSync.IsZero() {
		left = append(left, "synced "+common.Ago(a.lastSync))
	}
	l := t.StatusBar.Render(" " + strings.Join(left, " · ") + " ")
	var bindings []key.Binding
	if a.modal != nil {
		bindings = a.modal.Help()
	} else if a.focusSide {
		bindings = []key.Binding{a.ctx.Keys.Up, a.ctx.Keys.Down, a.ctx.Keys.Enter, a.ctx.Keys.FocusNext}
	} else if top := a.top(); top != nil {
		bindings = top.Help()
	}
	bindings = append(bindings, a.ctx.Keys.Palette, a.ctx.Keys.Help)
	a.help.SetWidth(max(10, a.w-lipgloss.Width(l)-1))
	return l + a.help.ShortHelpView(bindings)
}
