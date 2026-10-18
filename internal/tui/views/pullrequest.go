package views

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andrinoff/linear-tui/internal/github"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/components"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

// GitHub's own colors for pull request state and diffs. They carry meaning,
// so they apply in monochrome themes too.
var (
	prColors = map[string]string{"open": "#4CB782", "draft": "#8A8F98", "merged": "#8B5CF6", "closed": "#EB5757", "approved": "#4CB782", "inReview": "#4CB782"}
	diffAdd  = lipgloss.Color("#4CB782")
	diffDel  = lipgloss.Color("#EB5757")
)

var (
	prTabKey  = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "overview/diff"))
	nextFile  = key.NewBinding(key.WithKeys("]"), key.WithHelp("]/[", "next/prev file"))
	prevFile  = key.NewBinding(key.WithKeys("["))
	prComment = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "comment"))
)

// PRIcon renders a pull request glyph colored by status (open, draft,
// merged, closed).
func PRIcon(t *theme.Theme, status string) string {
	c, ok := prColors[status]
	if !ok {
		return t.Dim.Render("⑂")
	}
	return t.Fgc(lipgloss.Color(c), "⑂")
}

// PullRequest shows a GitHub pull request like Linear's PR page: an
// Overview tab with the description and activity, a Diff tab, and a
// properties pane with status, reviewers, checks, and changed files.
type PullRequest struct {
	ctx    *common.Ctx
	url    string
	ref    github.Ref
	title  string
	status string

	pr      *github.PR
	err     error
	loading bool
	reqID   int

	tab         int
	diff        string
	diffErr     error
	diffLoading bool
	fileLines   []int

	vp   viewport.Model
	spin spinner.Model
	md   markdown
	mds  map[string]*markdown
	w, h int
}

type prLoadedMsg struct {
	owner *PullRequest
	reqID int
	pr    *github.PR
	err   error
}

type prDiffMsg struct {
	owner *PullRequest
	diff  string
	err   error
}

// NewPullRequest opens the PR at url. title and status come from Linear
// and are shown until GitHub responds (or if it can't be reached).
func NewPullRequest(ctx *common.Ctx, url, title, status string) *PullRequest {
	ref, _ := github.ParseURL(url)
	return &PullRequest{ctx: ctx, url: url, ref: ref, title: title, status: status, loading: true,
		mds: map[string]*markdown{}, vp: viewport.New(),
		spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(ctx.Theme.Cursor))}
}

func (p *PullRequest) client() (*github.Client, error) {
	if p.ref.Host == "" {
		return nil, fmt.Errorf("not a GitHub pull request: %s", p.url)
	}
	return github.For(p.ref.Host)
}

func (p *PullRequest) Init() tea.Cmd { return tea.Batch(p.load(), p.spin.Tick) }

func (p *PullRequest) load() tea.Cmd {
	p.reqID++
	req := p.reqID
	return func() tea.Msg {
		c, err := p.client()
		if err != nil {
			return prLoadedMsg{owner: p, reqID: req, err: err}
		}
		pr, err := c.PullRequest(context.Background(), p.ref)
		return prLoadedMsg{owner: p, reqID: req, pr: pr, err: err}
	}
}

func (p *PullRequest) loadDiff() tea.Cmd {
	p.diffLoading = true
	return func() tea.Msg {
		c, err := p.client()
		if err != nil {
			return prDiffMsg{owner: p, err: err}
		}
		d, err := c.Diff(context.Background(), p.ref)
		return prDiffMsg{owner: p, diff: d, err: err}
	}
}

func (p *PullRequest) Title() string {
	if p.ref.Repo != "" {
		return p.ref.String()
	}
	return "Pull request"
}

func (p *PullRequest) Capturing() bool { return false }

func (p *PullRequest) paneW() int {
	if p.w >= 90 {
		return propsW
	}
	return 0
}

func (p *PullRequest) SetSize(w, h int) {
	p.w, p.h = w, h
	p.vp.SetWidth(w - p.paneW())
	p.vp.SetHeight(max(1, h-2))
	p.render()
}

func (p *PullRequest) Help() []key.Binding {
	k := p.ctx.Keys
	b := []key.Binding{prTabKey}
	if p.tab == 1 {
		b = append(b, nextFile)
	}
	return append(b, prComment, k.Open, k.CopyURL, k.CopyBranch, k.Checkout)
}

func (p *PullRequest) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if msg.ID != p.spin.ID() || (!p.loading && !p.diffLoading) {
			return nil
		}
		var cmd tea.Cmd
		p.spin, cmd = p.spin.Update(msg)
		p.render()
		return cmd
	case prLoadedMsg:
		if msg.owner != p || msg.reqID != p.reqID {
			return nil
		}
		p.loading, p.err = false, msg.err
		if msg.pr != nil {
			p.pr, p.title, p.status = msg.pr, msg.pr.Title, msg.pr.Status()
		}
		p.render()
		return nil
	case prDiffMsg:
		if msg.owner != p {
			return nil
		}
		p.diffLoading, p.diff, p.diffErr = false, msg.diff, msg.err
		p.render()
		return nil
	case common.RefreshMsg:
		p.loading = true
		cmds := []tea.Cmd{p.load(), p.spin.Tick}
		if p.tab == 1 {
			cmds = append(cmds, p.loadDiff())
		}
		return tea.Batch(cmds...)
	case tea.KeyPressMsg:
		return p.handleKey(msg)
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		p.vp, cmd = p.vp.Update(msg)
		return cmd
	}
	return nil
}

func (p *PullRequest) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := p.ctx.Keys
	branch := ""
	if p.pr != nil {
		branch = p.pr.HeadRefName
	}
	switch {
	case key.Matches(msg, prTabKey):
		p.tab = 1 - p.tab
		p.vp.GotoTop()
		p.render()
		if p.tab == 1 && p.diff == "" && p.diffErr == nil && !p.diffLoading {
			return tea.Batch(p.loadDiff(), p.spin.Tick)
		}
		return nil
	case key.Matches(msg, nextFile, prevFile) && p.tab == 1:
		y := p.vp.YOffset()
		if key.Matches(msg, nextFile) {
			for _, l := range p.fileLines {
				if l > y {
					p.vp.SetYOffset(l)
					break
				}
			}
		} else {
			for i := len(p.fileLines) - 1; i >= 0; i-- {
				if p.fileLines[i] < y {
					p.vp.SetYOffset(p.fileLines[i])
					break
				}
			}
		}
		return nil
	case key.Matches(msg, prComment):
		return modal(components.NewComposer(p.ctx, "Comment on "+p.Title(), "", func(body string) tea.Cmd {
			return func() tea.Msg {
				c, err := p.client()
				if err == nil {
					err = c.Comment(context.Background(), p.ref, body)
				}
				if err != nil {
					return common.ErrMsg{Err: err}
				}
				return tea.BatchMsg{common.Toast("Commented on " + p.Title()), p.load()}
			}
		}))
	case key.Matches(msg, k.Open):
		return common.OpenURL(p.url)
	case key.Matches(msg, k.CopyURL):
		return common.Copy(p.url, "URL")
	case key.Matches(msg, k.CopyBranch) && branch != "":
		return common.Copy(branch, "branch "+branch)
	case key.Matches(msg, k.Checkout):
		if _, err := exec.LookPath("gh"); err != nil {
			return common.Err(github.ErrNoToken)
		}
		cmd := exec.Command("gh", "pr", "checkout", fmt.Sprint(p.ref.Number), "--repo", p.ref.Host+"/"+p.ref.Owner+"/"+p.ref.Repo)
		cmd.Dir = p.ctx.Repo.Dir
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				return common.ErrMsg{Err: fmt.Errorf("gh pr checkout: %w", err)}
			}
			return common.ToastMsg{Text: "Checked out " + p.Title()}
		})
	}
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return cmd
}

func (p *PullRequest) render() {
	if p.w == 0 {
		return
	}
	if p.tab == 1 {
		p.vp.SetContent(p.renderDiff(p.w - p.paneW() - 2))
		return
	}
	p.vp.SetContent(p.renderOverview(min(p.w-p.paneW()-4, 100)))
}

// since formats " · 3d ago", or nothing for a zero time.
func since(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return " · " + common.Ago(at) + " ago"
}

func (p *PullRequest) renderOverview(w int) string {
	t := p.ctx.Theme
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }
	add("", t.Title.Width(w).Render(p.title), "")
	switch {
	case p.pr == nil && p.loading:
		add(p.spin.View() + t.Dim.Render(" Loading from GitHub…"))
		return strings.Join(lines, "\n")
	case p.pr == nil:
		if p.err != nil {
			add(t.Error.Render(p.err.Error()))
		}
		add("", t.Faint.Render("Press o to open the pull request in your browser."))
		return strings.Join(lines, "\n")
	}
	pr := p.pr
	if strings.TrimSpace(pr.Body) != "" {
		add(p.md.render(t, pr.Body, w))
	} else {
		add(t.Faint.Render("No description provided."))
	}
	add("", t.Fgc(t.Border, strings.Repeat("─", w)), t.Bold.Render("Activity"), "")

	event := func(icon, text string, at string) {
		add(" " + icon + "  " + t.Dim.Render(text) + t.Faint.Render(at))
	}
	card := func(a *github.Actor, when, path, body string) {
		md := p.mds[body]
		if md == nil {
			md = &markdown{}
			p.mds[body] = md
		}
		head := t.Avatar(a.Name(), a.AvatarURL) + " " + t.Bold.Render(a.Name()) + " " + t.Faint.Render(when+" ago")
		if path != "" {
			head += "  " + t.Dim.Render(path)
		}
		content := head + "\n\n" + md.render(t, body, w-6)
		add(t.Fgc(t.CardBg, strings.Repeat("▄", w)))
		add(strings.Split(theme.Surface(lipgloss.NewStyle().PaddingLeft(2).Render(content), w, t.CardBg), "\n")...)
		add(t.Fgc(t.CardBg, strings.Repeat("▀", w)))
	}

	items := pr.TimelineItems.Nodes
	i := 0
	commits := 0
	for i < len(items) && items[i].Typename == "PullRequestCommit" {
		commits++
		i++
	}
	opened := "Opened by " + pr.Author.Name()
	if commits > 0 {
		opened += fmt.Sprintf(" with %d commit%s", commits, plural(commits))
	}
	event(PRIcon(t, "open"), opened, since(pr.CreatedAt))
	for i < len(items) {
		e := items[i]
		j := i + 1
		for j < len(items) && items[j].Typename == e.Typename && (e.Typename == "PullRequestCommit" || e.Typename == "HeadRefForcePushedEvent") &&
			commitAuthor(items[j]) == commitAuthor(e) {
			j++
		}
		n := j - i
		last := items[j-1]
		switch e.Typename {
		case "PullRequestCommit":
			event(t.Dim.Render("◦"), fmt.Sprintf("%s pushed %d commit%s", commitAuthor(e), n, plural(n)),
				since(last.Commit.AuthoredDate))
		case "HeadRefForcePushedEvent":
			text := e.Actor.Name() + fmt.Sprintf(" force-pushed %d times", n)
			if n == 1 && e.BeforeCommit != nil && e.AfterCommit != nil {
				text = e.Actor.Name() + " force-pushed from " + e.BeforeCommit.AbbreviatedOid + " to " + e.AfterCommit.AbbreviatedOid
			}
			event(t.Dim.Render("⇡"), text, since(last.CreatedAt))
		case "ReviewRequestedEvent":
			who := "someone"
			if r := e.RequestedReviewer; r != nil {
				who = r.Login + r.Name
			}
			event(t.Dim.Render("◎"), e.Actor.Name()+" requested review from "+who, since(e.CreatedAt))
		case "PullRequestReview":
			switch e.State {
			case "APPROVED":
				event(t.Fgc(diffAdd, "✓"), "Approved by "+e.Author.Name(), since(e.CreatedAt))
			case "CHANGES_REQUESTED":
				event(t.Fgc(diffDel, "✕"), "Changes requested by "+e.Author.Name(), since(e.CreatedAt))
			}
			if strings.TrimSpace(e.Body) != "" {
				card(e.Author, common.Ago(e.CreatedAt), "", e.Body)
			}
			for _, c := range e.Comments.Nodes {
				card(c.Author, common.Ago(c.CreatedAt), c.Path, c.Body)
			}
		case "IssueComment":
			card(e.Author, common.Ago(e.CreatedAt), "", e.Body)
		case "MergedEvent":
			event(PRIcon(t, "merged"), "Merged by "+e.Actor.Name(), since(e.CreatedAt))
		case "ClosedEvent":
			if pr.State != "MERGED" {
				event(PRIcon(t, "closed"), "Closed by "+e.Actor.Name(), since(e.CreatedAt))
			}
		case "ReopenedEvent":
			event(PRIcon(t, "open"), "Reopened by "+e.Actor.Name(), since(e.CreatedAt))
		case "ReadyForReviewEvent":
			event(t.Dim.Render("◎"), e.Actor.Name()+" marked this ready for review", since(e.CreatedAt))
		}
		i = j
	}
	add("", t.Faint.Render(" m to comment · t for the diff"))
	return strings.Join(lines, "\n")
}

func commitAuthor(e github.Event) string {
	if e.Commit != nil {
		if e.Commit.Author.User != nil {
			return e.Commit.Author.User.Login
		}
		return e.Commit.Author.Name
	}
	return e.Actor.Name()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// renderDiff colors a unified diff and records where each file starts.
func (p *PullRequest) renderDiff(w int) string {
	t := p.ctx.Theme
	p.fileLines = p.fileLines[:0]
	switch {
	case p.diffLoading:
		return "\n" + p.spin.View() + t.Dim.Render(" Loading diff…")
	case p.diffErr != nil:
		return "\n" + t.Error.Render(p.diffErr.Error())
	case p.diff == "":
		return "\n" + t.Faint.Render("No changes.")
	}
	var out []string
	const maxLines = 8000
	for _, l := range strings.Split(strings.TrimRight(p.diff, "\n"), "\n") {
		if len(out) >= maxLines {
			out = append(out, "", t.Faint.Render("Diff truncated. Press o to see the rest on GitHub."))
			break
		}
		l = strings.ReplaceAll(l, "\t", "    ")
		switch {
		case strings.HasPrefix(l, "diff --git "):
			path := l[strings.LastIndex(l, " b/")+3:]
			if len(out) > 0 {
				out = append(out, "")
			}
			p.fileLines = append(p.fileLines, len(out))
			out = append(out, theme.Surface(" "+t.Bold.Render(path), w, t.CardBg))
		case strings.HasPrefix(l, "index "), strings.HasPrefix(l, "--- "), strings.HasPrefix(l, "+++ "),
			strings.HasPrefix(l, "new file mode"), strings.HasPrefix(l, "deleted file mode"), strings.HasPrefix(l, "similarity index"):
		case strings.HasPrefix(l, "@@"):
			out = append(out, t.Faint.Render(common.Fit(l, w)))
		case strings.HasPrefix(l, "+"):
			out = append(out, t.Fgc(diffAdd, common.Fit(l, w)))
		case strings.HasPrefix(l, "-"):
			out = append(out, t.Fgc(diffDel, common.Fit(l, w)))
		default:
			out = append(out, t.App.Render(common.Fit(l, w)))
		}
	}
	return strings.Join(out, "\n")
}

func (p *PullRequest) properties() string {
	t := p.ctx.Theme
	w := propsW - 3
	section := func(name string) []string { return []string{"", t.Faint.Render("  " + name), ""} }
	lines := []string{"", "  " + t.Dim.Render(p.Title())}
	statusName := map[string]string{"open": "Open", "draft": "Draft", "merged": "Merged", "closed": "Closed", "approved": "Approved", "inReview": "In review"}[p.status]
	lines = append(lines, section("Status")...)
	lines = append(lines, "  "+PRIcon(t, p.status)+" "+t.App.Render(statusName))
	if p.pr == nil {
		return common.FitLines(strings.Join(lines, "\n"), p.h)
	}
	pr := p.pr
	lines = append(lines, section("Branch")...)
	lines = append(lines, "  "+common.Fit(t.App.Render(pr.HeadRefName)+t.Faint.Render(" → ")+t.Dim.Render(pr.BaseRefName), w))

	lines = append(lines, section("Reviewers")...)
	seen := map[string]bool{}
	for _, r := range pr.LatestReviews.Nodes {
		mark := t.Dim.Render("◦")
		switch r.State {
		case "APPROVED":
			mark = t.Fgc(diffAdd, "✓")
		case "CHANGES_REQUESTED":
			mark = t.Fgc(diffDel, "✕")
		}
		seen[r.Author.Name()] = true
		lines = append(lines, "  "+common.Fit(t.Avatar(r.Author.Name(), r.Author.AvatarURL)+" "+t.App.Render(r.Author.Name()), w-2)+mark)
	}
	for _, r := range pr.ReviewRequests.Nodes {
		name := r.RequestedReviewer.Login + r.RequestedReviewer.Name
		if seen[name] {
			continue
		}
		lines = append(lines, "  "+common.Fit(t.Avatar(name, r.RequestedReviewer.AvatarURL)+" "+t.App.Render(name), w-2)+t.Faint.Render("◌"))
	}
	if len(pr.LatestReviews.Nodes)+len(pr.ReviewRequests.Nodes) == 0 {
		lines = append(lines, "  "+t.Faint.Render("No reviewers"))
	}

	lines = append(lines, section("Checks")...)
	state, total, failed := pr.Checks()
	switch {
	case total == 0:
		lines = append(lines, "  "+t.Faint.Render("No checks"))
	case failed > 0:
		lines = append(lines, "  "+t.Fgc(diffDel, "✕")+" "+t.App.Render(fmt.Sprintf("%d of %d failing", failed, total)))
	case state == "SUCCESS":
		lines = append(lines, "  "+t.Fgc(diffAdd, "✓")+" "+t.App.Render("All passed"))
	default:
		lines = append(lines, "  "+t.Fgc(lipgloss.Color("#F2C94C"), "◌")+" "+t.App.Render(fmt.Sprintf("%d running", total)))
	}

	stat := t.Fgc(diffAdd, fmt.Sprintf("+%d", pr.Additions)) + " " + t.Fgc(diffDel, fmt.Sprintf("−%d", pr.Deletions))
	lines = append(lines, "", "  "+common.Fit(t.Faint.Render(fmt.Sprintf("%d files changed", pr.ChangedFiles)), w-lipgloss.Width(stat))+stat, "")
	for _, f := range pr.Files.Nodes {
		name, dir := f.Path, ""
		if i := strings.LastIndex(f.Path, "/"); i >= 0 {
			name, dir = f.Path[i+1:], f.Path[:i]
		}
		fs := t.Fgc(diffAdd, fmt.Sprintf("+%d", f.Additions)) + t.Fgc(diffDel, fmt.Sprintf(" −%d", f.Deletions))
		lines = append(lines, "  "+common.Fit(t.App.Render(name)+" "+t.Faint.Render(dir), w-lipgloss.Width(fs)-1)+" "+fs)
	}
	for i, l := range lines {
		lines[i] = common.Fit(l, propsW-1)
	}
	return common.FitLines(strings.Join(lines, "\n"), p.h)
}

func (p *PullRequest) header() string {
	t := p.ctx.Theme
	tab := func(name string, on bool) string {
		if on {
			return theme.Pill(t.Bold.Render(name), t.ChipFocusBg)
		}
		return theme.Pill(t.Dim.Render(name), t.ChipBg)
	}
	left := tab("Overview", p.tab == 0) + " " + tab("Diff", p.tab == 1)
	right := ""
	if p.pr != nil {
		right = t.Fgc(diffAdd, fmt.Sprintf("+%d", p.pr.Additions)) + " " + t.Fgc(diffDel, fmt.Sprintf("−%d", p.pr.Deletions)) + " "
	}
	if p.loading && p.pr != nil {
		right = p.spin.View() + " " + right
	}
	return common.Fit(left, p.w-p.paneW()-lipgloss.Width(right)) + right
}

func (p *PullRequest) View() string {
	main := p.header() + "\n\n" + common.FitLines(p.vp.View(), max(1, p.h-2))
	if p.paneW() == 0 {
		return main
	}
	t := p.ctx.Theme
	sep := strings.TrimSuffix(strings.Repeat(t.Fgc(t.Border, "│")+"\n", p.h), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, common.FitLines(main, p.h), sep, p.properties())
}
