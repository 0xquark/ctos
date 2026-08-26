package tui

import (
	"strings"

	"github.com/0xquark/ctos/internal/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Frame border runes. Rounded corners match the rest of the Charm ecosystem.
const (
	cornerTL = "╭"
	cornerTR = "╮"
	cornerBL = "╰"
	cornerBR = "╯"
	horiz    = "─"
	vert     = "│"
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

	innerW := w - FrameOverheadX
	innerH := h - FrameOverheadY

	var b strings.Builder
	b.WriteString(topBorder(border, titleStyle, title, w))
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

		b.WriteString(border.Render(vert))
		b.WriteByte(' ')
		b.WriteString(line)
		b.WriteString(pad)
		b.WriteByte(' ')
		b.WriteString(border.Render(vert))
		b.WriteByte('\n')
	}

	b.WriteString(border.Render(cornerBL + strings.Repeat(horiz, w-2) + cornerBR))
	return b.String()
}

// topBorder renders "╭─ title ───────╮", trimming the title if it would not fit.
func topBorder(border, titleStyle lipgloss.Style, title string, w int) string {
	span := w - 2 // between the corners

	if title == "" {
		return border.Render(cornerTL + strings.Repeat(horiz, span) + cornerTR)
	}

	// "─ " before and " " after the title.
	const decoration = 3
	avail := span - decoration
	if avail < 1 {
		return border.Render(cornerTL + strings.Repeat(horiz, span) + cornerTR)
	}
	if lipgloss.Width(title) > avail {
		title = ansi.Truncate(title, avail, "…")
	}

	fill := span - decoration - lipgloss.Width(title)
	return border.Render(cornerTL+horiz+" ") +
		titleStyle.Render(title) +
		border.Render(" "+strings.Repeat(horiz, max(0, fill))+cornerTR)
}
