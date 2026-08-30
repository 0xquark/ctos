package tui

import (
	"strings"

	"github.com/0xquark/ctos/internal/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// FrameOverhead is the number of cells the frame consumes on each axis:
// a border and one column of padding on each side horizontally, and a border
// row top and bottom vertically.
const (
	FrameOverheadX = 4
	FrameOverheadY = 2
)

// FrameState selects how a widget's border is drawn.
type FrameState int

const (
	// FrameIdle is an unfocused widget.
	FrameIdle FrameState = iota
	// FrameFocused is the widget receiving keys.
	FrameFocused
	// FrameMoving is the widget being repositioned in layout mode.
	FrameMoving
)

// Frame draws a titled box of exactly w by h cells around content.
//
// lipgloss has no border-title support, so the top border is assembled by hand.
// content is expected to already fit the inner area; anything longer is
// truncated ANSI-safely rather than allowed to break the layout.
func Frame(t theme.Theme, title string, state FrameState, w, h int, content string) string {
	if w < FrameOverheadX+1 || h < FrameOverheadY+1 {
		return strings.Repeat(" ", max(0, w))
	}

	borderColor := t.Border
	titleStyle := t.DimStyle()
	switch state {
	case FrameFocused:
		borderColor = t.BorderFocus
		titleStyle = t.AccentStyle().Bold(true)
	case FrameMoving:
		// Deliberately not the accent colour: in layout mode the user
		// needs to tell "focused" from "currently being moved".
		borderColor = t.Warn
		titleStyle = t.WarnStyle().Bold(true)
		title += " ◆ moving"
	}
	border := lipgloss.NewStyle().Foreground(borderColor)
	c := t.Chrome

	innerW := w - FrameOverheadX
	innerH := h - FrameOverheadY

	var b strings.Builder
	b.WriteString(topBorder(c, border, titleStyle, title, w))
	b.WriteByte('\n')

	lines := strings.Split(content, "\n")
	for i := 0; i < innerH; i++ {
		var line string
		if i < len(lines) {
			line = lines[i]
		}
		if lipgloss.Width(line) > innerW {
			line = ansi.Truncate(line, innerW, "…")
		}
		pad := strings.Repeat(" ", max(0, innerW-lipgloss.Width(line)))

		b.WriteString(border.Render(c.Vertical))
		b.WriteByte(' ')
		b.WriteString(line)
		b.WriteString(pad)
		b.WriteByte(' ')
		b.WriteString(border.Render(c.Vertical))
		b.WriteByte('\n')
	}

	b.WriteString(border.Render(c.BottomLeft + strings.Repeat(c.Horizontal, w-2) + c.BottomRight))
	return b.String()
}

// topBorder renders "╭─ title ───────╮", or whatever the theme's chrome spells
// that as, trimming the title if it would not fit.
func topBorder(c theme.Chrome, border, titleStyle lipgloss.Style, title string, w int) string {
	span := w - 2 // between the corners
	plain := border.Render(c.TopLeft + strings.Repeat(c.Horizontal, span) + c.TopRight)

	if title == "" {
		return plain
	}

	decoration := lipgloss.Width(c.TitleOpen) + lipgloss.Width(c.TitleClose)
	avail := span - decoration
	if avail < 1 {
		return plain
	}
	if lipgloss.Width(title) > avail {
		title = ansi.Truncate(title, avail, "…")
	}

	fill := avail - lipgloss.Width(title)
	return border.Render(c.TopLeft+c.TitleOpen) +
		titleStyle.Render(title) +
		border.Render(c.TitleClose+strings.Repeat(c.Horizontal, max(0, fill))+c.TopRight)
}
