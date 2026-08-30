package widget

import tea "github.com/charmbracelet/bubbletea"

// List is the cursor and scroll state behind a vertical list of items. Every
// list-shaped widget needs the same four things — where the selection is, what
// slice of the items is on screen, how the arrow keys move, and how to keep
// the two in step — and getting the clamping subtly wrong is easy, so it lives
// here once.
//
// List holds no items and does no drawing: the widget owns its data and its
// layout. Nothing here knows the pane's height either, because a widget may
// give the list only part of its frame (notes keeps rows back for the preview
// pane). Height is passed to Window at render time instead.
//
// The zero List is an empty list with the cursor at the top.
type List struct {
	n      int
	cursor int
	offset int
}

// SetLen records how many items the list now holds, after a refresh or a
// filter. The cursor is clamped into the new range, so a list that shrinks
// under the selection leaves it on the last item rather than past the end.
func (l *List) SetLen(n int) {
	l.n = max(n, 0)
	if l.cursor >= l.n {
		l.cursor = max(0, l.n-1)
	}
}

// Len is the number of items.
func (l *List) Len() int { return l.n }

// Empty reports whether there is anything to select.
func (l *List) Empty() bool { return l.n == 0 }

// Cursor is the index of the selected item. It is 0 for an empty list, so a
// caller must check Empty before indexing its own slice.
func (l *List) Cursor() int { return l.cursor }

// Select moves the cursor to i, clamped to the list.
func (l *List) Select(i int) { l.cursor = min(max(i, 0), max(0, l.n-1)) }

// Move shifts the cursor by delta, clamped to the list. It stops at the ends
// rather than wrapping: in a long process table, wrapping from top to bottom
// on one keypress loses the user's place.
func (l *List) Move(delta int) { l.Select(l.cursor + delta) }

// Top puts the cursor on the first item and scrolls the view back to it. Use
// it when the order changes underneath the user, such as on a re-sort.
func (l *List) Top() { l.cursor, l.offset = 0, 0 }

// Window is the half-open range of items visible in a pane of the given
// height, scrolled so the cursor is inside it. Call it while rendering, then
// range over the widget's own items between start and end.
func (l *List) Window(height int) (start, end int) {
	if height <= 0 || l.n == 0 {
		return 0, 0
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+height {
		l.offset = l.cursor - height + 1
	}
	l.offset = min(max(l.offset, 0), max(0, l.n-height))
	return l.offset, min(l.offset+height, l.n)
}

// HandleKey applies the navigation keys every ctOS list shares — ↑/k, ↓/j,
// pgup, pgdown, home/g, end/G — and reports whether it consumed the key, so a
// widget can layer its own bindings on top:
//
//	if p.list.HandleKey(msg, p.listHeight()) {
//	    return p.afterMove()
//	}
//
// page is how far pgup and pgdown travel, normally the height of the pane.
func (l *List) HandleKey(msg tea.KeyMsg, page int) bool {
	switch msg.String() {
	case "up", "k":
		l.Move(-1)
	case "down", "j":
		l.Move(1)
	case "pgup":
		l.Move(-max(1, page))
	case "pgdown":
		l.Move(max(1, page))
	case "home", "g":
		l.Select(0)
	case "end", "G":
		l.Select(l.n - 1)
	default:
		return false
	}
	return true
}
