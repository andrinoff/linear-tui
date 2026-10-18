package tui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andrinoff/linear-tui/internal/github"
)

const previewWait = 400 * time.Millisecond

func TestBootShowsMyIssues(t *testing.T) {
	h := newHarness(t)
	h.contains("Acme", "Engineering", "My issues", "Assigned", "Created", "Subscribed",
		"Todo 1", "In Progress 1", "ENG-1", "Fix login crash", "ENG-2", "Add dark mode", "Created Jan 1", "Bug")
	c := h.fake.called("Issues")
	if len(c) == 0 {
		t.Fatal("Issues was not requested")
	}
	f, _ := json.Marshal(c[0].vars["filter"])
	if !strings.Contains(string(f), `"isMe"`) || !strings.Contains(string(f), `"-P1W"`) {
		t.Fatalf("expected assigned-to-me with recent done, got %s", f)
	}
}

func TestOpenDetail(t *testing.T) {
	h := newHarness(t)
	h.keys("enter")
	h.contains("My issues  ›  ENG-1", "Fix login crash", "cannot", "log in after the update", "Properties", "Status", "Priority",
		"Activity", "Reproduced on staging.", "Created by bob")
	h.keys("esc")
	h.contains("Add dark mode")
	h.lacks("Reproduced on staging.")
}

func TestOptimisticPriorityChange(t *testing.T) {
	h := newHarness(t)
	h.keys("p")
	h.contains("Set priority", "Urgent")
	h.keys("down", "enter")
	h.lacks("Set priority")
	calls := h.fake.called("IssueUpdate")
	if len(calls) != 1 {
		t.Fatalf("IssueUpdate calls = %d", len(calls))
	}
	in := calls[0].vars["input"].(map[string]any)
	if in["priority"] != float64(1) || calls[0].vars["id"] != "i1" {
		t.Fatalf("update vars = %v", calls[0].vars)
	}
	h.contains(" ! ")
}

func TestOptimisticRevertOnError(t *testing.T) {
	h := newHarness(t)
	h.fake.override("IssueUpdate", `{"errors":[{"message":"nope","extensions":{"userPresentableMessage":"Issue is locked"}}]}`)
	h.keys("p", "down", "enter")
	h.contains("Issue is locked")
	h.lacks(" ! ")
}

func TestAssignAndClearSelection(t *testing.T) {
	h := newHarness(t)
	h.keys("ctrl+b", "x", "x")
	h.contains("2 selected")
	h.keys("a")
	h.contains("Assign to", "Unassigned", "bob")
	h.keys("enter")
	if n := len(h.fake.called("IssueUpdate")); n != 2 {
		t.Fatalf("bulk updates = %d", n)
	}
	h.lacks("selected")
}

func TestNavigateBoardAndCycles(t *testing.T) {
	h := newHarness(t)
	h.send(tea.WindowSizeMsg{Width: 160, Height: 40})
	h.keys("g", "b")
	h.contains("Todo 1", "In Progress 1", "Done 0 Past week", "Fix login crash", "hidden by display options")
	h.keys(".")
	h.contains("Done 0", "Showing all issues")
	h.lacks("Past week")
	h.keys("g", "c")
	h.contains("Cycle 7", "current", "50%")
	h.keys("g", "p")
	h.contains("Mobile app", "on track")
}

func TestInboxMarksRead(t *testing.T) {
	h := newHarness(t)
	h.keys("g", "i")
	h.contains("1 unread", "bob assigned you", "ENG-1 Fix login crash")
	h.keys("u")
	calls := h.fake.called("NotificationUpdate")
	if len(calls) != 1 {
		t.Fatalf("NotificationUpdate calls = %d", len(calls))
	}
	if in := calls[0].vars["input"].(map[string]any); in["readAt"] == nil {
		t.Fatalf("expected readAt to be set: %v", in)
	}
	h.contains("0 unread")
}

func TestPaletteNavigates(t *testing.T) {
	h := newHarness(t)
	h.keys("ctrl+k")
	h.contains("Command palette", "Go to Team board")
	h.typeText("projects")
	h.keys("enter")
	h.lacks("Command palette")
	h.contains("Mobile app")
}

func TestHelpOverlay(t *testing.T) {
	h := newHarness(t)
	h.keys("?")
	h.contains("Keyboard shortcuts", "commands", "git checkout")
	h.keys("q")
	h.lacks("Keyboard shortcuts")
}

func TestAuthErrorOnBootShowsLogin(t *testing.T) {
	h := newHarness(t)
	h.fake.override("GetViewer", `{"errors":[{"message":"bad","extensions":{"type":"authentication error"}}]}`)
	h.app.ctx.Svc.Invalidate()
	h.exec(h.app.boot())
	h.settle()
	h.contains("How would you like to log in?", "no longer valid")
}

func TestTooSmall(t *testing.T) {
	h := newHarness(t)
	h.send(tea.WindowSizeMsg{Width: 40, Height: 10})
	h.contains("Terminal too small")
}

func TestQuitFromRoot(t *testing.T) {
	h := newHarness(t)
	h.keys("q")
	if !h.quit {
		t.Fatal("q on the root view should quit")
	}
}

func TestNarrowListHidesColumns(t *testing.T) {
	h := newHarness(t)
	h.keys("ctrl+b")
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.contains("Fix login crash", "Bug", "Jan 1")
	h.send(tea.WindowSizeMsg{Width: 94, Height: 30})
	if s := h.screen(); strings.Contains(s, "Bug") {
		t.Fatalf("labels column should hide when narrow:\n%s", s)
	}
}

func TestCreateIssueFlow(t *testing.T) {
	h := newHarness(t)
	h.fake.override("IssueCreate", `{"data":{"issueCreate":{"success":true,"issue":`+issueJSON("i9", "ENG-9", "New bug", stTodo, 0)+`}}}`)
	h.keys("c")
	h.contains("New issue", "Team", "Engineering (ENG)")
	h.keys("enter")
	h.typeText("New bug")
	h.keys("enter", "tab")
	h.contains("Properties", "Status", "Priority")
	for range 9 {
		h.keys("enter")
	}
	calls := h.fake.called("IssueCreate")
	if len(calls) != 1 {
		t.Fatalf("IssueCreate calls = %d", len(calls))
	}
	in := calls[0].vars["input"].(map[string]any)
	if in["teamId"] != "team-eng" || in["title"] != "New bug" || in["stateId"] != "st-todo" || in["assigneeId"] != "u-me" || len(in) != 4 {
		t.Fatalf("create input = %v", in)
	}
	h.contains("Created ENG-9")
	h.lacks("Properties")
	h.keys("ctrl+o")
	got := h.fake.called("GetIssue")
	if len(got) == 0 || got[len(got)-1].vars["id"] != "i9" {
		t.Fatalf("ctrl+o should open the created issue, calls = %v", got)
	}
}

func TestPasteIntoAPIKeyField(t *testing.T) {
	h := newHarness(t)
	h.fake.override("GetViewer", `{"errors":[{"message":"bad","extensions":{"type":"authentication error"}}]}`)
	h.app.ctx.Svc.Invalidate()
	h.exec(h.app.boot())
	h.settle()
	h.keys("enter")
	h.send(tea.PasteMsg{Content: "lin_api_abc"})
	h.contains("API key › ***********")
}

func TestPasteIntoListFilter(t *testing.T) {
	h := newHarness(t)
	h.keys("ctrl+b", "/")
	h.send(tea.PasteMsg{Content: "dark"})
	h.contains("/dark", "Add dark mode")
	h.lacks("Fix login crash")
}

func TestInboxSplitPreviewMarksRead(t *testing.T) {
	h := newHarness(t)
	h.send(tea.WindowSizeMsg{Width: 170, Height: 40})
	h.keys("g", "i")
	time.Sleep(previewWait)
	h.settle()
	h.contains("bob assigned you", "Activity", "Reproduced on staging.", "0 unread")
	if n := len(h.fake.called("NotificationUpdate")); n != 1 {
		t.Fatalf("previewing should mark read once, got %d updates", n)
	}
	h.keys("l", "m")
	h.contains("New comment")
}

func TestInboxPullRequestPreview(t *testing.T) {
	fixture, err := os.ReadFile("../github/testdata/pr.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			w.Write(fixture)
			return
		}
		io.WriteString(w, "diff --git a/tea.go b/tea.go\n--- a/tea.go\n+++ b/tea.go\n@@ -1 +1 @@\n-old line\n+new line\n")
	}))
	defer srv.Close()
	github.Register("github.com", &github.Client{HTTP: srv.Client(), Token: "t", API: srv.URL, GraphQL: srv.URL + "/graphql"})

	h := newHarness(t)
	h.send(tea.WindowSizeMsg{Width: 190, Height: 44})
	h.keys("g", "i")
	h.contains("bob requested your review", "(v2) feat: ferocious renderer")
	h.keys("down")
	time.Sleep(previewWait)
	h.settle()
	h.contains("Overview", "Diff", "Opened by", "Merged by", "Merged", "files changed")
	h.keys("l", "t")
	h.contains("tea.go", "+new line", "-old line")
}
