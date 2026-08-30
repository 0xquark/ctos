package git

import (
	"fmt"
	"strings"

	"github.com/0xquark/ctos/internal/humanize"
	"github.com/0xquark/ctos/internal/repos"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The pieces of a row, in cells.
const (
	markerWidth = 2  // "▸ "
	gap         = 1  // between columns
	minName     = 8  // a repository the user can still recognise
	maxName     = 22 // beyond this a long name is truncated rather than eating the row
	refWidth    = 14 // "feature/branch", or a short commit id
	stateWidth  = 13 // "● 12 ↑ 3 ↓ 2" at its widest realistic
	ageWidth    = 4  // "12m", "3d", "18mo" — humanize.RelTime's longest
)

// The glyphs the state column uses. One symbol per kind of drift, so a repo's
// state reads without a legend: a filled circle is work that is not committed,
// the arrows are commits that have not moved between here and the remote.
const (
	dirtyMark  = "●"
	aheadMark  = "↑"
	behindMark = "↓"
	cleanMark  = "✓"
	failMark   = "⚠"
)

// mark writes a glyph and its count with a space between them.
//
// The space is not decoration. Every glyph above is East Asian Ambiguous, so a
// terminal is free to draw it across two columns while the width tables — and
// so lipgloss, and so every column this widget lays out — count it as one.
// Even where the advance is one cell, these are ink-heavy glyphs that crowd
// whatever sits against them: "●12" reads as one smudge. A space gives the
// glyph somewhere to spill and the number somewhere to start.
func mark(glyph string, n int) string { return fmt.Sprintf("%s %d", glyph, n) }

// View renders the repository list.
func (g *Git) View() string {
	switch {
	case g.H <= 0 || g.W <= 0:
		return ""
	case g.err != nil:
		return g.theme.BadStyle().Render(ansi.Truncate(failMark+" "+g.err.Error(), g.W, "…"))
	case !g.loaded:
		return g.theme.DimStyle().Render("reading repositories…")
	}

	list := g.visible()

	// One line is a status strip, not a list: the same shape the system
	// widget takes in the bar, and for the same reason.
	if g.H == 1 {
		if len(list) == 0 {
			return g.theme.DimStyle().Render(g.emptyText())
		}
		return g.strip(list)
	}

	// A pane wide enough carries both panels at once, the way lazygit does:
	// what you are choosing between on the left, what you have chosen on
	// the right. A narrow one shows the panel the cursor is in.
	if lw, dw := g.split(); dw > 0 {
		return sideBySide(g.listPanel(list, lw), g.detailPanel(dw), lw, dw, g.H)
	}
	if g.mode == modeRepo {
		return strings.Join(g.repoPanel(g.W), "\n")
	}
	return strings.Join(g.listPanel(list, g.W), "\n")
}

// split is how the width is divided between the two panels. A zero detail
// width means there is only room for one.
//
// The list is the panel with a floor: its columns are a table, and a table
// squeezed below its name and state columns has stopped being one. The detail
// panel is text and will use whatever it is given.
func (g *Git) split() (list, detail int) {
	if !g.detail || g.W < minSplit {
		return g.W, 0
	}
	// Nothing selected is nothing to detail. Splitting anyway would draw a
	// rule down an empty pane and squeeze the message explaining why the
	// list is empty into half the width.
	if _, ok := g.target(); !ok {
		return g.W, 0
	}
	d := g.detailCols
	if d == 0 {
		d = g.W * detailShare / 100
	}
	d = min(d, g.W-minListPanel-splitGap)
	if d < minDetailPanel {
		return g.W, 0
	}
	return g.W - d - splitGap, d
}

// The two panels, in cells.
const (
	// minListPanel keeps the name and state columns, which is the pair
	// that answers "is there anything to do here?".
	minListPanel = 26

	// minDetailPanel is the narrowest a file path and a commit subject are
	// worth printing next to each other.
	minDetailPanel = 24

	// splitGap is the rule between the panels, plus a space either side.
	splitGap = 3

	// detailShare is the detail panel's cut of the width, as a percentage.
	// The list is a fixed-width table and the detail is prose, so the
	// larger half goes to the side that can spend it.
	detailShare = 55

	// minSplit is the narrowest pane worth dividing at all.
	minSplit = minListPanel + minDetailPanel + splitGap
)

// sideBySide draws the two panels with a rule between them, each padded to its
// own width so the rule stays straight down the pane.
func sideBySide(left, right []string, lw, rw, h int) string {
	rule := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render("│")

	out := make([]string, h)
	for i := range out {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = padTo(l, lw) + " " + rule + " " + ansi.Truncate(r, rw, "…")
	}
	return strings.Join(out, "\n")
}

// padTo pads a line to exactly w cells, cutting it if a widget overran.
func padTo(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	if n := w - lipgloss.Width(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// listPanel is the repository list and its summary, as lines.
func (g *Git) listPanel(list []repos.Repo, w int) []string {
	if len(list) == 0 {
		return []string{g.theme.DimStyle().Render(ansi.Truncate(g.emptyText(), w, "…"))}
	}

	lines := make([]string, 0, g.H)
	if g.headerLines() > 0 {
		lines = append(lines, g.summaryLine(list, w))
	}
	start, end := g.list.Window(g.listHeight())
	for i := start; i < end; i++ {
		lines = append(lines, g.row(list[i], i == g.list.Cursor(), w))
	}
	return lines
}

// detailPanel is the selected repository: what has changed in it, then what
// has happened in it lately.
//
// It is the answer to a list of repositories being mostly empty space on a
// wide pane. It is also the answer to having to drill in before you can see
// whether drilling in is worth it.
func (g *Git) detailPanel(w int) []string {
	r, ok := g.target()
	if !ok {
		return nil
	}
	if r.Err != nil {
		return []string{g.theme.BadStyle().Render(r.Err.Error())}
	}

	lines := []string{g.detailHeader(r, w)}

	// The changes get the room they need and the history takes what is
	// left: a file you are about to stage is more urgent than a commit
	// that already happened.
	files := g.filesSection(r, w, max(1, g.H-3))
	lines = append(lines, files...)

	if room := g.H - len(lines) - 1; room >= 2 {
		lines = append(lines, "")
		lines = append(lines, g.commitsSection(r, w, room-1)...)
	}
	return lines
}

// detailHeader names the repository, or shows what just happened to it.
func (g *Git) detailHeader(r repos.Repo, w int) string {
	if g.typing {
		// A block cursor makes it obvious the widget has the keyboard.
		return ansi.Truncate(g.theme.AccentStyle().Render("commit: ")+
			g.theme.TextStyle().Render(g.message)+g.theme.AccentStyle().Render("█"), w, "…")
	}

	left := g.theme.AccentStyle().Bold(true).Render(r.Name) + "  " + g.refStyle(r).Render(r.Ref())
	if !r.Clean() || !r.Synced() {
		left += "  " + g.state(r)
	}

	right := ""
	switch {
	case g.busy:
		right = g.theme.DimStyle().Render("working…")
	case g.status != "":
		right = g.theme.WarnStyle().Render(g.status)
	case r.Upstream != "":
		right = g.theme.FaintStyle().Render(r.Upstream)
	}

	if n := w - lipgloss.Width(left) - lipgloss.Width(right); right != "" && n >= 1 {
		return left + strings.Repeat(" ", n) + right
	}
	return ansi.Truncate(left, w, "…")
}

// filesSection is the changed files, headed by how many there are.
func (g *Git) filesSection(r repos.Repo, w, room int) []string {
	if len(r.Files) == 0 {
		return []string{g.theme.GoodStyle().Render(cleanMark + " nothing to commit")}
	}

	lines := []string{g.section(fmt.Sprintf("changes (%d)", len(r.Files)), w, g.mode == modeRepo, changesHint)}

	rows := max(1, room-1)
	start, end := g.files.Window(rows)
	for i := start; i < end; i++ {
		lines = append(lines, g.fileRow(r.Files[i], i == g.files.Cursor() && g.mode == modeRepo, w))
	}

	// The last visible row gives itself up to say how much is below it,
	// which is the only honest thing to do with a list that does not fit.
	if hidden := len(r.Files) - end; hidden > 0 && len(lines) > 1 {
		lines[len(lines)-1] = g.theme.FaintStyle().Render(fmt.Sprintf("  … %d more", hidden+1))
	}
	return lines
}

// commitsSection is the recent history.
func (g *Git) commitsSection(r repos.Repo, w, room int) []string {
	lines := []string{g.section("recent", w, false, "")}
	switch {
	case g.commitsErr != nil:
		return append(lines, g.theme.FaintStyle().Render(ansi.Truncate(g.commitsErr.Error(), w, "…")))
	case g.commitsPath != r.Path:
		return append(lines, g.theme.FaintStyle().Render("…"))
	case len(g.commits) == 0:
		return append(lines, g.theme.FaintStyle().Render("no commits yet"))
	}

	for _, c := range g.commits {
		if len(lines) > room {
			break
		}
		lines = append(lines, g.commitRow(c, w))
	}
	return lines
}

// commitRow is one commit: when, what was said, and by whom.
//
// The author's room is reserved before the subject is cut, not appended after
// it. Appending is what turns every row into a truncated subject with the name
// lost off the end, which is the worst of both.
func (g *Git) commitRow(c repos.Commit, w int) string {
	author := ""
	room := w - ageWidth - 1
	if w >= authorFrom && c.Author != "" {
		author = humanize.Truncate(c.Author, maxAuthor)
		room -= lipgloss.Width(author) + 1
	}

	line := g.theme.FaintStyle().Render(align(humanize.RelTime(c.When), ageWidth)) + " " +
		g.theme.TextStyle().Render(humanize.Truncate(c.Subject, max(1, room)))
	if author == "" {
		return ansi.Truncate(line, w, "…")
	}
	return pad(line, w-lipgloss.Width(author)-1) + " " + g.theme.FaintStyle().Render(author)
}

// The author column. Below authorFrom the panel spends everything on the
// subject: a name cut to two initials identifies nobody, and the subject is
// what the row is for.
const (
	authorFrom = 56
	maxAuthor  = 12
)

// section is a labelled rule, so the panel reads as two things rather than one
// long list. The label is highlighted when its panel has the cursor, and the
// rule carries that panel's keys — which is the only place the user finds out
// that "c" commits.
func (g *Git) section(label string, w int, active bool, hint string) string {
	style := g.theme.DimStyle()
	if active && g.Focused() {
		style = g.theme.AccentStyle().Bold(true)
	}
	head := style.Render(label)

	tail := ""
	if active && hint != "" && w-lipgloss.Width(head)-lipgloss.Width(hint)-4 >= minRule {
		tail = " " + g.theme.FaintStyle().Render(hint)
	}

	if n := w - lipgloss.Width(head) - lipgloss.Width(tail) - 2; n > 0 {
		return head + " " + g.theme.FaintStyle().Render(strings.Repeat("─", n)) + tail
	}
	return ansi.Truncate(head, w, "…")
}

// minRule is the shortest run of ─ still worth drawing. Below it the label and
// the hint have eaten the rule and the line reads as text, not a heading.
const minRule = 4

// changesHint is the local keys, in the order they get used. It names the
// external tool generically because the configured command is the footer's to
// say, and a rule is not the place for a name that might be forty characters.
const changesHint = "↵ stage · c commit · S stash · g more"

// repoPanel is the detail panel on its own, for a pane too narrow to carry
// both. It is the same content: the list is what the cursor left behind.
func (g *Git) repoPanel(w int) []string {
	if _, ok := g.current(); !ok {
		return []string{g.theme.DimStyle().Render("that repository is gone")}
	}
	return g.detailPanel(w)
}

// fileRow draws one changed file: what git has recorded, what is on disk, and
// the path.
func (g *Git) fileRow(f repos.File, selected bool, w int) string {
	var b strings.Builder

	marker := "  "
	if selected {
		marker = "▸ "
	}
	b.WriteString(g.markerStyle(g.lit(selected, modeRepo)).Render(marker))

	// The two status letters are coloured by column, not as a pair: the
	// first is what is already recorded and the second is what is not, and
	// that difference is the whole reason to look at this list.
	code := f.Status()
	b.WriteString(g.theme.GoodStyle().Render(string(code[0])))
	b.WriteString(g.theme.WarnStyle().Render(string(code[1])))
	b.WriteString("  ")

	path := f.Path
	if f.Orig != "" {
		path = f.Orig + " → " + f.Path
	}
	room := max(0, w-markerWidth-4)
	b.WriteString(g.pathStyle(f).Render(elide(path, room)))
	return b.String()
}

// lit reports whether a selected row should draw a lit cursor: only the panel
// holding it does.
//
// Both panels are on screen at once, and two lit markers would each claim to
// be where the next keystroke lands. Only one of them would be right.
func (g *Git) lit(selected bool, panel mode) bool {
	return selected && g.Focused() && g.mode == panel
}

func (g *Git) markerStyle(lit bool) lipgloss.Style {
	if lit {
		return g.theme.AccentStyle().Bold(true)
	}
	return g.theme.FaintStyle()
}

// pathStyle dims what git is not tracking and flags what is in conflict.
func (g *Git) pathStyle(f repos.File) lipgloss.Style {
	switch {
	case f.Conflicted():
		return g.theme.BadStyle().Bold(true)
	case f.Untracked():
		return g.theme.DimStyle()
	default:
		return g.theme.TextStyle()
	}
}

// elide shortens a path from the left, because the file name at the end of it
// is the part that identifies it.
func elide(path string, w int) string {
	if w <= 1 || lipgloss.Width(path) <= w {
		return path
	}
	r := []rune(path)
	return "…" + string(r[len(r)-(w-1):])
}

// headerLines is how many rows the summary occupies. A pane with only two
// lines spends both on repositories: the summary is context, and context is
// what a small pane gives up first.
func (g *Git) headerLines() int {
	if g.H >= 3 {
		return 1
	}
	return 0
}

// listHeight is how many repository rows fit below the summary.
func (g *Git) listHeight() int { return max(0, g.H-g.headerLines()) }

// summaryLine is the glanceable state of the set: how many repositories there
// are, how many want attention, and how the list is ordered.
//
// The sort is on the line because it is the only place the user finds out that
// "s" does anything. The processes widget makes the same trade.
func (g *Git) summaryLine(list []repos.Repo, w int) string {
	var dirty, behind, failed int
	for _, r := range list {
		switch {
		case r.Err != nil:
			failed++
		case !r.Clean():
			dirty++
		}
		if r.Behind > 0 {
			behind++
		}
	}

	parts := []string{g.theme.TextStyle().Render(fmt.Sprint(len(list))) + g.theme.DimStyle().Render(" repos")}
	if dirty > 0 {
		parts = append(parts, g.theme.WarnStyle().Render(mark(dirtyMark, dirty)+" dirty"))
	}
	if behind > 0 {
		parts = append(parts, g.theme.BadStyle().Render(mark(behindMark, behind)+" behind"))
	}
	if failed > 0 {
		parts = append(parts, g.theme.BadStyle().Render(mark(failMark, failed)))
	}

	left := " " + strings.Join(parts, g.theme.FaintStyle().Render(" · "))
	right := g.theme.FaintStyle().Render(string(g.order))
	if g.only {
		right = g.theme.WarnStyle().Render("interesting") + g.theme.FaintStyle().Render(" · "+string(g.order))
	}

	// The sort is a detail; on a pane too narrow for both it is the half
	// that goes.
	if n := w - lipgloss.Width(left) - lipgloss.Width(right) - 1; n >= 1 {
		return left + strings.Repeat(" ", n) + right
	}
	return ansi.Truncate(left, w, "…")
}

// emptyText explains an empty list, which is either good news or a filter.
func (g *Git) emptyText() string {
	if g.only && len(g.repos) > 0 {
		return fmt.Sprintf("all %d repositories are clean and in sync", len(g.repos))
	}
	if g.scan != "" {
		return "no repositories under " + g.scan
	}
	return "no repositories"
}

// row draws one repository: a marker, its name, where it is, what is going on
// in it, and how long ago that was.
func (g *Git) row(r repos.Repo, selected bool, w int) string {
	var b strings.Builder

	marker := "  "
	if selected {
		marker = "▸ "
	}
	b.WriteString(g.markerStyle(g.lit(selected, modeList)).Render(marker))

	l := layoutFor(w)
	b.WriteString(g.nameStyle(r, selected).Render(pad(humanize.Truncate(r.Name, l.name), l.name)))

	// An unreadable repository spends the rest of the row saying why. The
	// columns would all be zero, which reads as a clean repo rather than as
	// one that could not be looked at.
	if r.Err != nil {
		if room := w - markerWidth - l.name - gap; room > 0 {
			b.WriteString(strings.Repeat(" ", gap))
			b.WriteString(g.theme.BadStyle().Render(ansi.Truncate(r.Err.Error(), room, "…")))
		}
		return b.String()
	}

	if l.ref > 0 {
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(g.refStyle(r).Render(pad(humanize.Truncate(r.Ref(), l.ref), l.ref)))
	}
	if l.state > 0 {
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(pad(ansi.Truncate(g.state(r), l.state, "…"), l.state))
	}
	if l.age > 0 {
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(g.theme.FaintStyle().Render(align(g.age(r), l.age)))
	}
	return b.String()
}

// layout is the column arrangement for the current pane width.
//
// The three right-hand columns are fixed and the name takes what is left,
// within reason. The branch goes first as the pane narrows: it is the widest
// column and the least urgent, where the state is the alarm and the age says
// whether the alarm is stale. What survives to the end is the name and the
// state, which is the pair that answers "is there anything to do here?".
type layout struct {
	name  int
	ref   int
	state int
	age   int
}

// fixed is everything but the name.
func (l layout) fixed() int {
	w := markerWidth
	for _, c := range []int{l.ref, l.state, l.age} {
		if c > 0 {
			w += gap + c
		}
	}
	return w
}

func layoutFor(w int) layout {
	for _, l := range []layout{
		{ref: refWidth, state: stateWidth, age: ageWidth},
		{state: stateWidth, age: ageWidth},
		{state: stateWidth},
	} {
		if name := w - l.fixed(); name >= minName {
			l.name = min(name, maxName)
			return l
		}
	}
	// Nothing else fits. The name alone is still worth drawing.
	return layout{name: max(0, w-markerWidth)}
}

// state is the middle column: what has drifted, and by how much.
func (g *Git) state(r repos.Repo) string {
	parts := make([]string, 0, 3)
	if d := r.Dirty(); d > 0 {
		parts = append(parts, g.theme.WarnStyle().Render(mark(dirtyMark, d)))
	}
	if r.Ahead > 0 {
		parts = append(parts, g.theme.AccentStyle().Render(mark(aheadMark, r.Ahead)))
	}
	if r.Behind > 0 {
		// Behind is the one that costs you something later, so it is
		// the one drawn in the colour that means "look at this".
		parts = append(parts, g.theme.BadStyle().Render(mark(behindMark, r.Behind)))
	}
	if len(parts) == 0 {
		return g.theme.GoodStyle().Render(cleanMark)
	}
	return strings.Join(parts, " ")
}

// age is how long ago the last commit was, or nothing at all in a repository
// that has none.
func (g *Git) age(r repos.Repo) string {
	if r.Last.IsZero() {
		return "—"
	}
	return humanize.RelTime(r.Last)
}

// nameStyle marks the selected row and dims a repository with nothing to say.
func (g *Git) nameStyle(r repos.Repo, selected bool) lipgloss.Style {
	switch {
	case selected && g.Focused():
		return g.theme.AccentStyle().Bold(true)
	case r.Err != nil:
		return g.theme.BadStyle()
	case r.Clean() && r.Synced():
		return g.theme.DimStyle()
	default:
		return g.theme.TextStyle()
	}
}

// refStyle marks a detached HEAD, which is a state worth noticing rather than
// a branch name like any other.
func (g *Git) refStyle(r repos.Repo) lipgloss.Style {
	if r.Branch == "" {
		return g.theme.WarnStyle()
	}
	return g.theme.DimStyle()
}

// strip renders the whole list on one line, for the status bar: the
// repositories that have something to say, in as many as will fit.
func (g *Git) strip(list []repos.Repo) string {
	sep := g.theme.FaintStyle().Render(" │ ")
	out := " "
	shown := 0

	for _, r := range list {
		if r.Err == nil && r.Clean() && r.Synced() {
			continue // a clean repo is not news
		}
		chip := g.theme.TextStyle().Render(r.Name) + " " + g.state(r)
		if r.Err != nil {
			chip = g.theme.BadStyle().Render(r.Name + " " + failMark)
		}
		next := out + chip
		if shown > 0 {
			next = out + sep + chip
		}
		if lipgloss.Width(next) > g.W {
			break
		}
		out, shown = next, shown+1
	}

	if shown == 0 {
		return g.theme.DimStyle().Render(fmt.Sprintf(" %s %d repos clean", cleanMark, len(list)))
	}
	return out
}

// Lines is the widget.Liner contract: how tall this widget's content wants to
// be.
//
// A widget offered a single line takes it and renders the strip — which is how
// the status bar gets a one-line summary without the dashboard having to say
// so. Anywhere taller, a list wants a line per repository.
func (g *Git) Lines(int) int {
	if g.H <= 1 {
		return 1
	}
	// With both panels drawn, the taller one sets the height.
	n := max(1, len(g.visible())) + g.headerLines()
	if _, dw := g.split(); dw > 0 || g.mode == modeRepo {
		if r, ok := g.target(); ok {
			// header, the changes rule and its files, a blank, the
			// recent rule and its commits.
			detail := 2 + len(r.Files) + 2 + len(g.commits)
			if _, split := g.split(); split > 0 {
				return max(n, detail)
			}
			return detail
		}
	}
	return n
}

// pad right-fills to w display cells.
func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// align right-justifies to w display cells, which is what a column of ages
// needs to read as a column.
func align(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}
