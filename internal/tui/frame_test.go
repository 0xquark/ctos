package tui

import (
	"strings"
	"testing"

	"github.com/0xquark/ctos/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

// TestFrameExactSize is the guard that keeps the grid aligned: a frame must
// occupy exactly the box it was given, whatever the content does.
func TestFrameExactSize(t *testing.T) {
	th := theme.New("")

	cases := []struct {
		name    string
		w, h    int
		title   string
		content string
	}{
		{"empty content", 30, 8, "clock", ""},
		{"short content", 30, 8, "clock", "12:00"},
		{"too many lines", 30, 6, "notes", strings.Repeat("line\n", 20)},
		{"line too wide", 30, 6, "notes", strings.Repeat("x", 200)},
		{"no title", 30, 6, "", "hello"},
		{"title longer than frame", 20, 5, "an extremely long widget title", "hi"},
		{"minimum usable", 6, 3, "t", "x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Frame(th, tc.title, FrameIdle, tc.w, tc.h, tc.content)

			lines := strings.Split(out, "\n")
			if len(lines) != tc.h {
				t.Errorf("got %d lines, want %d", len(lines), tc.h)
			}
			for i, line := range lines {
				if got := lipgloss.Width(line); got != tc.w {
					t.Errorf("line %d is %d cells, want %d: %q", i, got, tc.w, line)
				}
			}
		})
	}
}

// TestFrameMovingIsDistinct checks layout mode is visually unambiguous: the
// widget being moved must not look the same as the merely focused one.
func TestFrameMovingIsDistinct(t *testing.T) {
	th := theme.New("")
	focused := Frame(th, "notes", FrameFocused, 40, 5, "")
	moving := Frame(th, "notes", FrameMoving, 40, 5, "")

	if focused == moving {
		t.Error("a moving widget renders identically to a focused one")
	}
	if !strings.Contains(moving, "moving") {
		t.Errorf("moving frame should say so in its title: %q", strings.Split(moving, "\n")[0])
	}
	// The size contract still holds with the longer title.
	for i, line := range strings.Split(moving, "\n") {
		if got := lipgloss.Width(line); got != 40 {
			t.Errorf("line %d is %d cells, want 40", i, got)
		}
	}
}

// TestFrameTitleShown checks the title actually lands in the top border.
func TestFrameTitleShown(t *testing.T) {
	out := Frame(theme.New(""), "hacker news", FrameIdle, 40, 5, "")
	top := strings.Split(out, "\n")[0]
	if !strings.Contains(top, "hacker news") {
		t.Errorf("title missing from top border: %q", top)
	}
}

// TestFrameDegenerateSize checks a frame too small to draw does not panic.
func TestFrameDegenerateSize(t *testing.T) {
	for _, s := range []struct{ w, h int }{{0, 0}, {1, 1}, {4, 2}, {-5, 3}} {
		if out := Frame(theme.New(""), "t", FrameFocused, s.w, s.h, "x"); strings.Contains(out, "\n") {
			t.Errorf("%dx%d should collapse to a single line, got %q", s.w, s.h, out)
		}
	}
}
