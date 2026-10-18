package common

import (
	"errors"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andrinoff/linear-tui/internal/linear"
)

type (
	// PushMsg opens a view on top of the stack.
	PushMsg struct{ View View }
	// PopMsg closes the top view.
	PopMsg struct{}
	// NavigateMsg resets the stack to a top-level destination.
	NavigateMsg struct{ Target string }
	// ModalMsg opens a modal overlay.
	ModalMsg struct{ Modal View }
	// CloseModalMsg closes the active modal.
	CloseModalMsg struct{}
	// ToastMsg shows a transient notification.
	ToastMsg struct {
		Text string
		Err  bool
	}
	// ErrMsg reports a failed operation.
	ErrMsg struct{ Err error }
	// RefreshMsg asks views to reload their data.
	RefreshMsg struct{ Silent bool }
	// OpenIssueMsg opens an issue's detail view by id or identifier.
	OpenIssueMsg struct{ ID string }
	// CreateIssueMsg opens the issue form prefilled with Defaults.
	CreateIssueMsg struct{ Defaults IssueDefaults }
	// EditIssueMsg opens the issue form for an existing issue.
	EditIssueMsg struct {
		Issue       linear.Issue
		Description string
	}
	// IssueUpdatedMsg is broadcast to every view after an issue changes.
	IssueUpdatedMsg struct{ Issue linear.Issue }
	// IssueRemovedMsg is broadcast after an issue is archived or deleted.
	IssueRemovedMsg struct{ ID string }
	// IssueCreatedMsg is broadcast after a new issue is created.
	IssueCreatedMsg struct{ Issue linear.Issue }
	// CommentAddedMsg is broadcast after a comment is created or edited.
	CommentAddedMsg struct {
		IssueID string
		Comment linear.Comment
	}
	// UnreadCountMsg updates the inbox badge.
	UnreadCountMsg struct{ N int }
	// MutationMsg carries the result of an optimistic issue update.
	MutationMsg struct {
		Seq    int
		Orig   linear.Issue
		Result *linear.Issue
		Err    error
	}
	// ReloadIssueMsg asks views showing the issue to refetch it.
	ReloadIssueMsg struct{ ID string }
	// ClickMsg is a mouse click translated to view-local coordinates.
	ClickMsg struct{ X, Y int }
	// TeamChangedMsg is sent when the focused team changes.
	TeamChangedMsg struct{ Team *linear.Team }
	// LoggedInMsg is sent once credentials are verified.
	LoggedInMsg struct{}
	// LogoutMsg clears credentials and returns to the login view.
	LogoutMsg struct{}
	// ToggleThemeMsg switches between light and dark styles.
	ToggleThemeMsg struct{}
	// ShowHelpMsg toggles the full help overlay.
	ShowHelpMsg struct{}
)

// IssueDefaults prefill the create form from the current context.
type IssueDefaults struct {
	TeamID    string
	StateID   string
	CycleID   string
	ProjectID string
	ParentID  string
	Assignee  string
}

func Cmd(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func Toast(text string) tea.Cmd { return Cmd(ToastMsg{Text: text}) }

func Err(err error) tea.Cmd { return Cmd(ErrMsg{Err: err}) }

// ErrText is a user-facing message for err.
func ErrText(err error) string { return linear.Message(err) }

func IsAuthErr(err error) bool { return errors.Is(err, linear.ErrAuth) }

// Ago formats a timestamp relative to now.
func Ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	case d < 365*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24/30)) + "mo"
	}
	return strconv.Itoa(int(d.Hours()/24/365)) + "y"
}
