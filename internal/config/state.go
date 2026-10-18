package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

const maxRecent = 20

type RecentIssue struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// State is persisted between runs.
type State struct {
	LastTeam string        `json:"last_team"`
	LastView string        `json:"last_view"`
	Recent   []RecentIssue `json:"recent"`

	path string
}

func LoadState() *State {
	s := &State{}
	dir, err := StateDir()
	if err != nil {
		return s
	}
	s.path = filepath.Join(dir, "state.json")
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, s)
	}
	return s
}

// AddRecent moves the issue to the front of the recent list.
func (s *State) AddRecent(r RecentIssue) {
	s.Recent = slices.DeleteFunc(s.Recent, func(x RecentIssue) bool { return x.ID == r.ID })
	s.Recent = append([]RecentIssue{r}, s.Recent...)
	if len(s.Recent) > maxRecent {
		s.Recent = s.Recent[:maxRecent]
	}
}

func (s *State) Save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}
