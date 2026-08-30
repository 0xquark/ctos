package tasks

import (
	"fmt"
	"strings"
	"time"

	"github.com/0xquark/ctos/internal/todo"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The pieces of a row, in cells.
const (
	markerWidth = 2 // "▸ "
	boxWidth    = 2 // the checkbox and the space after it
	gap         = 1 // between the text and the date column
	dueWidth    = 9 // "yesterday", "15 Sep 26" — DueLabel's longest
	minText     = 12
)

// The glyphs. A checkbox says the one thing every row has to say, and the
// space after it is not decoration: these are East Asian Ambiguous, so a
// terminal may draw them across two columns where the width tables count one,
// and a glyph pressed against its text reads as a smudge (ADR-038).
const (
	openBox = "☐"
	doneBox = "☑"
	lateM   = "⚠"
	todayM  = "◷"
	failM   = "⚠"
)

// View renders the checklist.
func (t *Tasks) View() string {
	switch {
	case t.H <= 0 || t.W <= 0:
		return ""
	case t.err != nil:
		return t.theme.BadStyle().Render(t.cut(failM + " " + t.err.Error()))
	case !t.loaded:
		return t.theme.DimStyle().Render("reading " + t.path + "…")
	}

	// One line is a status strip, not a list: the same shape the system and
	// git widgets take in the bar, and for the same reason.
	if t.H == 1 {
		return t.strip()
	}

	lines := append([]string{t.headerLine()}, t.listLines(t.listHeight())...)
	return strings.Join(lines, "\n")
}

// headerLines is what the list does not get: the one line above it.
func (t *Tasks) headerLines() int { return 1 }

// listHeight is the room left for tasks.
func (t *Tasks) listHeight() int { return max(0, t.H-t.headerLines()) }

// headerLine is the top line: the input box, an armed prompt, the result of the
// last write, or the summary — in that order of urgency.
func (t *Tasks) headerLine() string {
	switch {
	case t.typing:
		return t.inputLine()
	case t.arm != armedNone:
		return t.armedLine()
	case t.status != "":
		return t.cut(t.theme.WarnStyle().Render(" " + t.status))
	default:
		return t.summaryLine()
	}
}

// inputLine is the add/edit box. A block cursor makes it obvious the widget is
// swallowing keystrokes.
func (t *Tasks) inputLine() string {
	label := " + "
	if t.editing {
		label = " ✎ "
	}
	line := t.theme.AccentStyle().Render(label) +
		t.theme.TextStyle().Render(t.input) +
		t.theme.AccentStyle().Render("█")

	// The date syntax is only discoverable if something says it, and an
	// empty box is exactly when there is room to.
	if t.input == "" {
		hint := t.theme.FaintStyle().Render("  task, or \"pay rent due:fri\"")
		if lipgloss.Width(line+hint) <= t.W {
			line += hint
		}
	}
	return t.cut(line)
}

// armedLine asks before throwing work away.
func (t *Tasks) armedLine() string {
	var prompt, key string
	switch t.arm {
	case armedDelete:
		prompt, key = " delete "+quote(t.target.Text)+"?", "d"
	default:
		prompt, key = fmt.Sprintf(" clear %s?", plural(t.counts().Done, "completed task")), "x"
	}

	keys := t.theme.DimStyle().Render("  ") +
		t.theme.AccentStyle().Render("↵ "+key) + t.theme.DimStyle().Render(" yes  ") +
		t.theme.AccentStyle().Render("esc") + t.theme.DimStyle().Render(" cancel")

	line := t.theme.BadStyle().Bold(true).Render(prompt) + keys
	if lipgloss.Width(line) > t.W {
		return t.cut(t.theme.BadStyle().Bold(true).Render(prompt))
	}
	return line
}

// summaryLine is the glanceable state of the list: what is late, what is due,
// what is left, and which view is on.
func (t *Tasks) summaryLine() string {
	c := t.counts()
	var parts []string

	if c.Overdue > 0 {
		parts = append(parts, t.theme.BadStyle().Bold(true).Render(fmt.Sprintf("%s %d", lateM, c.Overdue))+
			t.theme.DimStyle().Render(" overdue"))
	}
	if c.Today > 0 {
		parts = append(parts, t.theme.WarnStyle().Render(fmt.Sprintf("%s %d", todayM, c.Today))+
			t.theme.DimStyle().Render(" today"))
	}
	parts = append(parts, t.theme.TextStyle().Render(fmt.Sprintf("%d", c.Open))+t.theme.DimStyle().Render(" open"))
	// The view comes before the done count: it is the reason the list is
	// short, and a summary that does not explain that is misleading.
	if t.show != todo.ShowAll {
		parts = append(parts, t.theme.DimStyle().Render("showing ")+
			t.theme.AccentStyle().Render(string(t.show)))
	}
	if c.Done > 0 {
		parts = append(parts, t.theme.FaintStyle().Render(fmt.Sprintf("%d done", c.Done)))
	}

	return fit(parts, t.theme.FaintStyle().Render(" · "), t.W)
}

// strip renders the whole list on one line, for the status bar: what is late,
// what is due today, and the next thing to do.
func (t *Tasks) strip() string {
	if t.err != nil {
		return t.theme.BadStyle().Render(t.cut(" " + failM + " tasks"))
	}
	c := t.counts()
	if c.Open == 0 {
		if c.Done == 0 {
			return t.theme.DimStyle().Render(" no tasks")
		}
		return t.theme.GoodStyle().Render(" ✓ all done")
	}

	var parts []string
	if c.Overdue > 0 {
		parts = append(parts, t.theme.BadStyle().Bold(true).Render(fmt.Sprintf("%s %d", lateM, c.Overdue))+
			t.theme.DimStyle().Render(" late"))
	}
	if c.Today > 0 {
		parts = append(parts, t.theme.WarnStyle().Render(fmt.Sprintf("%s %d", todayM, c.Today))+
			t.theme.DimStyle().Render(" today"))
	}
	parts = append(parts, t.theme.TextStyle().Render(fmt.Sprintf("%d", c.Open))+t.theme.DimStyle().Render(" open"))

	// The count says how much there is; the task says what it is. It goes
	// last so a narrow bar drops it first.
	if next, ok := t.next(); ok {
		parts = append(parts, t.theme.FaintStyle().Render("▸ ")+t.theme.TextStyle().Render(next.Text))
	}
	return fit(parts, t.theme.FaintStyle().Render(" · "), t.W)
}

// next is the most pressing open task: the first one a sorted list would show.
func (t *Tasks) next() (todo.Task, bool) {
	open := todo.Filter(t.file.Tasks, todo.ShowOpen, time.Now())
	if len(open) == 0 {
		return todo.Task{}, false
	}
	todo.Sort(open, time.Now())
	return open[0], true
}

// listLines renders the visible slice of the list, with a heading above each
// change of group when there is room for them.
func (t *Tasks) listLines(height int) []string {
	if height <= 0 {
		return nil
	}
	if len(t.rows) == 0 {
		return pad([]string{t.theme.DimStyle().Render(" " + t.emptyText())}, height)
	}

	// Headings cost a line each, so the scroll is worked out against fewer
	// rows than there is room for — fewer by the number of groups the list
	// has, which is the most that can ever be drawn. Reserving that worst
	// case is what guarantees the cursor is still on screen once the
	// headings are in.
	buckets := t.buckets()
	start, _ := t.list.Window(max(1, height-buckets))

	// Filling is then greedy from there, so a list with fewer groups on
	// screen than the worst case does not leave the reserved lines blank.
	now := time.Now()
	lines := make([]string, 0, height)
	last := todo.Bucket(-1)

	for i := start; i < len(t.rows) && len(lines) < height; i++ {
		if b := todo.BucketOf(t.rows[i], now); buckets > 0 && b != last {
			// A heading with no room for a task under it is just a
			// line of noise at the bottom of the pane.
			if len(lines)+2 > height {
				break
			}
			lines = append(lines, t.heading(b.String()))
			last = b
		}
		lines = append(lines, t.row(t.rows[i], i == t.list.Cursor()))
	}
	return pad(lines, height)
}

// buckets is how many group headings the list could draw, and 0 when grouping
// is off or the pane is too short to spend lines on them.
func (t *Tasks) buckets() int {
	// Below this the headings are taking the space they are meant to be
	// organising.
	const minForGroups = 6
	if !t.group || t.H < minForGroups {
		return 0
	}
	now := time.Now()
	seen := map[todo.Bucket]bool{}
	for _, task := range t.rows {
		seen[todo.BucketOf(task, now)] = true
	}
	return len(seen)
}

// heading labels a group.
func (t *Tasks) heading(name string) string {
	label := " " + strings.ToUpper(name) + " "
	rule := ""
	if n := t.W - lipgloss.Width(label); n > 0 {
		rule = strings.Repeat("─", n)
	}
	return t.theme.FaintStyle().Render(label + rule)
}

// row renders one task: marker, checkbox, text, then a right-aligned date.
func (t *Tasks) row(task todo.Task, selected bool) string {
	marker := "  "
	if selected {
		marker = "▸ "
	}

	box, boxStyle := openBox, t.theme.DimStyle()
	if task.Done {
		box, boxStyle = doneBox, t.theme.GoodStyle()
	}

	textStyle := t.theme.TextStyle()
	switch {
	case task.Done:
		// Struck through as well as dimmed: a finished task should read
		// as finished at a glance, not as a task in a paler colour.
		textStyle = t.theme.FaintStyle().Strikethrough(true)
	case selected && t.Focused():
		textStyle = t.theme.AccentStyle().Bold(true)
	case selected:
		textStyle = t.theme.TextStyle().Bold(true)
	}

	due := todo.DueLabel(task.Due, time.Now())
	dueCol := 0
	if due != "" {
		dueCol = dueWidth + gap
	}

	textW := t.W - markerWidth - boxWidth - dueCol
	if textW < minText {
		// No room for a date column; the task itself is the point.
		textW, dueCol = t.W-markerWidth-boxWidth, 0
	}
	if textW < 1 {
		return t.cut(textStyle.Render(marker + task.Text))
	}

	text := ansi.Truncate(task.Text, textW, "…")
	line := t.theme.FaintStyle().Render(marker) +
		boxStyle.Render(box) + " " +
		textStyle.Render(text)

	if dueCol == 0 {
		return line
	}
	line += strings.Repeat(" ", max(0, textW-lipgloss.Width(text))+gap)
	return line + align(t.dueStyle(task).Render(due), dueWidth)
}

// dueStyle colours a date by how much attention it is asking for.
func (t *Tasks) dueStyle(task todo.Task) lipgloss.Style {
	switch todo.BucketOf(task, time.Now()) {
	case todo.Overdue:
		return t.theme.BadStyle().Bold(true)
	case todo.Today:
		return t.theme.WarnStyle()
	case todo.Done:
		return t.theme.FaintStyle()
	default:
		return t.theme.DimStyle()
	}
}

// emptyText explains an empty list, which usually means the filter rather than
// the file.
func (t *Tasks) emptyText() string {
	if len(t.file.Tasks) == 0 {
		return "nothing here yet — press a to add a task"
	}
	switch t.show {
	case todo.ShowToday:
		return "nothing due today — press f for the full list"
	case todo.ShowOpen:
		return "everything is done"
	default:
		return "no tasks"
	}
}

// Lines is the widget.Liner contract: how tall this widget's content wants to
// be.
//
// A widget offered a single line takes it and renders the strip, which is how
// the status bar gets a summary without the dashboard having to ask for one.
// Anywhere taller it wants a line per task, plus its header and its groups.
func (t *Tasks) Lines(int) int {
	if t.H <= 1 {
		return 1
	}
	return t.headerLines() + max(1, len(t.rows)) + t.buckets()
}

// cut truncates to the widget's width, counting display cells rather than
// bytes and leaving any styling intact.
func (t *Tasks) cut(s string) string { return ansi.Truncate(s, t.W, "…") }

// fit joins as many parts as the width allows, dropping from the right: the
// parts are written most important first.
func fit(parts []string, sep string, w int) string {
	for n := len(parts); n > 0; n-- {
		line := " " + strings.Join(parts[:n], sep)
		if lipgloss.Width(line) <= w {
			return line
		}
	}
	return ""
}

// align right-justifies to w display cells, which is what a column of dates
// needs to read as a column.
func align(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// pad fills a pane out to its full height, so what is below the list does not
// slide up as the list shrinks.
func pad(lines []string, height int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines[:height]
}
