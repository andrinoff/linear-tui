package theme

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Spec is a theme palette as written in a theme file. Colors are hex
// strings; an empty surface color means transparent (the terminal's own
// background shows through).
type Spec struct {
	Dark       bool `toml:"dark"`
	Monochrome bool `toml:"monochrome"`

	Background string `toml:"background"`
	Panel      string `toml:"panel"`
	Card       string `toml:"card"`
	CardFocus  string `toml:"card_focus"`
	Chip       string `toml:"chip"`
	ChipFocus  string `toml:"chip_focus"`
	Modal      string `toml:"modal"`
	Selection  string `toml:"selection"`

	Accent string `toml:"accent"`
	Text   string `toml:"text"`
	Bright string `toml:"bright"`
	Muted  string `toml:"muted"`
	Subtle string `toml:"subtle"`
	Border string `toml:"border"`

	Red    string `toml:"red"`
	Orange string `toml:"orange"`
	Yellow string `toml:"yellow"`
	Green  string `toml:"green"`
	Blue   string `toml:"blue"`
}

// DefaultSpec is Linear's palette. The app and panel backgrounds are left
// transparent so the terminal background shows through.
func DefaultSpec(dark bool) Spec {
	sp := Spec{
		Dark: dark, Accent: "#5E6AD2",
		Red: "#EB5757", Orange: "#FC7840", Yellow: "#F2C94C", Green: "#4CB782", Blue: "#4EA7FC",
	}
	if dark {
		sp.Card, sp.CardFocus, sp.Chip, sp.ChipFocus = "#1E1F22", "#2A2B30", "#27282C", "#35363C"
		sp.Modal, sp.Selection = "#1C1D20", "#232428"
		sp.Text, sp.Bright, sp.Muted, sp.Subtle, sp.Border = "#D0D6E0", "#F7F8F8", "#8A8F98", "#5C5F66", "#2E2F34"
	} else {
		sp.Card, sp.CardFocus, sp.Chip, sp.ChipFocus = "#F4F5F7", "#E9EAEE", "#E8E9EC", "#DCDDE1"
		sp.Modal, sp.Selection = "#FFFFFF", "#ECEDF0"
		sp.Text, sp.Bright, sp.Muted, sp.Subtle, sp.Border = "#3C4149", "#1B1C1F", "#6B6F76", "#A0A4AB", "#E0E1E5"
	}
	return sp
}

// Load reads dir/<name>.toml. Keys the file leaves out fall back to the
// built-in palette for its dark setting.
func Load(dir, name string) (Spec, error) {
	path := filepath.Join(dir, name+".toml")
	var probe struct {
		Dark *bool `toml:"dark"`
	}
	if _, err := toml.DecodeFile(path, &probe); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Spec{}, fmt.Errorf("theme %q not found (looked for %s)", name, path)
		}
		return Spec{}, fmt.Errorf("theme %q: %w", name, err)
	}
	dark := probe.Dark == nil || *probe.Dark
	sp := DefaultSpec(dark)
	_, err := toml.DecodeFile(path, &sp)
	return sp, err
}

// Resolve picks the theme for a config value: "auto", "dark", "light", or
// the name of a file in dir. termDark is the detected terminal background.
func Resolve(name, dir string, termDark bool) (*Theme, error) {
	switch name {
	case "", "auto":
		return New(termDark), nil
	case "dark":
		return New(true), nil
	case "light":
		return New(false), nil
	}
	sp, err := Load(dir, name)
	if err != nil {
		return New(termDark), err
	}
	return FromSpec(sp), nil
}
