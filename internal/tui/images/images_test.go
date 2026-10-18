package images

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPlaceholderCells(t *testing.T) {
	s := Placeholder(idBase+1, 2)
	if w := lipgloss.Width(s); w != 2 {
		t.Fatalf("width = %d, want 2", w)
	}
	if !strings.Contains(s, "38;2;76;73;1") {
		t.Fatalf("id not encoded in foreground: %q", s)
	}
	plain := ansi.Strip(s)
	want := "\U0010EEEE\u0305\u0305\U0010EEEE\u0305\u030D"
	if plain != want {
		t.Fatalf("cells = %q, want %q", plain, want)
	}
}

func TestTransmitChunks(t *testing.T) {
	seq := Transmit(5, 2, 1, bytes.Repeat([]byte{1}, 5000))
	if !strings.HasPrefix(seq, "\x1b_Ga=T,U=1,f=100,t=d,q=2,i=5,c=2,r=1,m=1;") {
		t.Fatalf("bad first chunk: %.60q", seq)
	}
	if !strings.Contains(seq, "\x1b_Gm=0;") || strings.Count(seq, "\x1b\\") != 2 {
		t.Fatalf("expected two chunks ending with m=0: %d", strings.Count(seq, "\x1b\\"))
	}
}

func TestAvatarMask(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 50))
	for y := range 50 {
		for x := range 100 {
			src.Set(x, y, color.RGBA{200, 10, 10, 255})
		}
	}
	out := Avatar(src, 16)
	if out.Bounds().Dx() != 16 || out.NRGBAAt(0, 0).A != 0 || out.NRGBAAt(8, 8).A != 255 {
		t.Fatalf("mask wrong: corner %v center %v", out.NRGBAAt(0, 0), out.NRGBAAt(8, 8))
	}
	if c := out.NRGBAAt(8, 8); c.R != 200 || c.G != 10 {
		t.Fatalf("color not preserved: %v", c)
	}
}

func TestManagerLoadsOnce(t *testing.T) {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	fetches := 0
	got := make(chan tea.Msg, 1)
	m := NewManager()
	m.Fetch = func(context.Context, string) ([]byte, error) { fetches++; return buf.Bytes(), nil }
	m.Send = func(msg tea.Msg) { got <- msg }
	if _, ok := m.Cells("u", 2); ok {
		t.Fatal("first call should start loading")
	}
	select {
	case msg := <-got:
		ready := msg.(ReadyMsg)
		if !strings.HasPrefix(ready.Seq, "\x1b_G") {
			t.Fatal("ready msg missing transmit sequence")
		}
		if _, ok := m.Cells("u", 2); ok {
			t.Fatal("placeholders must wait until the upload is written")
		}
		m.Shown(ready.Key)
	case <-time.After(2 * time.Second):
		t.Fatal("image never loaded")
	}
	if s, ok := m.Cells("u", 2); !ok || lipgloss.Width(s) != 2 {
		t.Fatalf("cells = %q, %v", s, ok)
	}
	if fetches != 1 || !strings.Contains(m.Cleanup(), "a=d,d=I") {
		t.Fatalf("fetches = %d", fetches)
	}
}
