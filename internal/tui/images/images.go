// Package images shows remote images (avatars, logos) inline using the
// kitty graphics protocol. Images are drawn through Unicode placeholders,
// which are ordinary text cells, so they work with Bubble Tea's renderer,
// scrolling, and overlays.
package images

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	_ "golang.org/x/image/webp"
)

const (
	placeholder = "\U0010EEEE"
	// idBase keeps our image ids away from ones other programs in the same
	// terminal are likely to use.
	idBase = 0x4C4900
	// size is the pixel size images are scaled to before upload.
	size = 64
)

// diacritics encode row and column numbers in placeholder cells, from
// kitty's rowcolumn-diacritics table.
var diacritics = []rune{0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A}

// Supported reports whether the terminal can show kitty graphics with
// Unicode placeholders. mode is the "images" config value.
func Supported(mode string) bool {
	switch mode {
	case "on":
		return true
	case "off":
		return false
	}
	if os.Getenv("TMUX") != "" {
		return false
	}
	term, prog := os.Getenv("TERM"), os.Getenv("TERM_PROGRAM")
	return os.Getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty" ||
		prog == "ghostty" || term == "xterm-ghostty"
}

// ReadyMsg carries the escape sequence that uploads a loaded image. The app
// writes it with tea.Raw, then calls Shown so placeholders are only drawn
// once the terminal has the image.
type ReadyMsg struct {
	Key string
	Seq string
}

type entry struct {
	id    uint32
	ready bool
}

// Manager loads images in the background and hands out placeholder cells.
type Manager struct {
	// Fetch downloads an image.
	Fetch func(ctx context.Context, url string) ([]byte, error)
	// Send delivers ReadyMsg to the program (tea.Program.Send).
	Send func(tea.Msg)
	// Log, if set, records why an image could not be shown.
	Log func(format string, args ...any)

	mu      sync.Mutex
	entries map[string]*entry
	next    uint32
}

func NewManager() *Manager { return &Manager{entries: map[string]*entry{}} }

// Cells returns placeholder cells showing url in cols×1 cells. On the first
// call it starts loading and returns false; once the image is uploaded the
// next render shows it.
func (m *Manager) Cells(url string, cols int) (string, bool) {
	if url == "" || cols <= 0 || cols > len(diacritics) {
		return "", false
	}
	k := key(url, cols)
	m.mu.Lock()
	e := m.entries[k]
	if e == nil {
		e = &entry{id: idBase + m.next}
		m.next++
		m.entries[k] = e
		m.mu.Unlock()
		go m.load(url, cols, e)
		return "", false
	}
	ready := e.ready
	m.mu.Unlock()
	if !ready {
		return "", false
	}
	return Placeholder(e.id, cols), true
}

func (m *Manager) logf(format string, args ...any) {
	if m.Log != nil {
		m.Log(format, args...)
	}
}

func (m *Manager) load(url string, cols int, e *entry) {
	if m.Fetch == nil || m.Send == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	data, err := m.Fetch(ctx, url)
	if err != nil {
		m.logf("image %s: %v", url, err)
		return
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		m.logf("image %s: decode %d bytes (%q...): %v", url, len(data), sniff(data), err)
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, Avatar(src, size)); err != nil {
		m.logf("image %s: encode: %v", url, err)
		return
	}
	m.logf("image %s: loaded %s %v", url, format, src.Bounds().Size())
	m.Send(ReadyMsg{Key: key(url, cols), Seq: Transmit(e.id, cols, 1, buf.Bytes())})
}

func key(url string, cols int) string { return fmt.Sprintf("%d|%s", cols, url) }

func sniff(b []byte) string {
	if len(b) > 12 {
		b = b[:12]
	}
	return string(b)
}

// Shown marks an image as uploaded so placeholders can be drawn.
func (m *Manager) Shown(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[key]; e != nil {
		e.ready = true
	}
}

// Cleanup returns the sequence that frees every image this manager
// uploaded. Write it after the program exits.
func (m *Manager) Cleanup() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b strings.Builder
	for _, e := range m.entries {
		if e.ready {
			fmt.Fprintf(&b, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", e.id)
		}
	}
	return b.String()
}

// Transmit builds the kitty sequence that uploads a PNG and creates a
// virtual placement of cols×rows cells for Unicode placeholders.
func Transmit(id uint32, cols, rows int, pngData []byte) string {
	payload := base64.StdEncoding.EncodeToString(pngData)
	const chunk = 4096
	var b strings.Builder
	for i := 0; i < len(payload); i += chunk {
		end := min(len(payload), i+chunk)
		more := 0
		if end < len(payload) {
			more = 1
		}
		if i == 0 {
			fmt.Fprintf(&b, "\x1b_Ga=T,U=1,f=100,t=d,q=2,i=%d,c=%d,r=%d,m=%d;%s\x1b\\", id, cols, rows, more, payload[i:end])
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, payload[i:end])
		}
	}
	return b.String()
}

// Placeholder renders a row of cols placeholder cells for image id. The
// foreground color carries the image id.
func Placeholder(id uint32, cols int) string {
	var b strings.Builder
	for c := range cols {
		b.WriteString(placeholder)
		b.WriteRune(diacritics[0])
		b.WriteRune(diacritics[c])
	}
	fg := lipgloss.Color(fmt.Sprintf("#%06X", id&0xFFFFFF))
	return lipgloss.NewStyle().Foreground(fg).Render(b.String())
}

// Avatar center-crops src to a square, scales it to n×n with box
// filtering, and masks it to an anti-aliased circle, like Linear's avatars.
func Avatar(src image.Image, n int) *image.NRGBA {
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	out := image.NewNRGBA(image.Rect(0, 0, n, n))
	r := float64(n) / 2
	for y := range n {
		for x := range n {
			sx0, sx1 := x0+x*side/n, x0+max((x+1)*side/n, x*side/n+1)
			sy0, sy1 := y0+y*side/n, y0+max((y+1)*side/n, y*side/n+1)
			var rs, gs, bs, as, cnt uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					rs, gs, bs, as, cnt = rs+uint64(cr), gs+uint64(cg), bs+uint64(cb), as+uint64(ca), cnt+1
				}
			}
			c := color.NRGBA64{R: uint16(rs / cnt), G: uint16(gs / cnt), B: uint16(bs / cnt), A: uint16(as / cnt)}
			px := color.NRGBAModel.Convert(c).(color.NRGBA)
			d := math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r)
			px.A = uint8(float64(px.A) * math.Max(0, math.Min(1, r-d)))
			out.SetNRGBA(x, y, px)
		}
	}
	return out
}
