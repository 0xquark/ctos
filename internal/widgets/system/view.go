package system

import (
	"fmt"
	"math"
	"strings"

	"github.com/0xquark/ctos/internal/humanize"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The pieces of a row, in cells. Everything except the bar and the detail is
// fixed; those two share whatever is left.
const (
	indent     = 1  // the leading space every ctOS widget body starts with
	gap        = 1  // between columns
	sparkWidth = 8  // enough history to show a direction, not a trend
	valueWidth = 5  // "100%" and "12.45" both fit
	minBar     = 6  // narrower than this a bar says nothing a number doesn't
	maxBar     = 16 // wider than this the bar just crowds out the detail
	minDetail  = 9  // "1.9G/3.0G" is the longest thing worth half-printing
	minLabel   = 4  // "swap", "load"
	maxLabel   = 10 // a long mount path is truncated rather than eating the row
	barFull    = '█'
	barEmpty   = '░'
	sparkScale = 100 // percentages fill the sparkline against a fixed ceiling
)

// layout is the column arrangement for the current pane width. Columns are
// dropped in order of what a glance can spare: the detail text first, then the
// history, then the bar itself, leaving the label and the number.
type layout struct {
	label  int
	spark  int
	bar    int
	value  int
	detail int
}

func newLayout(w, labelW int, history bool) layout {
	l := layout{label: labelW, value: valueWidth}
	if history {
		l.spark = sparkWidth
	}

	for {
		fixed := indent + l.label + gap + l.value
		if l.spark > 0 {
			fixed += l.spark + gap
		}
		room := w - fixed - gap // the gap before the bar

		switch {
		case room >= minBar+gap+minDetail:
			l.bar = min(maxBar, room-gap-minDetail)
			l.detail = room - l.bar - gap
			return l
		case room >= minBar:
			l.bar, l.detail = room, 0
			return l
		case l.spark > 0:
			l.spark = 0
		default:
			// Nothing left to drop. The label and the number shrink
			// rather than overflow: a row wider than the pane is
			// truncated by the frame, which would silently eat the
			// right-hand column of every row at once.
			l.bar, l.detail = 0, 0
			l.label = min(l.label, max(0, w-indent))
			if room := w - indent - l.label - gap; room > 0 {
				l.value = min(l.value, room)
			} else {
				l.value = 0
			}
			return l
		}
	}
}

// row is one rendered metric.
type row struct {
	label  string
	pct    float64 // the bar's fill, or -1 for a row that has no bar
	value  string
	detail string
	spark  string
	style  lipgloss.Style

	// sparkKey and sparkTop are what "spark" was rendered from, so a pane
	// with width going spare can draw the same history wider.
	sparkKey string
	sparkTop float64

	// span rows put their text where the bar, value and detail would go.
	// Throughput and uptime have no ceiling to draw a bar against, so the
	// space is better spent on the numbers.
	span string
}

// View renders the vitals.
func (s *System) View() string {
	switch {
	case s.err != nil:
		return s.theme.BadStyle().Render("⚠ " + s.err.Error())
	case s.loading:
		return s.theme.DimStyle().Render("reading system statistics…")
	case s.H <= 0 || s.W <= 0:
		return ""
	}

	if s.resolved() == styleBar {
		return s.barView()
	}

	rows := s.rows()
	if len(rows) == 0 {
		return ""
	}

	// A pane too short for a row each packs the same numbers onto fewer
	// lines rather than hiding the metrics that did not fit.
	if len(rows) > s.H {
		return s.compact(rows)
	}

	l := newLayout(s.W, labelWidth(rows), s.history)

	// A pane wide enough for the flat row draws it and stops. The height it
	// does not use is the frame's to leave blank: a table with its rows
	// pushed apart is harder to read than a table, and a grid pane has
	// width to spend, which is the trade the flat layout is built for.
	if !s.tall(len(rows), l) {
		lines := make([]string, 0, len(rows))
		for _, r := range rows {
			lines = append(lines, s.render(r, l))
		}
		return strings.Join(lines, "\n")
	}
	return s.stacked(rows)
}

// tall reports whether the pane is the shape a status bar down the side of the
// screen produces: too narrow for the flat row to carry its detail, and tall
// enough to spend more than one line on a metric.
//
// That shape is the opposite trade from a pane in the grid — height to spend
// and no width — so it gets the opposite layout.
func (s *System) tall(n int, l layout) bool {
	return l.detail == 0 && s.H >= 2*n
}

// stacked draws each metric down the pane instead of across it.
//
// The blocks are packed from the top at the largest size that fits, with a
// single blank line between them. Height left over stays at the bottom in one
// piece: a column is almost always taller than seven vitals need, and opening
// that up as a gap between every metric pushes apart values that are read
// together — the panel stops looking like one thing.
func (s *System) stacked(rows []row) string {
	// Degrade the block before the separator: the detail line is worth more
	// than the space around it. Four lines is the fullest form, where the
	// history gets a line of its own; below that it folds back beside the
	// detail, then the detail goes, then the bar.
	for _, lines := range []int{4, 3, 2, 1} {
		for _, sep := range []int{1, 0} {
			blocks := make([][]string, len(rows))
			total := sep * (len(rows) - 1)
			for i, r := range rows {
				blocks[i] = s.block(r, lines)
				total += len(blocks[i])
			}
			if total <= s.H || (lines == 1 && sep == 0) {
				return packed(blocks, sep)
			}
		}
	}
	return ""
}

// packed joins blocks top to bottom with sep blank lines between them.
func packed(blocks [][]string, sep int) string {
	var out []string
	for i, block := range blocks {
		if i > 0 {
			for range sep {
				out = append(out, "")
			}
		}
		out = append(out, block...)
	}
	return strings.Join(out, "\n")
}

// block renders one metric across up to h lines: the label with its number,
// the bar at full width, then the history and the detail.
//
// Every line is cut to the pane on the way out. The pieces size themselves as
// well, but a block is assembled from several of them and one backstop is
// cheaper than trusting all of them at every width a column can be given.
func (s *System) block(r row, h int) []string {
	out := s.blockLines(r, h)
	for i, line := range out {
		out[i] = ansi.Truncate(line, s.W, "…")
	}
	return out
}

func (s *System) blockLines(r row, h int) []string {
	lead := strings.Repeat(" ", indent)
	inner := max(0, s.W-indent)
	label := s.theme.DimStyle().Render(humanize.Truncate(r.label, inner))

	// A span row has no magnitude to draw a bar against, so its text is the
	// whole row. It takes the line below the label only when it will not
	// fit beside it — which is what makes "↓ 640B/s  ↑ 738B/s" survive here
	// where the flat row had to cut it to "↑ 738…".
	if r.span != "" {
		if w := lipgloss.Width(r.span); h < 2 || w+lipgloss.Width(label)+gap <= inner {
			return []string{lead + label + strings.Repeat(" ", gap) +
				ansi.Truncate(r.span, max(0, inner-lipgloss.Width(label)-gap), "…")}
		}
		return []string{
			lead + spread(label, s.theme.FaintStyle().Render(r.spark), inner),
			lead + ansi.Truncate(r.span, inner, "…"),
		}
	}

	out := []string{lead + spread(label, r.style.Render(r.value), inner)}
	if h >= 2 {
		out = append(out, lead+s.bar(r.pct, inner, r.style))
	}

	// With four lines the history gets one of its own, at the width of the
	// bar above it. Eight cells show a direction; a full line shows the
	// shape of the last few minutes, which is the thing a column has the
	// room to say and a grid row does not.
	if h >= 4 && r.sparkKey != "" {
		if wide := s.sparkAt(r.sparkKey, r.sparkTop, inner); strings.TrimSpace(wide) != "" {
			out = append(out, lead+s.theme.FaintStyle().Render(wide))
			if r.detail != "" {
				out = append(out, lead+s.theme.FaintStyle().Render(humanize.Truncate(r.detail, inner)))
			}
			return out
		}
	}

	if h >= 3 {
		if tail := s.footnote(r, inner); tail != "" {
			out = append(out, lead+tail)
		}
	}
	return out
}

// footnote is the third line of a block: what the flat row would have put in
// its detail column, with the history at the far end.
//
// The history goes on the right because a sparkline is padded from the left —
// the newest sample is always the last cell, so a series that has not filled
// yet grows towards the reader rather than shifting under them. Put it first
// and every detail on the panel would start at a different column while the
// widget warmed up.
func (s *System) footnote(r row, w int) string {
	detail, spark := r.detail, r.spark
	switch {
	case detail == "" && spark == "":
		return ""
	case spark == "":
		return s.theme.FaintStyle().Render(humanize.Truncate(detail, w))
	case detail == "":
		return s.theme.FaintStyle().Render(spark)
	}
	faint := s.theme.FaintStyle()
	return spread(faint.Render(humanize.Truncate(detail, max(0, w-sparkWidth-gap))), faint.Render(spark), w)
}

// spread puts left at one end of w cells and right at the other, keeping at
// least one space between them.
func spread(left, right string, w int) string {
	n := max(1, w-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", n) + right
}

// labelWidth sizes the label column to the widest label actually present, so
// a dashboard with no disk rows does not reserve room for a mount path.
func labelWidth(rows []row) int {
	w := minLabel
	for _, r := range rows {
		w = max(w, len([]rune(r.label)))
	}
	return min(w, maxLabel)
}

// render draws one row into the column layout.
func (s *System) render(r row, l layout) string {
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", indent))
	b.WriteString(s.theme.DimStyle().Render(pad(humanize.Truncate(r.label, l.label), l.label)))

	if l.spark > 0 {
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(s.theme.FaintStyle().Render(pad(r.spark, l.spark)))
	}

	// A span row gets the bar, value and detail columns as one field.
	if r.span != "" {
		width := l.bar + l.value + l.detail
		if l.bar > 0 {
			width += gap
		}
		if l.detail > 0 {
			width += gap
		}
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(ansi.Truncate(r.span, width, "…"))
		return b.String()
	}

	if l.bar > 0 {
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(s.bar(r.pct, l.bar, r.style))
	}

	if l.value > 0 {
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(r.style.Render(align(humanize.Truncate(r.value, l.value), l.value)))
	}

	if l.detail > 0 && r.detail != "" {
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(s.theme.FaintStyle().Render(humanize.Truncate(r.detail, l.detail)))
	}
	return b.String()
}

// bar draws a proportional bar, filled in the level's own colour.
func (s *System) bar(pct float64, w int, style lipgloss.Style) string {
	if w <= 0 {
		return ""
	}
	filled := int(math.Round(pct / 100 * float64(w)))
	filled = min(max(filled, 0), w)
	// A reading that is small but not zero always shows one cell, so that
	// "3%" is visibly not idle.
	if filled == 0 && pct > 0 {
		filled = 1
	}
	return style.Render(strings.Repeat(string(barFull), filled)) +
		s.theme.FaintStyle().Render(strings.Repeat(string(barEmpty), w-filled))
}

// compact packs the rows onto fewer lines than there are metrics, for a pane
// used as a header strip rather than a panel.
func (s *System) compact(rows []row) string {
	chips := make([]string, 0, len(rows))
	for _, r := range rows {
		text := r.span
		if text == "" {
			text = r.label + " " + r.style.Render(strings.TrimSpace(r.value))
		}
		chips = append(chips, text)
	}

	sep := s.theme.FaintStyle().Render(" · ")
	var lines []string
	line := ""
	for _, chip := range chips {
		next := chip
		if line != "" {
			next = line + sep + chip
		}
		if lipgloss.Width(next)+indent > s.W && line != "" {
			lines = append(lines, " "+line)
			if len(lines) == s.H {
				return strings.Join(lines, "\n")
			}
			line = chip
			continue
		}
		line = next
	}
	if line != "" && len(lines) < s.H {
		lines = append(lines, " "+line)
	}
	return strings.Join(lines, "\n")
}

// rows builds one row per configured metric, skipping any the platform did
// not answer for.
func (s *System) rows() []row {
	var out []row
	for _, m := range s.metricList() {
		switch m {
		case metricCPU:
			out = append(out, s.cpuRow()...)
		case metricMem:
			out = append(out, s.memRow()...)
		case metricSwap:
			out = append(out, s.swapRow()...)
		case metricDisk:
			out = append(out, s.diskRows()...)
		case metricNet:
			out = append(out, s.netRow()...)
		case metricLoad:
			out = append(out, s.loadRow()...)
		case metricUptime:
			out = append(out, s.uptimeRow()...)
		}
	}
	return out
}

func (s *System) cpuRow() []row {
	c := s.stats.CPU
	if !c.OK {
		return nil
	}
	return []row{{
		label:    "cpu",
		pct:      c.Busy,
		value:    pct(c.Busy),
		detail:   fmt.Sprintf("%.0f us · %.0f sy", c.User, c.System),
		spark:    s.sparkline(string(metricCPU), sparkScale),
		sparkKey: string(metricCPU),
		sparkTop: sparkScale,
		style:    s.level(c.Busy, 70, 90),
	}}
}

func (s *System) memRow() []row {
	m := s.stats.Mem
	if !m.OK {
		return nil
	}
	return []row{{
		label:    "mem",
		pct:      m.Percent(),
		value:    pct(m.Percent()),
		detail:   humanize.Bytes(m.Used) + "/" + humanize.Bytes(m.Total),
		spark:    s.sparkline(string(metricMem), sparkScale),
		sparkKey: string(metricMem),
		sparkTop: sparkScale,
		style:    s.level(m.Percent(), 75, 90),
	}}
}

func (s *System) swapRow() []row {
	w := s.stats.Swap
	if !w.OK {
		return nil
	}
	// Swap that is switched off is a row worth keeping: its absence is
	// what explains the machine's behaviour under pressure.
	if w.Total == 0 {
		return []row{{label: "swap", pct: 0, value: "off", style: s.theme.DimStyle()}}
	}
	// Any swap in use at all is worth noticing, so the thresholds sit
	// lower than memory's: paging is a symptom before it is a problem.
	return []row{{
		label:  "swap",
		pct:    w.Percent(),
		value:  pct(w.Percent()),
		detail: humanize.Bytes(w.Used) + "/" + humanize.Bytes(w.Total),
		style:  s.level(w.Percent(), 50, 80),
	}}
}

func (s *System) diskRows() []row {
	out := make([]row, 0, len(s.stats.Disks))
	for _, d := range s.stats.Disks {
		if !d.OK {
			continue
		}
		out = append(out, row{
			label:  d.Path,
			pct:    d.Percent(),
			value:  pct(d.Percent()),
			detail: humanize.Bytes(d.Avail) + " free",
			style:  s.level(d.Percent(), 80, 92),
		})
	}
	return out
}

func (s *System) netRow() []row {
	n := s.stats.Net
	if !n.OK {
		// Throughput is a difference between two samples, so the first
		// tick has nothing to show yet. Saying so beats a blank row
		// that looks like an interface with no traffic.
		return []row{{label: "net", span: s.theme.FaintStyle().Render("…")}}
	}

	down := s.theme.TextStyle().Render(humanize.Rate(n.Rx))
	up := s.theme.TextStyle().Render(humanize.Rate(n.Tx))
	arrows := s.theme.DimStyle()
	return []row{{
		label: "net",
		spark: s.sparkline(netRxKey, 0),
		span:  arrows.Render("↓ ") + down + arrows.Render("  ↑ ") + up,
	}}
}

func (s *System) loadRow() []row {
	l := s.stats.Load
	if !l.OK {
		return nil
	}
	cores := max(s.stats.Cores, 1)

	// The bar is load against core count: one runnable task per core is a
	// machine at capacity, which is where the bar should read full.
	per := l.One / float64(cores) * 100
	return []row{{
		label:    "load",
		pct:      per,
		value:    fmt.Sprintf("%.2f", l.One),
		detail:   fmt.Sprintf("%.2f %.2f · %d cores", l.Five, l.Fifteen, cores),
		spark:    s.sparkline(string(metricLoad), sparkScale),
		sparkKey: string(metricLoad),
		sparkTop: sparkScale,
		style:    s.level(per, 100, 200),
	}}
}

func (s *System) uptimeRow() []row {
	if s.stats.Uptime <= 0 {
		return nil
	}
	span := s.theme.TextStyle().Render(humanize.Duration(s.stats.Uptime))
	if s.stats.Host != "" {
		span += s.theme.FaintStyle().Render(" · " + s.stats.Host)
	}
	return []row{{label: "up", span: span}}
}

// sparkline renders one metric's history at the width of the flat row's
// history column, or blanks when history is off or the metric has not been
// recorded yet.
func (s *System) sparkline(key string, scale float64) string {
	return s.sparkAt(key, scale, sparkWidth)
}

// sparkAt renders one metric's history at a given width.
func (s *System) sparkAt(key string, scale float64, w int) string {
	if !s.history || w <= 0 {
		return ""
	}
	series, ok := s.hist[key]
	if !ok {
		return ""
	}
	return series.Render(w, scale)
}

// level colours a reading against a warning and a critical threshold. It is
// the same judgement the processes widget makes about a load average, kept
// here as one function so every bar agrees on what "busy" looks like.
func (s *System) level(v, warn, bad float64) lipgloss.Style {
	switch {
	case v >= bad:
		return s.theme.BadStyle().Bold(true)
	case v >= warn:
		return s.theme.WarnStyle()
	default:
		return s.theme.GoodStyle()
	}
}

func pct(v float64) string { return fmt.Sprintf("%.0f%%", v) }

// pad right-fills to w display cells.
func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// align right-justifies to w display cells, which is what a column of
// percentages needs to read as a column.
func align(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}
