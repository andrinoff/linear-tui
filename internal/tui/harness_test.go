package tui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andrinoff/linear-tui/internal/config"
	"github.com/andrinoff/linear-tui/internal/linear"
)

const (
	stTodo     = `{"id":"st-todo","name":"Todo","type":"unstarted","color":"#e2e2e2","position":1}`
	stProgress = `{"id":"st-prog","name":"In Progress","type":"started","color":"#f2c94c","position":2}`
	stDone     = `{"id":"st-done","name":"Done","type":"completed","color":"#5e6ad2","position":3}`
	userMe     = `{"id":"u-me","name":"Ada Lovelace","displayName":"ada","email":"ada@example.com","isMe":true,"active":true}`
	userBob    = `{"id":"u-bob","name":"Bob","displayName":"bob","email":"bob@example.com","isMe":false,"active":true}`
	teamRef    = `{"id":"team-eng","key":"ENG","name":"Engineering"}`
)

func issueJSON(id, ident, title, state string, priority int) string {
	return `{"id":"` + id + `","identifier":"` + ident + `","title":"` + title + `","priority":` + itoa(priority) +
		`,"priorityLabel":"","estimate":null,"dueDate":null,"url":"https://linear.app/x/issue/` + ident + `","branchName":"ada/` +
		strings.ToLower(ident) + `","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z","sortOrder":1,"state":` + state +
		`,"assignee":` + userMe + `,"team":` + teamRef + `,"labels":{"nodes":[{"id":"lb-bug","name":"Bug","color":"#eb5757","team":null}]},"cycle":null,"project":null,"projectMilestone":null}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

var fixtures = map[string]string{
	"GetViewer":   `{"viewer":` + userMe + `,"organization":{"id":"org","name":"Acme","urlKey":"acme"}}`,
	"Teams":       `{"teams":{"nodes":[{"id":"team-eng","key":"ENG","name":"Engineering","cyclesEnabled":true,"issueEstimationType":"fibonacci","issueEstimationAllowZero":false,"issueEstimationExtended":false,"states":{"nodes":[` + stDone + `,` + stTodo + `,` + stProgress + `]},"members":{"nodes":[` + userMe + `,` + userBob + `]},"activeCycle":null}]}}`,
	"Labels":      `{"issueLabels":{"nodes":[{"id":"lb-bug","name":"Bug","color":"#eb5757","team":null}]}}`,
	"UnreadCount": `{"notificationsUnreadCount":1}`,
	"Issues": `{"issues":{"nodes":[` + issueJSON("i1", "ENG-1", "Fix login crash", stTodo, 3) + `,` +
		issueJSON("i2", "ENG-2", "Add dark mode", stProgress, 2) + `],"pageInfo":{"hasNextPage":false,"endCursor":"c"}}}`,
	"GetIssue": `{"issue":` + strings.TrimSuffix(issueJSON("i1", "ENG-1", "Fix login crash", stTodo, 3), "}") +
		`,"description":"Users **cannot** log in after the update.","creator":` + userBob + `,"parent":null,"children":{"nodes":[]},` +
		`"relations":{"nodes":[]},"inverseRelations":{"nodes":[]},"attachments":{"nodes":[]},` +
		`"comments":{"nodes":[{"id":"c1","body":"Reproduced on staging.","createdAt":"2026-01-02T00:00:00Z","editedAt":null,"user":` + userBob + `,"parent":null}]}}}`,
	"Notifications": `{"notifications":{"nodes":[{"__typename":"IssueNotification","id":"n1","type":"issueAssignedToYou","title":"Fix login crash","subtitle":"","readAt":null,"snoozedUntilAt":null,"createdAt":"2026-01-02T00:00:00Z","url":"","actor":` + userBob +
		`,"issue":` + issueJSON("i1", "ENG-1", "Fix login crash", stTodo, 3) + `,"comment":null}],"pageInfo":{"hasNextPage":false,"endCursor":"c"}}}`,
	"NotificationUpdate": `{"notificationUpdate":{"success":true}}`,
	"Cycles":             `{"team":{"cycles":{"nodes":[{"id":"cy1","number":7,"name":"","description":"","startsAt":"2026-01-01T00:00:00Z","endsAt":"2099-01-14T00:00:00Z","completedAt":null,"progress":0.5,"isActive":true,"isFuture":false,"isPast":false,"isNext":false,"scopeHistory":[10],"completedScopeHistory":[5],"issueCountHistory":[4],"completedIssueCountHistory":[2]}]}}}`,
	"Projects":           `{"projects":{"nodes":[{"id":"p1","name":"Mobile app","description":"Ship it","color":"#4ea7fc","progress":0.25,"startDate":null,"targetDate":"2026-03-01","health":"onTrack","url":"https://linear.app/p1","status":{"name":"In Progress","type":"started","color":"#f2c94c"},"lead":` + userMe + `,"teams":{"nodes":[` + teamRef + `]}}]}}`,
}

type fakeLinear struct {
	mu        sync.Mutex
	calls     []call
	overrides map[string]string
}

type call struct {
	op   string
	vars map[string]any
}

func (f *fakeLinear) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OperationName string         `json:"operationName"`
		Variables     map[string]any `json:"variables"`
	}
	body, _ := io.ReadAll(r.Body)
	json.Unmarshal(body, &req)
	f.mu.Lock()
	f.calls = append(f.calls, call{req.OperationName, req.Variables})
	resp, ok := f.overrides[req.OperationName]
	f.mu.Unlock()
	if ok {
		io.WriteString(w, resp)
		return
	}
	if req.OperationName == "IssueUpdate" {
		id, _ := req.Variables["id"].(string)
		in, _ := req.Variables["input"].(map[string]any)
		p := 3
		if v, ok := in["priority"].(float64); ok {
			p = int(v)
		}
		io.WriteString(w, `{"data":{"issueUpdate":{"success":true,"issue":`+issueJSON(id, "ENG-1", "Fix login crash", stTodo, p)+`}}}`)
		return
	}
	data, ok := fixtures[req.OperationName]
	if !ok {
		io.WriteString(w, `{"errors":[{"message":"no fixture for `+req.OperationName+`"}]}`)
		return
	}
	io.WriteString(w, `{"data":`+data+`}`)
}

func (f *fakeLinear) called(op string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.op == op {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeLinear) override(op, resp string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.overrides == nil {
		f.overrides = map[string]string{}
	}
	f.overrides[op] = resp
}

// harness drives the App like the Bubble Tea runtime, but synchronously:
// commands run in goroutines and their messages are applied in order until
// the app goes idle.
type harness struct {
	t    *testing.T
	app  *App
	fake *fakeLinear
	msgs chan tea.Msg
	quit bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fake := &fakeLinear{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	cfg := config.Default()
	cfg.Git.OpenFromBranch = false
	cfg.Theme = "dark"
	svc := linear.NewService(linear.NewClient(srv.URL, http.DefaultTransport))
	h := &harness{t: t, app: New(Options{Cfg: cfg, Service: svc}), fake: fake, msgs: make(chan tea.Msg, 256)}
	h.exec(h.app.Init())
	h.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	return h
}

var cmdType = reflect.TypeOf((tea.Cmd)(nil))

func (h *harness) exec(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() { h.msgs <- cmd() }()
}

func (h *harness) apply(msg tea.Msg) {
	switch m := msg.(type) {
	case nil, spinner.TickMsg:
		return
	case tea.QuitMsg:
		h.quit = true
		return
	case tea.BatchMsg:
		for _, c := range m {
			h.exec(c)
		}
		return
	}
	if rv := reflect.ValueOf(msg); rv.Kind() == reflect.Slice && rv.Type().Elem() == cmdType {
		for i := range rv.Len() {
			c := rv.Index(i).Interface().(tea.Cmd)
			if c == nil {
				continue
			}
			done := make(chan tea.Msg, 1)
			go func() { done <- c() }()
			select {
			case m := <-done:
				h.apply(m)
			case <-time.After(time.Second):
			}
		}
		return
	}
	_, cmd := h.app.Update(msg)
	h.exec(cmd)
}

// settle applies messages until none arrive for a short while.
func (h *harness) settle() {
	for {
		select {
		case m := <-h.msgs:
			h.apply(m)
		case <-time.After(250 * time.Millisecond):
			return
		}
	}
}

func (h *harness) send(msg tea.Msg) {
	h.apply(msg)
	h.settle()
}

func (h *harness) keys(keys ...string) {
	for _, k := range keys {
		h.send(press(k))
	}
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.send(tea.KeyPressMsg{Text: string(r), Code: r})
	}
}

func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Text: k, Code: []rune(k)[0]}
}

func (h *harness) screen() string { return ansi.Strip(h.app.render()) }

func (h *harness) contains(want ...string) {
	h.t.Helper()
	s := h.screen()
	for _, w := range want {
		if !strings.Contains(s, w) {
			h.t.Fatalf("screen missing %q:\n%s", w, s)
		}
	}
}

func (h *harness) lacks(bad string) {
	h.t.Helper()
	if s := h.screen(); strings.Contains(s, bad) {
		h.t.Fatalf("screen unexpectedly contains %q:\n%s", bad, s)
	}
}
