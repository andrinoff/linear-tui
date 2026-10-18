package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "missing.toml"))
	if err != nil || !cfg.Git.OpenFromBranch || cfg.Refresh() != time.Minute {
		t.Fatalf("defaults = %+v, %v", cfg, err)
	}
	p := filepath.Join(dir, "config.toml")
	os.WriteFile(p, []byte("refresh_interval = \"0s\"\n[keys]\ncreate = [\"n\"]\n[git]\nopen_from_branch = false\n"), 0o600)
	cfg, err = Load(p)
	if err != nil || cfg.Refresh() != 0 || cfg.Git.OpenFromBranch || cfg.Keys["create"][0] != "n" {
		t.Fatalf("overrides = %+v, %v", cfg, err)
	}
}

func TestRecent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := LoadState()
	for i := range 25 {
		s.AddRecent(RecentIssue{ID: string(rune('a' + i))})
	}
	s.AddRecent(RecentIssue{ID: "c"})
	if len(s.Recent) != maxRecent || s.Recent[0].ID != "c" {
		t.Fatalf("recent = %+v", s.Recent)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if got := LoadState(); len(got.Recent) != maxRecent {
		t.Fatalf("reloaded = %d", len(got.Recent))
	}
}
