package processes

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/0xquark/ctos/internal/humanize"
	"github.com/0xquark/ctos/internal/procs"
	"github.com/charmbracelet/lipgloss"
)

// colKey identifies a column so the responsive logic can drop one by name.
type colKey int

const (
	colPID colKey = iota
	colUser
	colCPU
	colMem
	colRSS
	colTime

	// colCommand is not in allColumns — it is always present and always
	// takes the leftover width — but sorting by name marks it, so it needs
	// a key.
	colCommand
)

type column struct {
	key   colKey
	head  string
	width int
	right bool // right-align the value under the header
}

// allColumns is the full table, in display order. COMMAND is not listed: it
// is always present and always takes whatever width is left.
var allColumns = []column{
	{colPID, "PID", 6, true},
	{colUser, "USER", 9, false},
	{colCPU, "CPU%", 6, true},
	{colMem, "MEM%", 5, true},
	{colRSS, "RSS", 6, true},
	{colTime, "TIME", 8, true},
}

// dropOrder is which column goes first as the pane narrows: the least
// diagnostic ones. CPU% is absent because a process table without it is
// pointless, so it is never dropped.
var dropOrder = []colKey{colRSS, colTime, colUser, colPID, colMem}

// minCommand is the narrowest command column worth rendering. It is set high
// enough that a half-width pane drops a column rather than squeezing the one
// field the user is actually reading down to an ellipsis.
const minCommand = 16

// markerWidth is the display width of the selection marker, in cells.
const markerWidth = 2

// gauge renders a magnitude as a single cell, which is the only bar that fits
// beside a number in a half-width pane.
var gauge = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// columns picks the widest set of columns that fits, dropping by dropOrder.
func columns(width int) []column {
	keep := map[colKey]bool{}
	for _, c := range allColumns {
		keep[c.key] = true
	}

	for _, drop := range append([]colKey{}, dropOrder...) {
		if fits(keep, width) {
			break
		}
		delete(keep, drop)
	}

	out := make([]column, 0, len(allColumns))
	for _, c := range allColumns {
		if keep[c.key] {
			out = append(out, c)
		}
	}
	return out
}

// fits reports whether the kept columns leave room for a usable command.
func fits(keep map[colKey]bool, width int) bool {
	used := markerWidth
	for _, c := range allColumns {
		if keep[c.key] {
			used += c.width + 1 // one space between columns
		}
	}
	return width-used >= minCommand
}

// View renders the summary line, the column headers and the visible rows.
func (p *Processes) View() string {
	switch {
	case p.err != nil && len(p.all) == 0:
		return p.theme.BadStyle().Render("⚠ " + p.err.Error())
	case p.loading && len(p.all) == 0:
		return p.theme.DimStyle().Render("reading process table…")
	case len(p.all) == 0:
		return p.theme.DimStyle().Render("no processes")
	}

	var b strings.Builder
	b.WriteString(p.headerLine())

	cols := columns(p.W)
	if p.headerLines() == 2 {
		b.WriteByte('\n')
		b.WriteString(p.columnHeader(cols))
	}

	visible := p.listHeight()
	if visible == 0 {
		// A one-line pane gets the summary and nothing else.
		return b.String()
	}

	if len(p.rows) == 0 {
		b.WriteByte('\n')
		b.WriteString(p.theme.DimStyle().Render("  nothing matches " + strconv.Quote(p.query)))
		return b.String()
	}

	start, end := p.list.Window(visible)
	for i := start; i < end; i++ {
		b.WriteByte('\n')
		b.WriteString(p.row(p.rows[i], cols, i == p.list.Cursor()))
	}

	if h := p.detailHeight(); h > 0 {
		b.WriteByte('\n')
		b.WriteString(p.rule())
		for _, line := range p.detailLines(h) {
			b.WriteByte('\n')
			b.WriteString(line)
		}
	}
	return b.String()
}

// rule separates the list from the detail pane, the same way the notes widget
// separates its list from the preview.
func (p *Processes) rule() string {
	return p.theme.FaintStyle().Render(strings.Repeat("─", max(0, p.W)))
}

// detailHeight is how many rows the detail pane gets, excluding its rule.
// Zero means the pane is closed or the widget is too short to split.
func (p *Processes) detailHeight() int {
	if !p.showDetail || p.H < minDetailHeight {
		return 0
	}

	// Everything below the summary and column headers, less the rule.
	body := p.H - p.headerLines() - 1
	if body < 4 {
		return 0
	}

	h := p.cfg.DetailLines
	if h <= 0 {
		h = body / 2
	}
	// Always leave the list at least three rows; a detail pane with no list
	// above it is not a process table.
	return min(h, body-3)
}

// headerLines is how many rows the summary and column headers occupy. The
// column headers are the first thing dropped when the pane is short.
func (p *Processes) headerLines() int {
	if p.H >= 3 {
		return 2
	}
	return 1
}

// listHeight is how many process rows fit between the header lines and the
// detail pane. It can be zero: a one-line pane has room for the summary only.
func (p *Processes) listHeight() int {
	h := p.H - p.headerLines()
	if d := p.detailHeight(); d > 0 {
		h -= d + 1 // the pane plus its rule
	}
	return max(0, h)
}

// headerLine is the top line: a kill prompt, a filter box, a transient status,
// or the system summary, in that order of urgency.
func (p *Processes) headerLine() string {
	switch {
	case p.confirm != nil:
		return p.confirmLine()
	case p.typing:
		return p.filterLine()
	case p.status != "":
		return truncate(p.theme.WarnStyle().Render(" "+p.status), p.W)
	default:
		return p.summaryLine()
	}
}

func (p *Processes) confirmLine() string {
	c := p.confirm
	prompt := fmt.Sprintf(" kill %s (%d)?", c.Name(), c.PID)
	keys := p.theme.DimStyle().Render("  ") +
		p.theme.AccentStyle().Render("↵") + p.theme.DimStyle().Render(" term  ") +
		p.theme.AccentStyle().Render("k") + p.theme.DimStyle().Render(" kill  ") +
		p.theme.AccentStyle().Render("esc") + p.theme.DimStyle().Render(" cancel")

	line := p.theme.BadStyle().Bold(true).Render(prompt) + keys
	if lipgloss.Width(line) > p.W {
		return truncate(p.theme.BadStyle().Bold(true).Render(prompt), p.W)
	}
	return line
}

func (p *Processes) filterLine() string {
	label := p.theme.AccentStyle().Render(" /")
	// A block cursor makes it obvious the widget is swallowing keystrokes.
	return truncate(label+p.theme.TextStyle().Render(p.query)+p.theme.AccentStyle().Render("█"), p.W)
}

// summaryLine is the glanceable state of the machine: load, process count and
// the current sort.
func (p *Processes) summaryLine() string {
	var parts []string

	if p.load.OK {
		parts = append(parts,
			p.theme.DimStyle().Render("load ")+
				p.loadStyle(p.load.One).Render(fmt.Sprintf("%.2f", p.load.One))+
				p.theme.FaintStyle().Render(fmt.Sprintf(" %.2f %.2f", p.load.Five, p.load.Fifteen)))
	}

	count := fmt.Sprintf("%d", len(p.rows))
	if len(p.rows) != len(p.all) {
		count = fmt.Sprintf("%d/%d", len(p.rows), len(p.all))
	}
	procsPart := p.theme.TextStyle().Render(count) + p.theme.DimStyle().Render(" procs")
	if n := running(p.rows); n > 0 {
		procsPart += p.theme.GoodStyle().Render(fmt.Sprintf(" %d", n)) + p.theme.DimStyle().Render(" running")
	}
	parts = append(parts, procsPart)

	if p.query != "" {
		parts = append(parts, p.theme.AccentStyle().Render("/"+p.query))
	}
	parts = append(parts, p.theme.DimStyle().Render("sort ")+p.theme.AccentStyle().Render(p.sort.String()))

	sep := p.theme.FaintStyle().Render(" · ")
	for n := len(parts); n > 0; n-- {
		line := " " + strings.Join(parts[:n], sep)
		if lipgloss.Width(line) <= p.W {
			return line
		}
	}
	return ""
}

// loadStyle judges a load average against the core count: one runnable task
// per core is fine, twice that is not.
func (p *Processes) loadStyle(load float64) lipgloss.Style {
	per := load / float64(procs.CPUs())
	switch {
	case per >= 2:
		return p.theme.BadStyle().Bold(true)
	case per >= 1:
		return p.theme.WarnStyle()
	default:
		return p.theme.GoodStyle()
	}
}

// sortColumn maps a sort order onto the column that shows it, so the header
// can mark which one is active.
func sortColumn(by procs.Sort) colKey {
	switch by {
	case procs.ByMem:
		return colMem
	case procs.ByPID:
		return colPID
	case procs.ByName:
		return colCommand
	default:
		return colCPU
	}
}

// columnHeader labels the columns and marks the sorted one with an arrow
// pointing the way the values actually run.
func (p *Processes) columnHeader(cols []column) string {
	active := sortColumn(p.sort)
	arrow := "↑"
	if p.sort.Descending(p.reversed) {
		arrow = "↓"
	}

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", markerWidth))

	for _, c := range cols {
		head := c.head
		style := p.theme.FaintStyle()
		if c.key == active {
			head += arrow
			style = p.theme.AccentStyle()
		}
		b.WriteString(style.Render(align(head, c.width, c.right)))
		b.WriteByte(' ')
	}

	command, style := "COMMAND", p.theme.FaintStyle()
	if active == colCommand {
		command += arrow
		style = p.theme.AccentStyle()
	}
	b.WriteString(style.Render(command))

	return truncate(b.String(), p.W)
}

// row renders one process across the chosen columns.
func (p *Processes) row(proc procs.Process, cols []column, selected bool) string {
	marker := "  "
	if selected {
		marker = "▸ "
	}

	var b strings.Builder
	if selected && p.Focused() {
		b.WriteString(p.theme.AccentStyle().Bold(true).Render(marker))
	} else {
		b.WriteString(p.theme.FaintStyle().Render(marker))
	}

	used := markerWidth
	for _, c := range cols {
		text, style := p.cell(proc, c)
		b.WriteString(style.Render(align(text, c.width, c.right)))
		b.WriteByte(' ')
		used += c.width + 1
	}

	name := commandText(proc, p.W-used)
	b.WriteString(p.commandStyle(selected).Render(name))
	return b.String()
}

// cell formats one column's value and picks its colour.
func (p *Processes) cell(proc procs.Process, c column) (string, lipgloss.Style) {
	switch c.key {
	case colPID:
		return strconv.Itoa(proc.PID), p.theme.DimStyle()
	case colUser:
		return humanize.Truncate(proc.User, c.width), p.theme.DimStyle()
	case colCPU:
		// The gauge is pinned to the column's left edge and the number is
		// right-aligned beside it, so the bar reads as a bar chart rather
		// than sliding as digits come and go.
		return string(gaugeFor(proc.CPU)) + align(pct(proc.CPU), c.width-1, true), p.usageStyle(proc.CPU, 50, 20)
	case colMem:
		return pct(proc.Mem), p.usageStyle(proc.Mem, 20, 5)
	case colRSS:
		return humanize.Bytes(proc.RSS), p.theme.DimStyle()
	case colTime:
		return shortDuration(proc.Elapsed), p.theme.FaintStyle()
	default:
		return "", p.theme.TextStyle()
	}
}

// usageStyle grades a percentage: quiet when idle, loud when it is eating the
// machine.
func (p *Processes) usageStyle(v, high, mid float64) lipgloss.Style {
	switch {
	case v >= high:
		return p.theme.BadStyle().Bold(true)
	case v >= mid:
		return p.theme.WarnStyle()
	case v > 0:
		return p.theme.TextStyle()
	default:
		return p.theme.FaintStyle()
	}
}

func (p *Processes) commandStyle(selected bool) lipgloss.Style {
	if !selected {
		return p.theme.TextStyle()
	}
	if p.Focused() {
		return p.theme.AccentStyle().Bold(true)
	}
	return p.theme.TextStyle().Bold(true)
}

// commandText shows the short program name, and appends its arguments only
// when there is room left over.
func commandText(proc procs.Process, width int) string {
	if width < 1 {
		return ""
	}
	name := proc.Name()
	if len(name)+2 >= width {
		return humanize.Truncate(name, width)
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(proc.Command), argv0(proc.Command)))
	if rest == "" {
		return name
	}
	return humanize.Truncate(name+" "+rest, width)
}

func argv0(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if i := strings.IndexByte(cmd, ' '); i > 0 {
		return cmd[:i]
	}
	return cmd
}

// pct formats a percentage in at most five cells: "100.0" down to "  0.0".
func pct(v float64) string {
	if v >= 100 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// gaugeFor maps a CPU percentage onto one block character.
func gaugeFor(v float64) rune {
	switch {
	case v <= 0:
		return gauge[0]
	case v >= 100:
		return gauge[len(gauge)-1]
	}
	i := int(v/100*float64(len(gauge)-1)) + 1
	return gauge[min(i, len(gauge)-1)]
}

// shortDuration renders elapsed time compactly: "3d04h", "2:05:11", "05:11".
func shortDuration(d time.Duration) string {
	if d < 0 {
		return "-"
	}
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%02d:%02d", int(d.Minutes()), int(d.Seconds())%60)
	}
}

// align pads text to width, left or right.
func align(s string, width int, right bool) string {
	s = humanize.Truncate(s, width)
	pad := width - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", pad) + s
	}
	return s + strings.Repeat(" ", pad)
}

// truncate cuts a already-styled line to width display cells.
func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// running counts processes the kernel currently has on a CPU. ps spells that
// state "R", optionally with modifiers like "R+" or "Rl".
func running(ps []procs.Process) int {
	n := 0
	for _, p := range ps {
		if strings.HasPrefix(p.State, "R") {
			n++
		}
	}
	return n
}
