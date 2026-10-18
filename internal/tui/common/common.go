// Package common holds the types shared by the TUI shell, views, and
// components: the app context, messages, the View interface, and keys.
package common

import (
	"context"
	"sync"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andrinoff/linear-tui/internal/config"
	"github.com/andrinoff/linear-tui/internal/git"
	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

// Ctx is shared by every view.
type Ctx struct {
	Svc    *linear.Service
	Cfg    config.Config
	State  *config.State
	Theme  *theme.Theme
	Keys   *Keys
	Viewer *linear.Viewer
	Repo   git.Repo
	// Teams are the workspace teams, for the sidebar.
	Teams []linear.Team
	// Team is the team currently in focus, used as a default for new issues
	// and team-scoped views.
	Team *linear.Team

	mu  sync.Mutex
	seq map[string]int
}

func (c *Ctx) Context() context.Context { return context.Background() }

// NextSeq bumps and returns the mutation sequence for an issue, used to drop
// stale optimistic-update results.
func (c *Ctx) NextSeq(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seq == nil {
		c.seq = map[string]int{}
	}
	c.seq[id]++
	return c.seq[id]
}

func (c *Ctx) CurrentSeq(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seq[id]
}

func (c *Ctx) Me() *linear.User {
	if c.Viewer == nil {
		return nil
	}
	return &c.Viewer.Viewer
}

// View is a screen in the main area, or a modal overlay.
type View interface {
	Init() tea.Cmd
	Update(tea.Msg) tea.Cmd
	View() string
	SetSize(w, h int)
	Title() string
	Help() []key.Binding
	// Capturing reports whether the view is consuming text input, which
	// disables global single-key shortcuts.
	Capturing() bool
}
