package system

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0xquark/ctos/internal/procs"
	"github.com/0xquark/ctos/internal/sysinfo"
	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// newSystem builds a widget from a YAML block, the way a dashboard does.
func newSystem(t *testing.T, yml string) (*System, error) {
	t.Helper()

	ctx := widget.Context{Name: "system", Theme: theme.New("")}
	if yml != "" {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(yml), &doc); err != nil {
			t.Fatal(err)
		}
		ctx.Node = doc.Content[0]
	}

	w, err := New(ctx)
	if err != nil {
		return nil, err
	}
	return w.(*System), nil
}

// loaded returns a widget holding a fixed sample, so that rendering tests do
// not depend on what the machine running them happens to be doing.
func loaded(t *testing.T, yml string) *System {
	t.Helper()
	s, err := newSystem(t, yml)
	if err != nil {
		t.Fatal(err)
	}
	s.loading = false
	s.stats = sampleStats()
	s.record(s.stats)
	return s
}

func sampleStats() sysinfo.Stats {
	const gb = 1 << 30
	return sysinfo.Stats{
		CPU: sysinfo.CPU{Busy: 22, User: 17, System: 5, OK: true},
		Mem: sysinfo.Memory{
			Used: 16 * gb, Total: 24 * gb, Free: 2 * gb,
			Cached: 5 * gb, Wired: 3 * gb, Compressed: 8 * gb, OK: true,
		},
		Swap: sysinfo.Memory{Used: 2 * gb, Total: 3 * gb, OK: true},
		Disks: []sysinfo.Disk{
			{Path: "/", Used: 18 * gb, Avail: 10 * gb, Total: 460 * gb, OK: true},
		},
		Net:    sysinfo.Net{Rx: 27000, Tx: 49000, OK: true},
		DiskIO: sysinfo.DiskIO{Read: 12 << 20, Write: 3500000, Total: 12<<20 + 3500000, Split: true, OK: true},
		Load:   sysinfo.Load{One: 4.62, Five: 3.76, Fifteen: 3.33, OK: true},
		Uptime: 23*time.Hour + 57*time.Minute,
		Cores:  12,
		Host:   "delorean",
	}
}

func TestDefaultsToEveryMetricAndTheRootFilesystem(t *testing.T) {
	s, err := newSystem(t, "type: system")
	if err != nil {
		t.Fatal(err)
	}
	// The panel leaves out the two metrics that have no magnitude to draw
	// a bar against; the strip, which is all text, takes everything.
	if len(s.metrics) != len(rowMetrics) {
		t.Errorf("metrics = %v, want %v", s.metrics, rowMetrics)
	}

	bar, err := newSystem(t, "type: system\nstyle: bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(bar.metrics) != len(allMetrics) {
		t.Errorf("bar metrics = %v, want all %v", bar.metrics, allMetrics)
	}
	if len(s.paths) != 1 || s.paths[0] != "/" {
		t.Errorf("paths = %v, want [/]", s.paths)
	}
	if !s.history {
		t.Error("history should default on")
	}
	if s.refresh != defaultRefresh {
		t.Errorf("refresh = %s, want %s", s.refresh, defaultRefresh)
	}
}

func TestMetricsAreOrderedAsConfigured(t *testing.T) {
	s, err := newSystem(t, "type: system\nmetrics: [load, cpu]")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.metrics) != 2 || s.metrics[0] != metricLoad || s.metrics[1] != metricCPU {
		t.Errorf("metrics = %v, want [load cpu]", s.metrics)
	}
}

// A dashboard is hand-written, so a metric nobody recognises has to name
// itself and its alternatives rather than quietly rendering one row fewer.
func TestUnknownMetricIsAConfigError(t *testing.T) {
	_, err := newSystem(t, "type: system\nmetrics: [cpu, memory]")
	if err == nil {
		t.Fatal("want an error for an unknown metric")
	}
	for _, want := range []string{`"memory"`, "cpu", "mem", "uptime"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestDuplicateMetricIsAConfigError(t *testing.T) {
	if _, err := newSystem(t, "type: system\nmetrics: [cpu, cpu]"); err == nil {
		t.Error("want an error for a metric listed twice")
	}
}

// A mount point is checked while the user is still looking at the file they
// typed it into, rather than becoming a permanently blank row.
func TestUnreadableDiskPathIsAConfigError(t *testing.T) {
	_, err := newSystem(t, `type: system
disks: ["/nonexistent-mount-point-for-tests"]`)
	if err == nil {
		t.Fatal("want an error for a path that does not exist")
	}
	if !strings.Contains(err.Error(), "/nonexistent-mount-point-for-tests") {
		t.Errorf("error %q should name the path", err)
	}
}

func TestRefreshIsClampedToTheFloor(t *testing.T) {
	s, err := newSystem(t, "type: system\nrefresh: 10ms")
	if err != nil {
		t.Fatal(err)
	}
	if s.refresh != minRefresh {
		t.Errorf("refresh = %s, want the %s floor", s.refresh, minRefresh)
	}
}

func TestEmptyDiskListDropsTheDiskRows(t *testing.T) {
	s, err := newSystem(t, "type: system\ndisks: []")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.paths) != 0 {
		t.Errorf("paths = %v, want none", s.paths)
	}
}

// The frame truncates anything wider than the pane, which would silently eat
// the right-hand column of every row. Every layout has to fit on its own.
func TestRowsNeverExceedTheWidth(t *testing.T) {
	for _, history := range []bool{true, false} {
		yml := "type: system"
		if !history {
			yml += "\nhistory: false"
		}
		s := loaded(t, yml)

		for w := 12; w <= 120; w++ {
			s.SetSize(w, 10)
			for i, line := range strings.Split(s.View(), "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Fatalf("history=%v width=%d: line %d is %d cells:\n%s", history, w, i, got, line)
				}
			}
		}
	}
}

// A pane with fewer rows than metrics packs the numbers instead of hiding the
// metrics that did not fit.
func TestShortPaneFallsBackToCompactRows(t *testing.T) {
	s := loaded(t, "type: system")
	s.SetSize(80, 2)

	out := s.View()
	if lines := strings.Count(out, "\n") + 1; lines > 2 {
		t.Errorf("a two-line pane rendered %d lines:\n%s", lines, out)
	}
	for _, want := range []string{"cpu", "mem", "load"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact output is missing %q:\n%s", want, out)
		}
	}
}

func TestCompactNeverOverflowsTheHeight(t *testing.T) {
	s := loaded(t, "type: system")
	for h := 1; h <= 4; h++ {
		s.SetSize(30, h)
		if lines := strings.Count(s.View(), "\n") + 1; lines > h {
			t.Errorf("height %d rendered %d lines", h, lines)
		}
	}
}

// Columns go in order of what a glance can spare: the detail text, then the
// history, then the bar, leaving the label and the number.
func TestLayoutDropsColumnsInOrder(t *testing.T) {
	wide := newLayout(90, 4, true)
	if wide.detail == 0 || wide.spark == 0 || wide.bar == 0 {
		t.Fatalf("a wide pane should keep everything: %+v", wide)
	}

	narrower := newLayout(34, 4, true)
	if narrower.detail != 0 || narrower.spark == 0 {
		t.Errorf("detail should go before the history: %+v", narrower)
	}

	narrow := newLayout(26, 4, true)
	if narrow.spark != 0 || narrow.bar == 0 {
		t.Errorf("the history should go before the bar: %+v", narrow)
	}

	tiny := newLayout(12, 4, true)
	if tiny.bar != 0 || tiny.value == 0 {
		t.Errorf("the number is the last thing standing: %+v", tiny)
	}
}

func TestLayoutFitsTheWidthItWasGiven(t *testing.T) {
	for w := 10; w <= 120; w++ {
		for _, history := range []bool{true, false} {
			l := newLayout(w, 4, history)
			used := indent + l.label + gap + l.value
			if l.spark > 0 {
				used += l.spark + gap
			}
			if l.bar > 0 {
				used += l.bar + gap
			}
			if l.detail > 0 {
				used += l.detail + gap
			}
			if l.value == 0 {
				used -= gap
			}
			if used > w {
				t.Errorf("width %d history=%v: layout %+v uses %d cells", w, history, l, used)
			}
		}
	}
}

// A bar that rounds a small reading down to nothing makes a busy machine look
// idle, so anything above zero keeps one cell.
func TestBarAlwaysShowsANonZeroReading(t *testing.T) {
	s := loaded(t, "type: system")
	if got := stripANSI(s.bar(0.4, 10, s.theme.GoodStyle())); !strings.HasPrefix(got, "█") {
		t.Errorf("bar(0.4%%) = %q, want one filled cell", got)
	}
	if got := stripANSI(s.bar(0, 10, s.theme.GoodStyle())); strings.Contains(got, "█") {
		t.Errorf("bar(0%%) = %q, want no filled cells", got)
	}
	if got := stripANSI(s.bar(100, 10, s.theme.GoodStyle())); got != strings.Repeat("█", 10) {
		t.Errorf("bar(100%%) = %q, want a full bar", got)
	}
}

// Swap being switched off explains how the machine behaves under pressure, so
// the row stays and says so.
func TestSwapOffRendersAsOff(t *testing.T) {
	s := loaded(t, "type: system\nmetrics: [swap]")
	s.stats.Swap = sysinfo.Memory{Total: 0, OK: true}
	s.SetSize(60, 4)

	if !strings.Contains(s.View(), "off") {
		t.Errorf("swap that is off should say so:\n%s", s.View())
	}
}

// Throughput is a difference between two samples, so the first tick has none.
func TestNetWithoutABaselineSaysNothingYet(t *testing.T) {
	s := loaded(t, "type: system\nmetrics: [net]")
	s.stats.Net = sysinfo.Net{}
	s.SetSize(60, 4)

	if out := stripANSI(s.View()); !strings.Contains(out, "…") {
		t.Errorf("the first net row should show a placeholder, got %q", out)
	}
}

// A metric the platform did not answer for is a row that is not drawn, not a
// row of dashes.
func TestUnavailableMetricsAreSkipped(t *testing.T) {
	s := loaded(t, "type: system")
	s.stats.CPU = sysinfo.CPU{}
	s.stats.Load = sysinfo.Load{}
	s.SetSize(70, 10)

	out := stripANSI(s.View())
	if strings.Contains(out, "cpu") || strings.Contains(out, "load") {
		t.Errorf("unavailable metrics should not render:\n%s", out)
	}
	if !strings.Contains(out, "mem") {
		t.Errorf("the metrics that did answer should still render:\n%s", out)
	}
}

func TestDiskRowIsLabelledByItsMountPoint(t *testing.T) {
	s := loaded(t, "type: system\nmetrics: [disk]")
	s.stats.Disks = []sysinfo.Disk{
		{Path: "/", Used: 1, Avail: 1, OK: true},
		{Path: "/data", Used: 3, Avail: 1, OK: true},
	}
	s.SetSize(70, 10)

	out := stripANSI(s.View())
	if !strings.Contains(out, "/data") {
		t.Errorf("each mount point gets its own row:\n%s", out)
	}
	if !strings.Contains(out, "75%") {
		t.Errorf("/data is three-quarters full:\n%s", out)
	}
}

// A sample can outlast the refresh tick — one CPU reading takes a second on
// macOS — so a tick arriving mid-sample must not start a second one.
func TestTickDoesNotStackSamples(t *testing.T) {
	s := loaded(t, "type: system")
	s.inflight = true

	if cmd := s.Update(tickMsg{}); cmd == nil {
		t.Fatal("a skipped tick should still reschedule")
	}
	if !s.inflight {
		t.Error("a skipped tick should leave the running sample alone")
	}

	// The result clears the guard and schedules the next tick.
	s.Update(sampledMsg{stats: sampleStats()})
	if s.inflight {
		t.Error("a result should clear the in-flight guard")
	}
}

func TestSampleErrorIsShown(t *testing.T) {
	s := loaded(t, "type: system")
	s.SetSize(60, 6)
	s.Update(sampledMsg{err: errFake{}})

	if out := stripANSI(s.View()); !strings.Contains(out, "no ps here") {
		t.Errorf("the error should be rendered, got %q", out)
	}
}

func TestRefreshKeyResamples(t *testing.T) {
	s := loaded(t, "type: system")
	if cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}); cmd == nil {
		t.Fatal(`"r" should start a sample`)
	}
	if !s.inflight {
		t.Error(`"r" should set the in-flight guard`)
	}
}

// History off means no sparklines are drawn and none are kept.
func TestHistoryCanBeTurnedOff(t *testing.T) {
	s := loaded(t, "type: system\nhistory: false")
	if len(s.hist) != 0 {
		t.Errorf("history should not be recorded when it is off: %v", s.hist)
	}
	if got := s.sparkline("cpu", 100); got != "" {
		t.Errorf("sparkline = %q, want empty", got)
	}
}

type errFake struct{}

func (errFake) Error() string { return "no ps here" }

// stripANSI removes escape sequences so tests compare text, not colours.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// loadedBar is a widget in the status-bar style, with a fixed sample and
// enough history for the deltas to have something to measure against.
func loadedBar(t *testing.T, yml string) *System {
	t.Helper()
	s := loaded(t, yml)
	s.top = topProcs{
		cpu: procs.Process{Command: "/Applications/Arc.app/Contents/MacOS/Arc", CPU: 33.8},
		mem: procs.Process{Command: "/usr/lib/BrowserHelper", RSS: 3 << 30},
		ok:  true,
	}
	return s
}

// Given room for everything, the bar shows everything at full detail.
func TestBarStyleRendersEveryValue(t *testing.T) {
	s := loadedBar(t, "type: system\nstyle: bar")
	s.SetSize(300, 1)

	out := stripANSI(s.View())
	for _, want := range []string{
		"CPU 22.0%",
		"MEM ", "67% 16.0G/24.0G", // the segment bar sits between the two
		"free 2.0G", "cache 5.0G", "wired 3.0G", "comp 8.0G",
		"SWP 67% 2.0G/3.0G",
		"/ 64% 10.0G free",
		"DISK ↓12M/s ↑3.3M/s", // split, because the fixture says Linux
		"NET ↓26K/s ↑48K/s",
		"LOAD 4.62 3.76 3.33",
		"TOP CPU Arc 33.8%",
		"TOP MEM BrowserHelper 3.0G",
		"UP 23h 57m",
		"│", // pipe-separated, ticker style
	} {
		if !strings.Contains(out, want) {
			t.Errorf("bar is missing %q:\n%s", want, out)
		}
	}
}

// The memory bar is drawn from what cannot be reclaimed through to what is
// already free, so its shape is readable without a legend.
func TestMemBarSegmentsAreOrderedByReclaimability(t *testing.T) {
	s := loadedBar(t, "type: system\nstyle: bar")
	// Eighths, so the rounding is exact: 1 wired, 1 compressed, 2 other
	// in use, 2 cached, 2 free.
	m := sysinfo.Memory{
		Used: 4 << 30, Total: 8 << 30,
		Wired: 1 << 30, Compressed: 1 << 30, Cached: 2 << 30, OK: true,
	}

	// Everything in use is a solid block — the three colours tell wired
	// from compressed from the rest — then cache, then free.
	if got := stripANSI(s.memBar(m, 8)); got != "████▒▒░░" {
		t.Errorf("memBar = %q, want ████▒▒░░", got)
	}
}

// The strip must never be wider than the terminal, at any width.
func TestBarNeverExceedsTheWidth(t *testing.T) {
	s := loadedBar(t, "type: system\nstyle: bar")
	for w := 20; w <= 200; w++ {
		s.SetSize(w, 3)
		for i, line := range strings.Split(s.View(), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Fatalf("width %d: line %d is %d cells:\n%s", w, i, got, line)
			}
		}
	}
}

// Lines is what the shell reserves, so it has to match what View actually
// draws — a disagreement is a gap or an overlap on screen.
// The strip is one line by definition, at every width.
func TestBarIsAlwaysOneLine(t *testing.T) {
	s := loadedBar(t, "type: system\nstyle: bar")
	for w := 10; w <= 400; w += 7 {
		if got := s.Lines(w); got != 1 {
			t.Errorf("Lines(%d) = %d, want 1", w, got)
		}
		s.SetSize(w, 1)
		if got := lipgloss.Height(s.View()); got != 1 {
			t.Errorf("width %d: View drew %d lines, want 1", w, got)
		}
	}
}

// Narrowing the bar drops its least important values, and keeps the ones it
// exists to show.
func TestBarKeepsTheImportantValuesLongest(t *testing.T) {
	s := loadedBar(t, "type: system\nstyle: bar")

	s.SetSize(50, 1)
	out := stripANSI(s.View())
	for _, want := range []string{"CPU", "MEM"} {
		if !strings.Contains(out, want) {
			t.Errorf("a 50-cell bar dropped %q:\n%s", want, out)
		}
	}
	// The memory breakdown is the first thing to go.
	for _, gone := range []string{"WIRED", "COMP", "CACHE"} {
		if strings.Contains(out, gone) {
			t.Errorf("a 50-cell bar should have dropped %q:\n%s", gone, out)
		}
	}
}

// Even at an absurd width the bar shows something rather than nothing.
func TestBarSurvivesAnAbsurdlyNarrowPane(t *testing.T) {
	s := loadedBar(t, "type: system\nstyle: bar")
	for _, w := range []int{4, 8, 12} {
		s.SetSize(w, 1)
		if out := strings.TrimSpace(stripANSI(s.View())); out == "" {
			t.Errorf("width %d rendered nothing", w)
		}
	}
}

// Deltas are the difference between a readout and a ticker, but only above a
// floor: a machine at rest jitters every tick.
func TestDeltasAppearOnlyOnRealMovement(t *testing.T) {
	s := loadedBar(t, "type: system\nstyle: bar\nmetrics: [cpu]")
	s.SetSize(80, 1)

	// Flat history: no arrow.
	for range s.deltaWindow + 1 {
		s.record(s.stats)
	}
	if out := stripANSI(s.View()); strings.ContainsAny(out, "▲▼") {
		t.Errorf("a flat reading should draw no arrow, got %q", out)
	}

	// A real climb: an arrow, pointing up.
	for range s.deltaWindow + 1 {
		st := s.stats
		st.CPU.Busy += 5
		s.stats = st
		s.record(st)
	}
	if out := stripANSI(s.View()); !strings.Contains(out, "▲") {
		t.Errorf("a rising reading should draw ▲, got %q", out)
	}
}

func TestDeltasCanBeTurnedOff(t *testing.T) {
	s := loadedBar(t, "type: system\nstyle: bar\ndeltas: false")
	for range s.deltaWindow + 1 {
		st := s.stats
		st.CPU.Busy += 5
		s.stats = st
		s.record(st)
	}
	s.SetSize(120, 3)

	if out := stripANSI(s.View()); strings.ContainsAny(out, "▲▼") {
		t.Errorf("deltas are off, got %q", out)
	}
}

// The window is measured in wall-clock time, so a slower refresh compares
// against fewer samples for the same span.
func TestDeltaWindowFollowsTheRefreshRate(t *testing.T) {
	cases := map[time.Duration]int{
		time.Second:     30,
		3 * time.Second: 10,
		time.Minute:     1, // slower than the span: compare with the last one
	}
	for refresh, want := range cases {
		if got := deltaWindow(refresh); got != want {
			t.Errorf("deltaWindow(%s) = %d, want %d", refresh, got, want)
		}
	}
}

func TestUnknownStyleIsAConfigError(t *testing.T) {
	_, err := newSystem(t, "type: system\nstyle: ticker")
	if err == nil {
		t.Fatal("want an error for an unknown style")
	}
	for _, want := range []string{`"ticker"`, "rows", "bar"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// macOS cannot separate disk reads from writes, so the bar must not draw
// arrows it has no basis for.
func TestCombinedDiskThroughputDrawsNoArrows(t *testing.T) {
	s := loadedBar(t, "type: system\nstyle: bar\nmetrics: [diskio]")
	s.stats.DiskIO = sysinfo.DiskIO{Total: 12 << 20, Split: false, OK: true}
	s.SetSize(80, 1)

	out := stripANSI(s.View())
	if !strings.Contains(out, "DISK 12M/s") {
		t.Errorf("want a combined figure, got %q", out)
	}
	if strings.ContainsAny(out, "↓↑") {
		t.Errorf("a combined figure has no direction to show, got %q", out)
	}
}

// The top process is a second `ps`, so it must not run when nobody asked.
func TestTopProcessIsOnlySampledWhenConfigured(t *testing.T) {
	without := loaded(t, "type: system\nmetrics: [cpu]")
	if slices.Contains(without.metrics, metricTop) {
		t.Fatal("test setup: top should not be configured here")
	}

	with := loaded(t, "type: system\nstyle: bar")
	if !slices.Contains(with.metrics, metricTop) {
		t.Error("the bar style should include the top process by default")
	}
}

// The fitting algorithm, in isolation from what the chips happen to say.
func TestFitOneLineGivesUpDetailBeforeValues(t *testing.T) {
	chips := []chip{
		{prio: 100, forms: []string{"CPU 8.0% up", "CPU 8.0%", "CPU 8%"}},
		{prio: 10, forms: []string{"CACHE 4.0G", "CACHE 4G"}},
	}

	// Room for everything: every chip at its fullest.
	if got := fitOneLine(chips, " | ", 40); got != "CPU 8.0% up | CACHE 4.0G" {
		t.Errorf("fitOneLine(40) = %q", got)
	}
	// A little tight: the low-priority chip gives up detail first.
	if got := fitOneLine(chips, " | ", 23); got != "CPU 8.0% up | CACHE 4G" {
		t.Errorf("fitOneLine(23) = %q, want CACHE shortened first", got)
	}
	// Tighter: the low-priority chip goes entirely, rather than the
	// important one losing detail while the unimportant one keeps it.
	if got := fitOneLine(chips, " | ", 20); got != "CPU 8.0% up" {
		t.Errorf("fitOneLine(20) = %q, want CACHE dropped before CPU shortens", got)
	}
	// Only once the tier is gone does the headline start shortening.
	if got := fitOneLine(chips, " | ", 10); got != "CPU 8.0%" {
		t.Errorf("fitOneLine(10) = %q", got)
	}
	if got := fitOneLine(chips, " | ", 7); got != "CPU 8%" {
		t.Errorf("fitOneLine(7) = %q", got)
	}
}

// The last value standing is truncated rather than dropped: the start of one
// number says more than an empty bar.
func TestFitOneLineNeverRendersNothing(t *testing.T) {
	chips := []chip{{prio: 100, forms: []string{"CPU 8.0%"}}}
	got := fitOneLine(chips, " | ", 4)

	if got == "" {
		t.Error("the last chip should be truncated, not dropped")
	}
	if lipgloss.Width(got) > 4 {
		t.Errorf("fitOneLine = %q, wider than 4", got)
	}
}

// Among values of equal importance, the widest is the one to give up: it buys
// the most room for the same loss.
func TestFitOneLineShrinksTheWidestOfEquals(t *testing.T) {
	chips := []chip{
		{prio: 50, forms: []string{"SHORT 1", "S 1"}},
		{prio: 50, forms: []string{"MUCH LONGER 2", "M 2"}},
	}
	if got := fitOneLine(chips, " | ", 16); got != "SHORT 1 | M 2" {
		t.Errorf("fitOneLine = %q, want the longer chip shortened", got)
	}
}

// The memory bar is exactly as wide as asked, whatever the rounding does.
func TestMemBarIsExactlyTheRequestedWidth(t *testing.T) {
	s := loadedBar(t, "type: system\nstyle: bar")
	for _, m := range []sysinfo.Memory{
		{Used: 16 << 30, Total: 24 << 30, Cached: 5 << 30, Wired: 3 << 30, Compressed: 8 << 30, OK: true},
		{Used: 1, Total: 24 << 30, OK: true},
		{Used: 24 << 30, Total: 24 << 30, OK: true},
		{Used: 0, Total: 3, Cached: 1, OK: true},
	} {
		for w := 1; w <= 20; w++ {
			if got := lipgloss.Width(s.memBar(m, w)); got != w {
				t.Errorf("memBar(%+v, %d) is %d cells", m, w, got)
			}
		}
	}
	if got := s.memBar(sysinfo.Memory{}, 8); got != "" {
		t.Errorf("an unread memory pool has no bar, got %q", got)
	}
}
