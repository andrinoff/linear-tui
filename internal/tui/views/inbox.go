package views

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/components"
)

// Inbox lists the viewer's notifications.
type Inbox struct {
	cursorList
	items   []linear.Notification
	read    map[string]bool
	cursorS string
	hasMore bool
	paging  bool
}

type inboxLoadedMsg struct {
	owner  *Inbox
	reqID  int
	page   linear.Page[linear.Notification]
	append bool
	err    error
}

func NewInbox(ctx *common.Ctx) *Inbox {
	return &Inbox{cursorList: newCursorList(ctx), read: map[string]bool{}}
}

func (b *Inbox) Title() string { return "Inbox" }
func (b *Inbox) Help() []key.Binding {
	k := b.ctx.Keys
	return []key.Binding{k.Enter, k.MarkRead, k.Snooze, key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "archive")), k.MarkAllRead, k.Open}
}

func (b *Inbox) Init() tea.Cmd { return tea.Batch(b.load(false), b.spin.Tick) }

func (b *Inbox) load(more bool) tea.Cmd {
	b.reqID++
	req := b.reqID
	cursor := ""
	if more {
		cursor, b.paging = b.cursorS, true
	}
	return func() tea.Msg {
		p, err := b.ctx.Svc.Notifications(bg(), cursor)
		return inboxLoadedMsg{owner: b, reqID: req, page: p, append: more, err: err}
	}
}

func (b *Inbox) isRead(n linear.Notification) bool {
	if r, ok := b.read[n.GetId()]; ok {
		return r
	}
	return !n.GetReadAt().IsZero()
}

func (b *Inbox) unread() int {
	c := 0
	for _, n := range b.items {
		if !b.isRead(n) {
			c++
		}
	}
	return c
}

func (b *Inbox) current() linear.Notification {
	if b.cursor < len(b.items) {
		return b.items[b.cursor]
	}
	return nil
}

func (b *Inbox) remove(id string) {
	for i, n := range b.items {
		if n.GetId() == id {
			b.items = append(b.items[:i], b.items[i+1:]...)
			break
		}
	}
	b.n = len(b.items)
}

func (b *Inbox) refreshCount() tea.Cmd {
	return func() tea.Msg {
		n, err := b.ctx.Svc.UnreadCount(bg())
		if err != nil {
			return nil
		}
		return common.UnreadCountMsg{N: n}
	}
}

func (b *Inbox) setRead(n linear.Notification, read bool) tea.Cmd {
	id := n.GetId()
	b.read[id] = read
	return func() tea.Msg {
		if err := b.ctx.Svc.MarkNotification(bg(), id, read); err != nil {
			delete(b.read, id)
			return common.ErrMsg{Err: err}
		}
		c, err := b.ctx.Svc.UnreadCount(bg())
		if err != nil {
			return nil
		}
		return common.UnreadCountMsg{N: c}
	}
}

func (b *Inbox) Update(msg tea.Msg) tea.Cmd {
	if cmd, ok := b.tick(msg); ok {
		return cmd
	}
	if b.nav(msg) {
		if b.hasMore && !b.paging && b.cursor >= b.n-10 {
			return b.load(true)
		}
		return nil
	}
	switch msg := msg.(type) {
	case inboxLoadedMsg:
		if msg.owner != b || msg.reqID != b.reqID {
			return nil
		}
		b.loading, b.paging, b.err = false, false, msg.err
		if msg.err != nil {
			return common.Err(msg.err)
		}
		if msg.append {
			b.items = append(b.items, msg.page.Items...)
		} else {
			b.items = msg.page.Items
			b.read = map[string]bool{}
		}
		b.n, b.cursorS, b.hasMore = len(b.items), msg.page.Cursor, msg.page.HasMore
		return nil
	case common.RefreshMsg:
		return tea.Batch(b.load(false), b.refreshCount())
	case tea.KeyPressMsg:
		return b.handleKey(msg)
	}
	return nil
}

func (b *Inbox) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := b.ctx.Keys
	if key.Matches(msg, k.MarkAllRead) {
		var ids []string
		for _, n := range b.items {
			if !b.isRead(n) {
				ids = append(ids, n.GetId())
				b.read[n.GetId()] = true
			}
		}
		return func() tea.Msg {
			if err := b.ctx.Svc.MarkAllRead(bg(), ids); err != nil {
				return common.ErrMsg{Err: err}
			}
			return tea.BatchMsg{common.Cmd(common.UnreadCountMsg{N: 0}), common.Toast("Marked all as read")}
		}
	}
	n := b.current()
	if n == nil {
		return nil
	}
	in, isIssue := n.(*linear.NotificationFieldsIssueNotification)
	switch {
	case key.Matches(msg, k.Enter):
		var cmds []tea.Cmd
		if !b.isRead(n) {
			cmds = append(cmds, b.setRead(n, true))
		}
		if isIssue {
			iss := in.Issue
			cmds = append(cmds, common.Cmd(common.PushMsg{View: NewIssueDetail(b.ctx, iss.Id, &iss)}))
		} else if n.GetUrl() != "" {
			cmds = append(cmds, common.OpenURL(n.GetUrl()))
		}
		return tea.Batch(cmds...)
	case key.Matches(msg, k.MarkRead):
		return b.setRead(n, !b.isRead(n))
	case key.Matches(msg, k.Snooze):
		id := n.GetId()
		return modal(components.NewPicker(b.ctx, "Snooze until", snoozeOptions(time.Now()), func(sel []components.Item) tea.Cmd {
			until := sel[0].Value.(time.Time)
			b.remove(id)
			return func() tea.Msg {
				if err := b.ctx.Svc.SnoozeNotification(bg(), id, until); err != nil {
					return common.ErrMsg{Err: err}
				}
				return common.ToastMsg{Text: "Snoozed until " + until.Format("Mon Jan 2 15:04")}
			}
		}))
	case msg.String() == "d":
		id := n.GetId()
		b.remove(id)
		return func() tea.Msg {
			if err := b.ctx.Svc.ArchiveNotification(bg(), id); err != nil {
				return common.ErrMsg{Err: err}
			}
			c, _ := b.ctx.Svc.UnreadCount(bg())
			return tea.BatchMsg{common.Toast("Archived"), common.Cmd(common.UnreadCountMsg{N: c})}
		}
	case key.Matches(msg, k.Open):
		if n.GetUrl() != "" {
			return common.OpenURL(n.GetUrl())
		}
	default:
		if isIssue {
			cmd, _ := IssueAction(b.ctx, msg, []linear.Issue{in.Issue})
			return cmd
		}
	}
	return nil
}

// humanizeType turns "issueStatusChanged" into "status changed".
func humanizeType(s string) string {
	s = strings.TrimPrefix(s, "issue")
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteRune(' ')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func (b *Inbox) View() string {
	t := b.ctx.Theme
	header := []string{t.Dim.Render(" " + strconv.Itoa(b.unread()) + " unread · enter open · u read/unread · z snooze · d archive · M mark all read")}
	return b.render(header, "Inbox zero. Nothing to see here.", func(i int, _ bool) string {
		n := b.items[i]
		read := b.isRead(n)
		dot := t.Cursor.Render("●")
		if read {
			dot = " "
		}
		actor := ""
		if a := n.GetActor(); a != nil {
			actor = a.DisplayName
		}
		title := n.GetTitle()
		id := ""
		if in, ok := n.(*linear.NotificationFieldsIssueNotification); ok {
			id = in.Issue.Identifier
			if title == "" {
				title = in.Issue.Title
			}
		}
		if !read {
			title = t.Bold.Render(title)
		}
		return dot + " " + common.Fit(t.Dim.Render(id), 9) + " " + common.Fit(title, max(20, b.w-58)) + " " +
			common.Fit(t.Dim.Render(humanizeType(n.GetType())), 20) + " " + common.Fit(t.Dim.Render(actor), 14) + " " + t.Dim.Render(common.Ago(n.GetCreatedAt()))
	})
}
