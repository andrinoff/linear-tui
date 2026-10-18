package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestBootShowsMyIssues(t *testing.T) {
	h := newHarness(t)
	h.contains("Acme", "Engineering", "My issues", "ENG-1", "Fix login crash", "ENG-2", "Add dark mode", "status: active", "Todo")
	if c := h.fake.called("Issues"); len(c) == 0 {
		t.Fatal("Issues was not requested")
	} else if f, _ := c[0].vars["filter"].(map[string]any); f == nil {
		t.Fatalf("expected a filter, got %v", c[0].vars)
	}
}

func TestOpenDetail(t *testing.T) {
	h := newHarness(t)
	h.keys("enter")
	h.contains("My issues › ENG-1", "cannot", "log in after the update", "Comments (1)", "Reproduced on staging.", "Creator", "bob")
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
	h.contains("!!!")
}

func TestOptimisticRevertOnError(t *testing.T) {
	h := newHarness(t)
	h.fake.override("IssueUpdate", `{"errors":[{"message":"nope","extensions":{"userPresentableMessage":"Issue is locked"}}]}`)
	h.keys("p", "down", "enter")
	h.contains("Issue is locked")
	h.lacks("!!!")
}

func TestAssignAndClearSelection(t *testing.T) {
	h := newHarness(t)
	h.keys("x", "x")
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
	h.keys("g", "b")
	h.contains("Engineering board", "Todo", "In Progress", "Fix login crash")
	h.lacks("Done 0")
	h.keys(".")
	h.contains("Done")
	h.keys("g", "c")
	h.contains("Cycle 7", "current", "50%")
	h.keys("g", "p")
	h.contains("Mobile app", "on track")
}

func TestInboxMarksRead(t *testing.T) {
	h := newHarness(t)
	h.keys("g", "i")
	h.contains("1 unread", "issue Assigned To You"[:0]+"assigned to you", "ENG-1")
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
	h.contains("Command palette", "Go to Board")
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
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.contains("Labels", "Assignee", "Bug")
	h.send(tea.WindowSizeMsg{Width: 94, Height: 30})
	if s := h.screen(); strings.Contains(s, "Labels") {
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
	if in["teamId"] != "team-eng" || in["title"] != "New bug" || len(in) != 2 {
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
