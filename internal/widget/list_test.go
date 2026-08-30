package widget

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestListMoveStopsAtTheEnds(t *testing.T) {
	var l List
	l.SetLen(3)

	l.Move(-1)
	if l.Cursor() != 0 {
		t.Errorf("moving up from the top wrapped to %d", l.Cursor())
	}
	l.Move(10)
	if l.Cursor() != 2 {
		t.Errorf("moving down past the end gave %d, want 2", l.Cursor())
	}
}

// A refresh that drops items must not leave the cursor past the end.
func TestListShrinkingClampsTheCursor(t *testing.T) {
	var l List
	l.SetLen(10)
	l.Select(9)

	l.SetLen(3)
	if l.Cursor() != 2 {
		t.Errorf("cursor = %d, want 2", l.Cursor())
	}

	l.SetLen(0)
	if l.Cursor() != 0 || !l.Empty() {
		t.Errorf("emptied list left cursor %d", l.Cursor())
	}
}

func TestListWindowFollowsTheCursor(t *testing.T) {
	var l List
	l.SetLen(100)

	if start, end := l.Window(10); start != 0 || end != 10 {
		t.Errorf("window = [%d,%d), want [0,10)", start, end)
	}

	// Walking down scrolls only once the cursor reaches the bottom row.
	l.Select(9)
	if start, _ := l.Window(10); start != 0 {
		t.Errorf("window scrolled early, start = %d", start)
	}
	l.Select(10)
	if start, end := l.Window(10); start != 1 || end != 11 {
		t.Errorf("window = [%d,%d), want [1,11)", start, end)
	}

	// Jumping to the end shows the last full page, not a page off the end.
	l.Select(99)
	if start, end := l.Window(10); start != 90 || end != 100 {
		t.Errorf("window = [%d,%d), want [90,100)", start, end)
	}
}

// A pane can be laid out with no room for the list at all, and a widget may
// hold no items yet. Neither may produce a range a caller cannot slice with.
func TestListWindowDegenerateCases(t *testing.T) {
	var l List
	if start, end := l.Window(10); start != 0 || end != 0 {
		t.Errorf("empty list window = [%d,%d), want [0,0)", start, end)
	}

	l.SetLen(5)
	for _, h := range []int{0, -3} {
		if start, end := l.Window(h); start != 0 || end != 0 {
			t.Errorf("height %d window = [%d,%d), want [0,0)", h, start, end)
		}
	}

	// A pane taller than the list shows all of it, and never scrolls.
	l.Select(4)
	if start, end := l.Window(20); start != 0 || end != 5 {
		t.Errorf("window = [%d,%d), want [0,5)", start, end)
	}
}

func TestListHandleKey(t *testing.T) {
	var l List
	l.SetLen(50)

	cases := []struct {
		key  string
		want int
	}{
		{"down", 1}, {"j", 2}, {"up", 1}, {"k", 0},
		{"pgdown", 10}, {"pgup", 0},
		{"G", 49}, {"g", 0}, {"end", 49}, {"home", 0},
	}
	for _, tc := range cases {
		if !l.HandleKey(key(tc.key), 10) {
			t.Fatalf("%q was not handled", tc.key)
		}
		if l.Cursor() != tc.want {
			t.Errorf("after %q cursor = %d, want %d", tc.key, l.Cursor(), tc.want)
		}
	}

	// Anything else belongs to the widget, which needs to know it is free.
	if l.HandleKey(key("/"), 10) {
		t.Error(`HandleKey claimed "/", which widgets bind themselves`)
	}
}

// Top exists for a re-sort: the view must follow the cursor back up, not stay
// scrolled where it was.
func TestListTopResetsTheScroll(t *testing.T) {
	var l List
	l.SetLen(100)
	l.Select(80)
	l.Window(10)

	l.Top()
	if start, _ := l.Window(10); l.Cursor() != 0 || start != 0 {
		t.Errorf("Top left cursor %d, offset %d", l.Cursor(), start)
	}
}
