package system

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/0xquark/ctos/internal/humanize"
	"github.com/0xquark/ctos/internal/procs"
	"github.com/0xquark/ctos/internal/sysinfo"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The status bar is one line, always. A strip that wraps is no longer a strip:
// it pushes the dashboard down as the machine gets busier, and the second line
// is read as a continuation rather than at a glance.
//
// Fitting a machine's vitals into one line of unknown width is therefore the
// whole problem, and it is solved the way the process table solves its columns
// (ADR-028): every value knows how to render itself at several levels of
// detail and carries a priority, and the bar gives up detail on its least
// important values before it gives up any value at all.

// Chip priorities. Higher survives longer; the three at the top are what the
// bar exists to say and are never dropped in practice.
const (
	prioCPU    = 100
	prioMem    = 100
	prioLoad   = 90
	prioDisk   = 80
	prioSwap   = 70
	prioTopCPU = 60
	prioNet    = 50
	prioDiskIO = 40
	prioTopMem = 30

	// The breakdown ranks low because the memory bar already carries its
	// shape; the numbers are the precise version of something the reader
	// can already see.
	prioMemPart = 25

	// Uptime is last on purpose. It is the one value on the bar that
	// never tells you to do anything.
	prioUptime = 20
)

// memBarWidth is how many cells the memory segment bar gets. Eight is enough
// for each category to be visible at a glance and small enough to survive the
// fit on a narrow terminal.
const memBarWidth = 8

// deltaFloors are the smallest movements worth an arrow, per unit. A machine
// at rest jitters below these every tick.
const (
	floorPercent = 0.5  // cpu, mem, swap, disk
	floorLoad    = 0.05 // load average
)

// chip is one value on the strip, in every form it knows how to take.
//
// forms run widest to narrowest and are pre-rendered, styles and all, because
// the fit is decided by measuring them. A chip always has at least one form,
// and dropping it entirely is the step after its last one.
type chip struct {
	prio  int
	forms []string
}

// barView renders the strip onto exactly one line.
func (s *System) barView() string {
	chips := s.chips()
	if len(chips) == 0 {
		return ""
	}
	sep := s.theme.FaintStyle().Render(" │ ")
	return " " + fitOneLine(chips, sep, max(0, s.W-indent))
}

// Lines is the widget.Liner contract. The strip is one line by definition, so
// there is nothing to measure.
func (s *System) Lines(int) int {
	if s.style != styleBar {
		return s.H
	}
	return 1
}

// fitOneLine renders as much of chips as fits in w cells on a single line.
//
// It works from the least important value up, and takes a whole tier apart
// before touching the next one: the lowest-priority values shorten, then those
// same values disappear, and only then does anything more important lose a
// digit. The alternative — shortening everything before dropping anything —
// trades a detail the reader wanted for a value they did not, which is how
// "TOP CPU WindowServer 37.6%" ends up as "TOP WindowServer 38%" on a terminal
// with room to spare for both.
func fitOneLine(chips []chip, sep string, w int) string {
	if w <= 0 || len(chips) == 0 {
		return ""
	}

	level := make([]int, len(chips))
	alive := make([]bool, len(chips))
	for i := range alive {
		alive[i] = true
	}

	// Each pass either shortens one chip or removes one, and neither can
	// happen unboundedly, so the loop always terminates.
	for {
		line := joinChips(chips, level, alive, sep)
		if lipgloss.Width(line) <= w {
			return line
		}
		i, drop, ok := nextSacrifice(chips, level, alive)
		if !ok {
			// One value left at its shortest and still too wide:
			// show the start of it rather than an empty bar.
			return ansi.Truncate(line, w, "…")
		}
		if drop {
			alive[i] = false
		} else {
			level[i]++
		}
	}
}

func joinChips(chips []chip, level []int, alive []bool, sep string) string {
	parts := make([]string, 0, len(chips))
	for i, c := range chips {
		if alive[i] {
			parts = append(parts, c.forms[min(level[i], len(c.forms)-1)])
		}
	}
	return strings.Join(parts, sep)
}

// nextSacrifice decides what to give up next: within the least important tier
// still on the bar, shorten the widest value that can shorten, or remove the
// widest if none can. Widest first because it is the change that buys the most
// room for the same loss.
func nextSacrifice(chips []chip, level []int, alive []bool) (idx int, drop, ok bool) {
	tier, remaining := 0, 0
	for i, c := range chips {
		if !alive[i] {
			continue
		}
		if remaining == 0 || c.prio < tier {
			tier = c.prio
		}
		remaining++
	}
	if remaining == 0 {
		return 0, false, false
	}

	if i, found := widestIn(chips, level, alive, tier, true); found {
		return i, false, true
	}
	// The last value standing is never removed: an empty bar says less
	// than a truncated one.
	if remaining <= 1 {
		return 0, false, false
	}
	if i, found := widestIn(chips, level, alive, tier, false); found {
		return i, true, true
	}
	return 0, false, false
}

// widestIn returns the widest live chip in a priority tier, optionally
// restricted to those with a shorter form left to fall back on.
func widestIn(chips []chip, level []int, alive []bool, tier int, shrinkable bool) (int, bool) {
	best, bestW, found := -1, -1, false
	for i, c := range chips {
		if !alive[i] || c.prio != tier {
			continue
		}
		if shrinkable && level[i] >= len(c.forms)-1 {
			continue
		}
		if width := lipgloss.Width(c.forms[level[i]]); width > bestW {
			best, bestW, found = i, width, true
		}
	}
	return best, found
}

// chips builds the strip's contents, one or more per configured metric.
func (s *System) chips() []chip {
	var out []chip
	for _, m := range s.metrics {
		switch m {
		case metricCPU:
			out = append(out, s.cpuChips()...)
		case metricMem:
			out = append(out, s.memChips()...)
		case metricSwap:
			out = append(out, s.swapChips()...)
		case metricDisk:
			out = append(out, s.diskChips()...)
		case metricDiskIO:
			out = append(out, s.diskIOChips()...)
		case metricNet:
			out = append(out, s.netChips()...)
		case metricLoad:
			out = append(out, s.loadChips()...)
		case metricTop:
			out = append(out, s.topChips()...)
		case metricUptime:
			out = append(out, s.uptimeChips()...)
		}
	}
	return out
}

// text composes one form: a dim label, the number that matters in the colour
// its level earned, and any supporting figures a shade back.
//
// The hierarchy is the point. A row of values all the same weight is a wall of
// text; making the headline number the only bright thing in each group is what
// lets the eye cross the whole bar in one pass and stop only where it should.
func (s *System) text(label, primary string, st lipgloss.Style, rest ...string) string {
	out := s.theme.DimStyle().Render(label)
	if primary != "" {
		out += " " + st.Render(primary)
	}
	for _, r := range rest {
		if r != "" {
			out += " " + s.theme.FaintStyle().Render(r)
		}
	}
	return out
}

func (s *System) cpuChips() []chip {
	c := s.stats.CPU
	if !c.OK {
		return nil
	}
	st := s.level(c.Busy, 70, 90)
	d := s.deltaText(string(metricCPU), floorPercent, "%.1f")

	return []chip{{
		prio: prioCPU,
		forms: []string{
			s.text("CPU", fmt.Sprintf("%.1f%%", c.Busy), st) + spaced(d),
			s.text("CPU", fmt.Sprintf("%.1f%%", c.Busy), st),
			s.text("CPU", fmt.Sprintf("%.0f%%", c.Busy), st),
		},
	}}
}

// memChips are the headline figure with a segmented bar, plus the breakdown
// as separate low-priority values.
//
// The bar carries the shape of memory in eight cells where the numbers need
// forty, so it survives a narrow terminal that the numbers do not: even when
// FREE and CACHE and WIRED have all been dropped, the strip still shows how
// much of what is in use cannot be handed back.
func (s *System) memChips() []chip {
	m := s.stats.Mem
	if !m.OK {
		return nil
	}
	st := s.level(m.Percent(), 75, 90)
	pct := fmt.Sprintf("%.0f%%", m.Percent())
	size := fmt.Sprintf("%s/%s", humanize.Size(m.Used), humanize.Size(m.Total))
	bar := s.memBar(m, memBarWidth)
	d := s.deltaText(string(metricMem), floorPercent, "%.1f")

	// The bar sits between the label and the percentage, so the eye reads
	// the shape of memory and then the number that summarises it.
	withBar := s.theme.DimStyle().Render("MEM") + " " + bar + " " + st.Render(pct)

	out := []chip{{
		prio: prioMem,
		forms: []string{
			withBar + " " + s.theme.FaintStyle().Render(size) + spaced(d),
			withBar + " " + s.theme.FaintStyle().Render(size),
			withBar,
			s.text("MEM", pct, st),
		},
	}}

	// The breakdown is one value, not four. Four would spend three extra
	// separators on the least important thing on the bar, and would let
	// the fit leave the reader with WIRED but not COMP — half a breakdown,
	// which is worse than none.
	parts := memParts(m)
	if len(parts) == 0 {
		return out
	}

	// The shortest form keeps the two largest parts, which are the two
	// doing the most to explain the headline percentage.
	biggest := slices.Clone(parts)
	slices.SortStableFunc(biggest, func(a, b memPart) int { return cmp.Compare(b.bytes, a.bytes) })

	out = append(out, chip{
		prio: prioMemPart,
		forms: []string{
			s.renderParts(parts, "  ", humanize.Size),
			s.renderParts(parts, " ", humanize.Bytes),
			s.renderParts(biggest[:min(2, len(biggest))], "  ", humanize.Size),
		},
	})
	return out
}

// memPart is one category of the memory breakdown.
type memPart struct {
	label string
	bytes int64
}

// memParts is the breakdown this platform publishes. A category it does not
// report is left out rather than shown as a zero standing in for a fact.
func memParts(m sysinfo.Memory) []memPart {
	all := []memPart{
		{"free", m.Free},
		{"cache", m.Cached},
		{"wired", m.Wired},
		{"comp", m.Compressed},
	}
	return slices.DeleteFunc(all, func(p memPart) bool { return p.bytes <= 0 })
}

func (s *System) renderParts(parts []memPart, sep string, size func(int64) string) string {
	fields := make([]string, 0, len(parts))
	for _, p := range parts {
		fields = append(fields, s.theme.DimStyle().Render(p.label)+" "+s.theme.TextStyle().Render(size(p.bytes)))
	}
	return strings.Join(fields, sep)
}

// memBar draws memory as one bar divided by category, ordered from what the
// kernel can never hand back to what is already free.
//
// Colour does the labelling: red for wired, amber for compressed, the accent
// for everything else in use, then two dimmer glyphs for cache and free. The
// glyphs differ as well as the colours, so the bar still reads on a terminal
// that is not showing colour at all.
func (s *System) memBar(m sysinfo.Memory, w int) string {
	if w <= 0 || m.Total <= 0 {
		return ""
	}

	cached := min(m.Cached, max(0, m.Total-m.Used))
	segments := []struct {
		size  int64
		glyph string
		style lipgloss.Style
	}{
		{m.Wired, "█", s.theme.BadStyle()},
		{m.Compressed, "█", s.theme.WarnStyle()},
		{max(0, m.Used-m.Wired-m.Compressed), "█", s.theme.AccentStyle()},
		{cached, "▒", s.theme.DimStyle()},
		{max(0, m.Total-m.Used-cached), "░", s.theme.FaintStyle()},
	}

	// Boundaries are rounded cumulatively rather than per segment, so the
	// bar is always exactly w cells however the rounding falls.
	var b strings.Builder
	var acc int64
	filled := 0
	for i, seg := range segments {
		acc += seg.size
		end := int(math.Round(float64(acc) / float64(m.Total) * float64(w)))
		if i == len(segments)-1 {
			end = w
		}
		end = min(max(end, filled), w)
		if n := end - filled; n > 0 {
			b.WriteString(seg.style.Render(strings.Repeat(seg.glyph, n)))
		}
		filled = end
	}
	return b.String()
}

func (s *System) swapChips() []chip {
	w := s.stats.Swap
	if !w.OK {
		return nil
	}
	if w.Total == 0 {
		return []chip{{prio: prioSwap, forms: []string{s.text("SWP", "off", s.theme.DimStyle())}}}
	}

	st := s.level(w.Percent(), 50, 80)
	pct := fmt.Sprintf("%.0f%%", w.Percent())
	return []chip{{
		prio: prioSwap,
		forms: []string{
			s.text("SWP", pct, st, fmt.Sprintf("%s/%s", humanize.Size(w.Used), humanize.Size(w.Total))),
			s.text("SWP", pct, st),
		},
	}}
}

func (s *System) diskChips() []chip {
	out := make([]chip, 0, len(s.stats.Disks))
	for _, d := range s.stats.Disks {
		if !d.OK {
			continue
		}
		st := s.level(d.Percent(), 80, 92)
		pct := fmt.Sprintf("%.0f%%", d.Percent())
		out = append(out, chip{
			prio: prioDisk,
			forms: []string{
				s.text(d.Path, pct, st, humanize.Size(d.Avail)+" free"),
				s.text(d.Path, pct, st, humanize.Bytes(d.Avail)),
				s.text(d.Path, pct, st),
			},
		})
	}
	return out
}

func (s *System) diskIOChips() []chip {
	io := s.stats.DiskIO
	if !io.OK {
		return []chip{{prio: prioDiskIO, forms: []string{s.text("DISK", "…", s.theme.FaintStyle())}}}
	}

	st := s.theme.TextStyle()
	// macOS reports one combined figure, so claiming a direction it does
	// not know would be a lie the reader cannot see through.
	if !io.Split {
		return []chip{{
			prio:  prioDiskIO,
			forms: []string{s.text("DISK", humanize.Rate(io.Total), st)},
		}}
	}
	return []chip{{
		prio: prioDiskIO,
		forms: []string{
			s.text("DISK", "↓"+humanize.Rate(io.Read)+" ↑"+humanize.Rate(io.Write), st),
			s.text("DISK", humanize.Rate(io.Total), st),
		},
	}}
}

func (s *System) netChips() []chip {
	n := s.stats.Net
	if !n.OK {
		return []chip{{prio: prioNet, forms: []string{s.text("NET", "…", s.theme.FaintStyle())}}}
	}
	st := s.theme.TextStyle()
	return []chip{{
		prio: prioNet,
		forms: []string{
			s.text("NET", "↓"+humanize.Rate(n.Rx)+" ↑"+humanize.Rate(n.Tx), st),
			s.text("NET", "↓"+trimRate(humanize.Rate(n.Rx))+" ↑"+trimRate(humanize.Rate(n.Tx)), st),
		},
	}}
}

func (s *System) loadChips() []chip {
	l := s.stats.Load
	if !l.OK {
		return nil
	}
	cores := max(s.stats.Cores, 1)
	per := l.One / float64(cores) * 100
	st := s.level(per, 100, 200)

	// Only the one-minute figure is the headline: the other two are there
	// to say whether it is a spike or a trend, which is a second glance.
	one := fmt.Sprintf("%.2f", l.One)
	rest := fmt.Sprintf("%.2f %.2f", l.Five, l.Fifteen)
	d := s.deltaText(string(metricLoad), floorLoad*100/float64(cores), "%.2f", float64(cores)/100)

	return []chip{{
		prio: prioLoad,
		forms: []string{
			s.text("LOAD", one, st, rest) + spaced(d),
			s.text("LOAD", one, st, rest),
			s.text("LOAD", one, st),
		},
	}}
}

// topChips name the busiest process by each measure. They are the one part of
// the bar that answers "why" rather than "how much".
func (s *System) topChips() []chip {
	if !s.top.ok {
		return nil
	}
	name := func(p procs.Process, w int) string { return humanize.Truncate(p.Name(), w) }

	return []chip{
		{
			prio: prioTopCPU,
			forms: []string{
				s.text("TOP CPU", name(s.top.cpu, 18), s.level(s.top.cpu.CPU, 70, 90), fmt.Sprintf("%.1f%%", s.top.cpu.CPU)),
				s.text("TOP", name(s.top.cpu, 12), s.level(s.top.cpu.CPU, 70, 90), fmt.Sprintf("%.0f%%", s.top.cpu.CPU)),
			},
		},
		{
			prio: prioTopMem,
			forms: []string{
				s.text("TOP MEM", name(s.top.mem, 18), s.theme.TextStyle(), humanize.Size(s.top.mem.RSS)),
				s.text("TOP MEM", name(s.top.mem, 12), s.theme.TextStyle(), humanize.Bytes(s.top.mem.RSS)),
			},
		},
	}
}

func (s *System) uptimeChips() []chip {
	if s.stats.Uptime <= 0 {
		return nil
	}
	full := humanize.Duration(s.stats.Uptime)
	short, _, _ := strings.Cut(full, " ")
	return []chip{{
		prio: prioUptime,
		forms: []string{
			s.text("UP", full, s.theme.TextStyle()),
			s.text("UP", short, s.theme.TextStyle()),
		},
	}}
}

// deltaText renders a metric's recent change as an arrow and a magnitude.
//
// Direction is coloured by pressure rather than by the convention a financial
// ticker uses: on this bar a rising number means the machine is working
// harder, so up is amber and down is green. Green for "CPU climbing" would
// read exactly backwards (ADR-027).
//
// scale converts the stored history's units back into the units shown, for
// load, which is recorded as a percentage of the core count.
func (s *System) deltaText(key string, floor float64, format string, scale ...float64) string {
	d, ok := s.delta(key, floor)
	if !ok {
		return ""
	}
	if len(scale) > 0 {
		d *= scale[0]
	}

	arrow, st := "▲", s.theme.WarnStyle()
	if d < 0 {
		arrow, st = "▼", s.theme.GoodStyle()
		d = -d
	}
	return st.Render(arrow + fmt.Sprintf(format, d))
}

// spaced prefixes a space to a part that may be empty, so an absent delta
// leaves no trailing gap to measure.
func spaced(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

// trimRate drops the per-second suffix, for the form where the label has
// already established that these are rates.
func trimRate(s string) string { return strings.TrimSuffix(s, "/s") }
