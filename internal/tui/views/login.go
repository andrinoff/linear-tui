package views

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pkg/browser"

	"github.com/andrinoff/linear-tui/internal/auth"
	"github.com/andrinoff/linear-tui/internal/tui/common"
)

// Login lets the user authenticate with an API key or OAuth.
type Login struct {
	ctx      *common.Ctx
	store    *auth.Store
	endpoint string
	oauth    *auth.OAuth

	choice  int
	stage   int // 0 choose, 1 api key, 2 waiting for browser
	input   textinput.Model
	spin    spinner.Model
	busy    bool
	err     error
	authURL string
	cancel  context.CancelFunc
	w, h    int
	notice  string
}

type loginResultMsg struct {
	owner *Login
	creds *auth.Credentials
	err   error
}

type oauthURLMsg struct {
	owner *Login
	url   string
}

// NewLogin builds the login screen. notice explains why it is shown (for
// example an expired key).
func NewLogin(ctx *common.Ctx, store *auth.Store, endpoint string, oauth *auth.OAuth, notice string) *Login {
	in := textinput.New()
	in.Prompt = "API key › "
	in.Placeholder = "lin_api_…"
	in.EchoMode = textinput.EchoPassword
	in.SetStyles(textinput.DefaultStyles(ctx.Theme.Dark))
	return &Login{ctx: ctx, store: store, endpoint: endpoint, oauth: oauth, input: in, notice: notice,
		spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(ctx.Theme.Cursor))}
}

func (l *Login) Init() tea.Cmd    { return l.spin.Tick }
func (l *Login) Title() string    { return "Log in" }
func (l *Login) Capturing() bool  { return true }
func (l *Login) SetSize(w, h int) { l.w, l.h = w, h; l.input.SetWidth(min(60, w-14)) }
func (l *Login) Help() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "continue")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		l.ctx.Keys.Quit,
	}
}

func (l *Login) oauthEnabled() bool { return l.oauth != nil && l.oauth.ClientID != "" }

func (l *Login) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		l.spin, cmd = l.spin.Update(msg)
		return cmd
	case oauthURLMsg:
		if msg.owner == l {
			l.authURL = msg.url
		}
		return nil
	case loginResultMsg:
		if msg.owner != l {
			return nil
		}
		l.busy = false
		if msg.err != nil {
			l.err = msg.err
			if l.stage == 2 {
				l.stage = 0
			}
			return nil
		}
		if err := l.store.Save(msg.creds); err != nil {
			l.err = err
			return nil
		}
		return common.Cmd(common.LoggedInMsg{})
	case tea.PasteMsg:
		if l.stage != 1 || l.busy {
			return nil
		}
		var cmd tea.Cmd
		l.input, cmd = l.input.Update(msg)
		return cmd
	case tea.KeyPressMsg:
		if l.busy && msg.String() != "esc" {
			return nil
		}
		switch l.stage {
		case 0:
			switch msg.String() {
			case "up", "k", "down", "j", "tab":
				l.choice = 1 - l.choice
			case "enter":
				l.err = nil
				if l.choice == 0 {
					l.stage = 1
					return l.input.Focus()
				}
				if !l.oauthEnabled() {
					l.err = errNoClientID
					return nil
				}
				return l.startOAuth()
			}
			return nil
		case 1:
			switch msg.String() {
			case "esc":
				l.stage, l.err = 0, nil
				l.input.Blur()
				return nil
			case "enter":
				k := strings.TrimSpace(l.input.Value())
				if k == "" {
					return nil
				}
				l.busy, l.err = true, nil
				return func() tea.Msg {
					_, err := auth.ValidateAPIKey(bg(), l.endpoint, k)
					return loginResultMsg{owner: l, creds: &auth.Credentials{Kind: auth.KindAPIKey, APIKey: k}, err: err}
				}
			}
			var cmd tea.Cmd
			l.input, cmd = l.input.Update(msg)
			return cmd
		case 2:
			if msg.String() == "esc" && l.cancel != nil {
				l.cancel()
			}
		}
	}
	return nil
}

type loginErr string

func (e loginErr) Error() string { return string(e) }

const errNoClientID = loginErr("OAuth needs a client id: set oauth.client_id in config.toml or LINEAR_OAUTH_CLIENT_ID")

func (l *Login) startOAuth() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel, l.stage, l.busy, l.authURL = cancel, 2, true, ""
	urlCh := make(chan string, 1)
	login := func() tea.Msg {
		defer cancel()
		tok, err := l.oauth.Login(ctx, func(u string) {
			urlCh <- u
			_ = browser.OpenURL(u)
		})
		if err != nil {
			return loginResultMsg{owner: l, err: err}
		}
		return loginResultMsg{owner: l, creds: &auth.Credentials{Kind: auth.KindOAuth, Token: tok}}
	}
	waitURL := func() tea.Msg {
		select {
		case u := <-urlCh:
			return oauthURLMsg{owner: l, url: u}
		case <-ctx.Done():
			return nil
		}
	}
	return tea.Batch(login, waitURL)
}

func (l *Login) View() string {
	t := l.ctx.Theme
	var b strings.Builder
	b.WriteString(t.Fgc(t.Accent, "◢◤ ") + t.Title.Render("linear-tui") + "\n\n")
	if l.notice != "" {
		b.WriteString(t.Error.Render(l.notice) + "\n\n")
	}
	switch l.stage {
	case 0:
		b.WriteString("How would you like to log in?\n\n")
		opts := []string{"Personal API key", "OAuth (browser)"}
		for i, o := range opts {
			line := "  " + o
			if i == 1 && !l.oauthEnabled() {
				line += t.Faint.Render("  (needs oauth.client_id)")
			}
			if i == l.choice {
				line = t.Cursor.Render("› ") + t.Bold.Render(o)
				if i == 1 && !l.oauthEnabled() {
					line += t.Faint.Render("  (needs oauth.client_id)")
				}
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n" + t.Dim.Render("Create a key at Linear → Settings → Security & access → Personal API keys.\nTip: set LINEAR_API_KEY to skip this screen."))
	case 1:
		b.WriteString(l.input.View() + "\n\n")
		if l.busy {
			b.WriteString(l.spin.View() + " Verifying key…")
		} else {
			b.WriteString(t.Dim.Render("enter to verify · esc back"))
		}
	case 2:
		b.WriteString(l.spin.View() + " Waiting for authorization in your browser…\n\n")
		if l.authURL != "" {
			b.WriteString(t.Dim.Render("If the browser didn't open, visit:") + "\n" + t.Link.Render(l.authURL) + "\n\n")
		}
		b.WriteString(t.Dim.Render("esc to cancel · times out after 5 minutes"))
	}
	if l.err != nil {
		b.WriteString("\n\n" + t.Error.Render(l.err.Error()))
	}
	box := t.Box("\n"+b.String()+"\n", min(80, l.w-4), t.Border)
	return lipgloss.Place(l.w, l.h, lipgloss.Center, lipgloss.Center, box)
}
