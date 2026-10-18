package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mono.toml"), []byte("monochrome = true\naccent = \"#FFFFFF\"\n"), 0o600)
	th, err := Resolve("mono", dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if !th.Dark || !th.Mono || th.Accent == nil || th.CardBg == nil {
		t.Fatalf("theme = %+v", th)
	}
	if th.AppBg != nil || th.PanelBg != nil {
		t.Fatal("backgrounds should default to transparent")
	}
	if th.Hex("#ff0000") != th.Muted {
		t.Fatal("monochrome themes should ignore decorative colors")
	}
	if !strings.Contains(th.StateIcon("started", "#F2C94C"), "242;201;76") {
		t.Fatal("status icons keep their color in monochrome themes")
	}
	if _, err := Resolve("missing", dir, true); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing theme err = %v", err)
	}
}

func TestPaintTransparent(t *testing.T) {
	if got := Surface("ab", 4, nil); got != "ab  " {
		t.Fatalf("Surface with nil bg = %q", got)
	}
}
