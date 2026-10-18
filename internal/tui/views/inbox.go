package views

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/components"
)

const (
	// inboxListW is the notification list width in split view, like
	// Linear's inbox column.
	inboxListW = 46
	// inboxRowH is the height of one notification: two lines and a gap.
	inboxRowH = 3
	// previewDelay keeps fast scrolling from loading (and marking read)
	// every notification passed over.
	previewDelay = 300 * time.Millisecond
)

var (
	focusPreviewKey = key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("→/l", "focus issue"))
	focusListKey    = key.NewBinding(key.WithKeys("h", "left", "esc"), key.WithHelp("←/h", "back to list"))
	archiveKey      = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "archive"))
)

// Inbox mirrors Linear's inbox: notifications on the left and, on wide
// screens, the selected issue on the right. Previewing a notification
// marks it read, as in Linear.
type Inbox struct {
	cursorList
	items   []linear.Notification
	read    map[string]bool
	cursorS string
	hasMore bool
	paging  bool

	preview      *IssueDetail
	previewID    string
	previewSeq   int
	focusPreview bool
}

type inboxLoadedMsg struct {
	owner  *Inbox
	reqID  int
	page   linear.Page[linear.Notification]
	append bool
	err    error
}

type previewTickMsg struct {
	owner *Inbox
	seq   int
}

func NewInbox(ctx *common.Ctx) *Inbox {
	return &Inbox{cursorList: newCursorList(ctx), read: map[string]bool{}}
}

func (b *Inbox) Title() string { return "Inbox" }

func (b *Inbox) split() bool { return b.w >= 100 }

func (b *Inbox) listW() int {
	if b.split() {
		return inboxListW
	}
	return b.w
}

func (b *Inbox) previewW() int { return max(1, b.w-inboxListW-2) }

func (b *Inbox) SetSize(w, h int) {
	b.w, b.h = w, h
	if b.preview != nil {
		b.preview.SetSize(b.previewW(), h)
	}
}

// HandlesEsc lets esc leave the preview pane instead of the screen.
func (b *Inbox) HandlesEsc() bool { return b.focusPreview }

func (b *Inbox) Help() []key.Binding {
	k := b.ctx.Keys
	if b.focusPreview && b.preview != nil {
		return append([]key.Binding{focusListKey}, b.preview.Help()...)
	}
	keys := []key.Binding{k.Enter}
	if b.split() {
		keys = append(keys, focusPreviewKey)
	}
	return append(keys, k.MarkRead, k.Snooze, archiveKey, k.MarkAllRead, k.Open)
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

func issueOf(n linear.Notification) (*linear.Issue, bool) {
	if in, ok := n.(*linear.NotificationFieldsIssueNotification); ok && in.Issue.Id != "" {
		return &in.Issue, true
	}
	return nil, false
}

func (b *Inbox) remove(id string) {
	for i, n := range b.items {
		if n.GetId() == id {
			b.items = append(b.items[:i], b.items[i+1:]...)
			break
		}
	}
	b.n = len(b.items)
	b.cursor = min(b.cursor, max(0, b.n-1))
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

// schedulePreview loads the selected notification's issue after a pause.
func (b *Inbox) schedulePreview() tea.Cmd {
	if !b.split() {
		return nil
	}
	b.previewSeq++
	seq := b.previewSeq
	return tea.Tick(previewDelay, func(time.Time) tea.Msg { return previewTickMsg{owner: b, seq: seq} })
}

func (b *Inbox) showPreview() tea.Cmd {
	n := b.current()
	if n == nil {
		b.preview, b.previewID = nil, ""
		return nil
	}
	var cmds []tea.Cmd
	if !b.isRead(n) {
		cmds = append(cmds, b.setRead(n, true))
	}
	iss, ok := issueOf(n)
	switch {
	case !ok:
		b.preview, b.previewID = nil, ""
	case iss.Id != b.previewID:
		b.preview = NewIssueDetail(b.ctx, iss.Id, iss)
		b.preview.Embedded = true
		b.previewID = iss.Id
		b.preview.SetSize(b.previewW(), b.h)
		cmds = append(cmds, b.preview.Init())
	}
	return tea.Batch(cmds...)
}

func (b *Inbox) Update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(spinner.TickMsg); ok && m.ID == b.spin.ID() {
		cmd, _ := b.tick(msg)
		return cmd
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
		first := len(b.items) == 0
		if msg.append {
			b.items = append(b.items, msg.page.Items...)
		} else {
			b.items = msg.page.Items
			b.read = map[string]bool{}
		}
		b.n, b.cursorS, b.hasMore = len(b.items), msg.page.Cursor, msg.page.HasMore
		b.cursor = min(b.cursor, max(0, b.n-1))
		if first {
			return b.schedulePreview()
		}
		return nil
	case previewTickMsg:
		if msg.owner != b || msg.seq != b.previewSeq {
			return nil
		}
		return b.showPreview()
	case common.RefreshMsg:
		cmds := []tea.Cmd{b.load(false), b.refreshCount()}
		if b.preview != nil {
			cmds = append(cmds, b.preview.Update(msg))
		}
		return tea.Batch(cmds...)
	case common.ClickMsg:
		if msg.X >= b.listW() {
			b.focusPreview = b.preview != nil
			if b.preview != nil {
				msg.X -= b.listW() + 2
				return b.preview.Update(msg)
			}
			return nil
		}
		b.focusPreview = false
		if i := b.offset + (msg.Y-b.top)/inboxRowH; msg.Y >= b.top && i >= 0 && i < b.n {
			if i == b.cursor && !b.split() {
				return b.open()
			}
			b.cursor = i
			return b.schedulePreview()
		}
		return nil
	case tea.KeyPressMsg:
		return b.handleKey(msg)
	}
	if b.preview != nil {
		return b.preview.Update(msg)
	}
	return nil
}

func (b *Inbox) open() tea.Cmd {
	n := b.current()
	if n == nil {
		return nil
	}
	var cmds []tea.Cmd
	if !b.isRead(n) {
		cmds = append(cmds, b.setRead(n, true))
	}
	if iss, ok := issueOf(n); ok {
		cmds = append(cmds, common.Cmd(common.PushMsg{View: NewIssueDetail(b.ctx, iss.Id, iss)}))
	} else if n.GetUrl() != "" {
		cmds = append(cmds, common.OpenURL(n.GetUrl()))
	}
	return tea.Batch(cmds...)
}

func (b *Inbox) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := b.ctx.Keys
	if b.focusPreview && b.preview != nil {
		if key.Matches(msg, focusListKey) {
			b.focusPreview = false
			return nil
		}
		return b.preview.Update(msg)
	}
	before := b.cursor
	if b.nav(msg) {
		var cmds []tea.Cmd
		if b.cursor != before {
			cmds = append(cmds, b.schedulePreview())
		}
		if b.hasMore && !b.paging && b.cursor >= b.n-10 {
			cmds = append(cmds, b.load(true))
		}
		return tea.Batch(cmds...)
	}
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
	iss, isIssue := issueOf(n)
	switch {
	case key.Matches(msg, focusPreviewKey) && b.split() && b.preview != nil:
		b.focusPreview = true
	case key.Matches(msg, k.Enter):
		return b.open()
	case key.Matches(msg, k.MarkRead):
		return b.setRead(n, !b.isRead(n))
	case key.Matches(msg, k.Snooze):
		id := n.GetId()
		return modal(components.NewPicker(b.ctx, "Snooze until", snoozeOptions(time.Now()), func(sel []components.Item) tea.Cmd {
			until := sel[0].Value.(time.Time)
			b.remove(id)
			return tea.Batch(b.schedulePreview(), func() tea.Msg {
				if err := b.ctx.Svc.SnoozeNotification(bg(), id, until); err != nil {
					return common.ErrMsg{Err: err}
				}
				return common.ToastMsg{Text: "Snoozed until " + until.Format("Mon Jan 2 15:04")}
			})
		}))
	case key.Matches(msg, archiveKey):
		id := n.GetId()
		b.remove(id)
		return tea.Batch(b.schedulePreview(), func() tea.Msg {
			if err := b.ctx.Svc.ArchiveNotification(bg(), id); err != nil {
				return common.ErrMsg{Err: err}
			}
			c, _ := b.ctx.Svc.UnreadCount(bg())
			return tea.BatchMsg{common.Toast("Archived"), common.Cmd(common.UnreadCountMsg{N: c})}
		})
	case key.Matches(msg, k.Open):
		if n.GetUrl() != "" {
			return common.OpenURL(n.GetUrl())
		}
	default:
		if isIssue {
			cmd, _ := IssueAction(b.ctx, msg, []linear.Issue{*iss})
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

var (
	mdLink  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdNoise = regexp.MustCompile("[*_`#>~]+")
)

// snippet flattens markdown to one line of plain text.
func snippet(md string) string {
	md = mdLink.ReplaceAllString(md, "$1")
	md = mdNoise.ReplaceAllString(md, "")
	return strings.Join(strings.Fields(md), " ")
}

var verbs = map[string]string{
	"issueNewComment":        "commented",
	"issueCommentMention":    "mentioned you",
	"issueMention":           "mentioned you",
	"issueAssignedToYou":     "assigned you",
	"issueUnassignedFromYou": "unassigned you",
	"issueStatusChanged":     "changed the status",
	"issueCreated":           "created the issue",
	"issueSubscribed":        "subscribed you",
	"issueDue":               "set it due",
	"issueBlocking":          "marked it blocking",
	"issueUnblocked":         "unblocked it",
	"issueCommentReaction":   "reacted",
	"issueEmojiReaction":     "reacted",
}

// activity describes a notification like Linear's second line, for example
// "Kieran replied: thats not a bad idea".
func activity(n linear.Notification) string {
	actor := "Linear"
	if a := n.GetActor(); a != nil {
		actor = a.DisplayName
	}
	verb := verbs[n.GetType()]
	if verb == "" {
		verb = humanizeType(n.GetType())
	}
	in, ok := n.(*linear.NotificationFieldsIssueNotification)
	if !ok {
		if s := n.GetSubtitle(); s != "" {
			return actor + " " + s
		}
		return actor + " " + verb
	}
	if in.ParentCommentId != "" && verb == "commented" {
		verb = "replied"
	}
	if in.ReactionEmoji != "" {
		verb += " " + in.ReactionEmoji
	}
	if in.Comment != nil && in.Comment.Body != "" {
		return actor + " " + verb + ": " + snippet(in.Comment.Body)
	}
	return actor + " " + verb
}

// row renders a notification as Linear does: the actor's avatar, the issue
// with its status icon, then the activity and its age.
func (b *Inbox) row(n linear.Notification, w int) []string {
	t := b.ctx.Theme
	name, url := "", ""
	if a := n.GetActor(); a != nil {
		name, url = a.DisplayName, a.AvatarUrl
	}
	title, status := n.GetTitle(), " "
	if iss, ok := issueOf(n); ok {
		title = t.Dim.Render(iss.Identifier) + " " + iss.Title
		status = t.StateIcon(iss.State.Type, iss.State.Color)
	}
	dot := " "
	if b.isRead(n) {
		title = t.App.Render(title)
	} else {
		dot = t.Cursor.Render("•")
		title = t.Bold.Render(title)
	}
	age := t.Faint.Render(common.Ago(n.GetCreatedAt()))
	line1 := dot + t.Avatar(name, url) + " " + common.Fit(title, w-7) + " " + status + " "
	line2 := "    " + common.Fit(t.Dim.Render(activity(n)), w-6-lipgloss.Width(age)) + " " + age + " "
	return []string{line1, line2}
}

func (b *Inbox) renderList(w int) string {
	t := b.ctx.Theme
	head := " " + t.Bold.Render(strconv.Itoa(b.unread())+" unread") +
		t.Faint.Render("   u read  z snooze  d archive  M read all")
	lines := []string{common.Fit(head, w), ""}
	switch {
	case b.loading && b.n == 0:
		lines = append(lines, " "+b.spin.View()+t.Dim.Render(" Loading…"))
	case b.err != nil && b.n == 0:
		lines = append(lines, " "+t.Error.Render(common.ErrText(b.err)), " "+t.Dim.Render("Press r to retry."))
	case b.n == 0:
		lines = append(lines, "", lipgloss.PlaceHorizontal(w, lipgloss.Center, t.Dim.Render("You're all caught up.")))
	default:
		visible := max(1, (b.h-len(lines))/inboxRowH)
		b.offset = common.Scroll(b.cursor, b.offset, visible, b.n)
		b.top = len(lines)
		for i := b.offset; i < min(b.n, b.offset+visible); i++ {
			row := b.row(b.items[i], w)
			for j := range row {
				row[j] = common.Fit(row[j], w)
				if i == b.cursor {
					row[j] = t.Select(row[j], w)
				}
			}
			lines = append(lines, row[0], row[1], "")
		}
	}
	return common.FitLines(strings.Join(lines, "\n"), b.h)
}

func (b *Inbox) View() string {
	t := b.ctx.Theme
	if !b.split() {
		return b.renderList(b.w)
	}
	list := b.renderList(inboxListW)
	sepColor := t.Border
	if b.focusPreview {
		sepColor = t.Accent
	}
	sep := strings.TrimSuffix(strings.Repeat(t.Fgc(sepColor, "│")+"\n", b.h), "\n")
	var right string
	switch n := b.current(); {
	case b.preview != nil:
		right = b.preview.View()
	case n != nil:
		right = "\n" + t.Bold.Render(n.GetTitle()) + "\n" + t.Dim.Render(activity(n)) + "\n\n" + t.Faint.Render("Press o to open in the browser.")
	default:
		right = "\n" + lipgloss.PlaceHorizontal(b.previewW(), lipgloss.Center, t.Faint.Render("No notifications"))
	}
	right = strings.ReplaceAll(common.FitLines(right, b.h), "\n", "\n ")
	return lipgloss.JoinHorizontal(lipgloss.Top, list, sep, " "+right)
}
