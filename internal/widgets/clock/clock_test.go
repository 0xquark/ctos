package clock

import (
	"strings"
	"testing"

	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
)

func newClock(t *testing.T, w, h int, big bool) *Clock {
	t.Helper()
	wdg, err := New(widget.Context{Name: "clock", Theme: theme.New("")})
	if err != nil {
		t.Fatal(err)
	}
	c := wdg.(*Clock)
	c.cfg.Big = big
	c.SetSize(w, h)
	return c
}

// TestRenderBigGlyphs pins the block-digit output so a typo in the glyph table
// is caught rather than silently drawn.
func TestRenderBigGlyphs(t *testing.T) {
	c := newClock(t, 80, 10, true)

	got, ok := c.renderBig("10:45")
	if !ok {
		t.Fatal("renderBig declined a size that should fit")
	}

	want := strings.Join([]string{
		"  ╷ ╭─╮     ╷ ╷ ╭─╴",
		"  │ │ │  ▪  ╰─┤ ╰─╮",
		"  ╵ ╰─╯       ╵ ╰─╯",
	}, "\n")

	if stripANSI(got) != want {
		t.Errorf("block digits wrong.\n got:\n%s\nwant:\n%s", stripANSI(got), want)
	}
}

// TestRenderBigDeclines checks the fallback conditions rather than letting the
// clock overflow its frame.
func TestRenderBigDeclines(t *testing.T) {
	tests := []struct {
		name    string
		w, h    int
		big     bool
		input   string
		wantsOK bool
	}{
		{"fits", 40, 3, true, "15:04:05", true},
		{"too narrow", 10, 6, true, "15:04:05", false},
		{"too short", 40, 2, true, "15:04:05", false},
		{"big disabled", 40, 6, false, "15:04:05", false},
		{"unknown glyph", 80, 6, true, "15h04 CEST", false},
		{"empty", 40, 6, true, "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newClock(t, tc.w, tc.h, tc.big)
			if _, ok := c.renderBig(tc.input); ok != tc.wantsOK {
				t.Errorf("renderBig(%q) at %dx%d = %v, want %v", tc.input, tc.w, tc.h, ok, tc.wantsOK)
			}
		})
	}
}

// TestGlyphTableShape guards the invariant the renderer relies on: every glyph
// is exactly glyphWidth cells across on every row.
func TestGlyphTableShape(t *testing.T) {
	for r, rows := range glyphs {
		for i, row := range rows {
			if got := len([]rune(row)); got != glyphWidth {
				t.Errorf("glyph %q row %d is %d cells wide, want %d", r, i, got, glyphWidth)
			}
		}
	}
}

// stripANSI removes escape sequences so tests compare glyphs, not colours.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}
