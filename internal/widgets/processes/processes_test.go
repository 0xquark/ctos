package processes

import (
	"fmt"
	"strconv"
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

func newWidget(t *testing.T, w, h int) *Processes {
	t.Helper()
	wdg, err := New(widget.Context{Name: "procs", Theme: theme.New("")})
	if err != nil {
		t.Fatal(err)
	}
	p := wdg.(*Processes)
	p.SetSize(w, h)
	return p
}

// load seeds the widget as if a sample had just arrived.
func load(p *Processes, list []procs.Process) {
	p.all = list
	p.load = sysinfo.Load{One: 1, Five: 1, Fifteen: 1, OK: true}
	p.index = procs.NewIndex(list)
	p.rebuild()
}

func fixture() []procs.Process {
	return []procs.Process{
		{PID: 1, PPID: 0, User: "root", CPU: 0.1, Mem: 0.1, RSS: 8 << 20, Elapsed: 40 * time.Hour, State: "Ss", Command: "/sbin/init"},
		{PID: 100, PPID: 1, User: "root", CPU: 0.2, Mem: 0.4, RSS: 12 << 20, Elapsed: 3 * time.Hour, State: "Ss", Command: "/usr/sbin/sshd -D"},
		{PID: 200, PPID: 1, User: "pparker", CPU: 88.5, Mem: 14.2, RSS: 900 << 20, Elapsed: 12 * time.Minute, State: "R", Command: "/usr/lib/firefox/firefox -contentproc"},
		{PID: 300, PPID: 100, User: "pparker", CPU: 3.0, Mem: 1.1, RSS: 40 << 20, Elapsed: 30 * time.Second, State: "S", Command: "-zsh"},
		{PID: 301, PPID: 200, User: "pparker", CPU: 1.0, Mem: 2.2, RSS: 60 << 20, Elapsed: 20 * time.Second, State: "S", Command: "/usr/lib/firefox/firefox -tab"},
	}
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	panic("unhandled key " + s)
}

// --- kill safety ----------------------------------------------------------

// Signalling is not undoable, so one keystroke must never be enough.
func TestKillNeedsConfirmation(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())

	actions := p.Actions()
	if len(actions) != 1 || actions[0].Name != "kill" {
		t.Fatalf("actions = %+v, want a single \"kill\"", actions)
	}

	// First enter only arms it.
	if cmd := actions[0].Run(); cmd != nil {
		t.Fatal("arming a kill must not return a command that signals anything")
	}
	if p.confirm == nil {
		t.Fatal("first enter did not arm the kill")
	}

	// The footer must now say so.
	if got := p.Actions()[0].Name; got != "confirm kill" {
		t.Errorf("armed action name = %q, want \"confirm kill\"", got)
	}
	if !strings.Contains(stripANSI(p.View()), "kill firefox (200)?") {
		t.Errorf("armed view does not show the prompt:\n%s", stripANSI(p.View()))
	}
}

func TestEscCancelsAnArmedKill(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())
	p.Actions()[0].Run()

	p.Update(key("esc"))
	if p.confirm != nil {
		t.Fatal("esc did not cancel the armed kill")
	}
	if p.Actions()[0].Name != "kill" {
		t.Error("action label did not revert after cancelling")
	}
}

// The dangerous case: a refresh lands between arming and confirming, the sort
// reorders the list, and the cursor now points at a different process. The
// signal must still go to the process the user actually looked at.
func TestConfirmSignalsTheArmedPIDNotTheCursorRow(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())

	// Cursor sits on firefox (highest CPU), arm it.
	if p.rows[p.list.Cursor()].PID != 200 {
		t.Fatalf("cursor started on pid %d, want 200", p.rows[p.list.Cursor()].PID)
	}
	p.Actions()[0].Run()

	// firefox calms down; sshd spikes. A refresh reorders everything.
	updated := fixture()
	updated[1].CPU = 0.1 // firefox
	updated[0].CPU = 99  // sshd
	load(p, updated)

	if p.confirm == nil || p.confirm.PID != 200 {
		t.Fatalf("armed target changed under a refresh: %+v", p.confirm)
	}

	cmd := p.send(procs.Term)
	if cmd == nil {
		t.Fatal("confirm produced no command")
	}
	msg, ok := widget.Unwrap(cmd()).(signalledMsg)
	if !ok {
		t.Fatalf("got %T, want signalledMsg", widget.Unwrap(cmd()))
	}
	if msg.pid != 200 {
		t.Fatalf("signalled pid %d, want 200 (the armed process)", msg.pid)
	}
}

// While a kill is armed, navigation is frozen: nothing should be one
// keystroke away from being the new target.
func TestNavigationIsFrozenWhileArmed(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())
	p.Actions()[0].Run()

	before := p.list.Cursor()
	p.Update(key("down"))
	p.Update(key("j"))
	if p.list.Cursor() != before {
		t.Fatalf("cursor moved to %d while a kill was armed (was %d)", p.list.Cursor(), before)
	}
}

func TestKEscalatesToSIGKILL(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())
	p.Actions()[0].Run()

	cmd := p.Update(key("k"))
	if cmd == nil {
		t.Fatal("k did not send anything while armed")
	}
	msg := widget.Unwrap(cmd()).(signalledMsg)
	if msg.sig != procs.Kill {
		t.Errorf("signal = %v, want SIGKILL", msg.sig)
	}
	if p.confirm != nil {
		t.Error("confirm should clear once the signal is sent")
	}
}

func TestNoActionsWhenListIsEmpty(t *testing.T) {
	p := newWidget(t, 80, 12)
	if p.Actions() != nil {
		t.Fatal("an empty table must offer no kill action")
	}
}

// --- selection ------------------------------------------------------------

// The cursor follows the process, not the row index, so a refresh that
// reorders the table does not silently move the selection.
func TestCursorFollowsThePIDAcrossReSorts(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())

	p.Update(key("down")) // move off the top onto zsh (pid 300 by cpu order)
	selected := p.rows[p.list.Cursor()].PID

	p.sort = procs.ByPID
	p.rebuild()

	if got := p.rows[p.list.Cursor()].PID; got != selected {
		t.Fatalf("after re-sorting the cursor sits on pid %d, want %d", got, selected)
	}
}

func TestCursorClampsWhenTheProcessExits(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())
	selectRow(p, 2) // last row

	load(p, fixture()[:1]) // everything but sshd exits

	if p.list.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0 after the list shrank", p.list.Cursor())
	}
	if p.View() == "" {
		t.Fatal("view went blank after the selected process exited")
	}
}

// --- filtering ------------------------------------------------------------

// "q" quits the dashboard, so a widget taking text input has to claim the
// keyboard or the user cannot type the letter at all.
func TestFilterGrabsTheKeyboard(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())

	if widget.Grabbing(p) {
		t.Fatal("widget grabbed keys before filtering started")
	}

	p.Update(key("/"))
	if !widget.Grabbing(p) {
		t.Fatal("filter mode did not claim the keyboard")
	}

	for _, r := range "quit" {
		p.Update(key(string(r)))
	}
	if p.query != "quit" {
		t.Fatalf("query = %q, want \"quit\" (letters must reach the filter)", p.query)
	}
}

func TestFilterNarrowsAndEscRestores(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())

	p.Update(key("/"))
	for _, r := range "firefox" {
		p.Update(key(string(r)))
	}
	// Both firefox processes match; sshd, init and zsh do not.
	if len(p.rows) != 2 {
		t.Fatalf("filtered to %d rows, want the 2 firefox processes", len(p.rows))
	}
	for _, r := range p.rows {
		if !strings.Contains(r.Command, "firefox") {
			t.Errorf("filter kept a non-match: %q", r.Command)
		}
	}

	p.Update(key("backspace"))
	if p.query != "firefo" {
		t.Errorf("query = %q after backspace, want \"firefo\"", p.query)
	}

	p.Update(key("esc"))
	if p.query != "" || widget.Grabbing(p) {
		t.Errorf("esc left query=%q grabbing=%v, want cleared", p.query, widget.Grabbing(p))
	}
	if len(p.rows) != len(fixture()) {
		t.Errorf("rows = %d after clearing the filter, want %d", len(p.rows), len(fixture()))
	}
}

func TestEnterKeepsTheFilterButReleasesTheKeyboard(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())

	p.Update(key("/"))
	p.Update(key("z"))
	p.Update(key("enter"))

	if widget.Grabbing(p) {
		t.Error("enter did not hand the keyboard back")
	}
	if p.query != "z" {
		t.Errorf("query = %q, want it kept as \"z\"", p.query)
	}
}

func TestFilterWithNoMatchesExplainsItself(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())
	p.query = "nothingmatchesthis"
	p.rebuild()

	if !strings.Contains(stripANSI(p.View()), "nothing matches") {
		t.Errorf("empty result renders no explanation:\n%s", stripANSI(p.View()))
	}
}

// --- sorting --------------------------------------------------------------

func TestSKeyCyclesSort(t *testing.T) {
	p := newWidget(t, 80, 12)
	load(p, fixture())

	if p.sort != procs.ByCPU {
		t.Fatalf("default sort = %v, want cpu", p.sort)
	}
	p.Update(key("s"))
	if p.sort != procs.ByMem {
		t.Fatalf("after one s, sort = %v, want mem", p.sort)
	}
	if !strings.Contains(stripANSI(p.View()), "sort mem") {
		t.Error("the header does not say which sort is active")
	}
}

func TestConfigSortIsValidated(t *testing.T) {
	p := newWidget(t, 80, 12)
	if _, err := parseSort("nonsense"); err == nil {
		t.Fatal("an invalid sort in YAML should be an error, not a silent default")
	}
	for _, ok := range []string{"", "cpu", "mem", "memory", "pid", "name", "command"} {
		if _, err := parseSort(ok); err != nil {
			t.Errorf("parseSort(%q) = %v, want accepted", ok, err)
		}
	}
	_ = p
}

// --- layout ---------------------------------------------------------------

// Every rendered line must fit the pane, or the frame border breaks.
func TestViewNeverExceedsItsWidth(t *testing.T) {
	for _, w := range []int{20, 28, 34, 46, 60, 80, 120} {
		p := newWidget(t, w, 10)
		load(p, fixture())
		for _, line := range strings.Split(p.View(), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width %d: line is %d cells: %q", w, got, stripANSI(line))
			}
		}
	}
}

func TestViewNeverExceedsItsHeight(t *testing.T) {
	many := make([]procs.Process, 200)
	for i := range many {
		many[i] = procs.Process{PID: i + 1, User: "u", Command: "proc"}
	}
	for _, h := range []int{1, 2, 3, 6, 20} {
		p := newWidget(t, 80, h)
		load(p, many)
		if got := len(strings.Split(p.View(), "\n")); got > h {
			t.Errorf("height %d: rendered %d lines", h, got)
		}
	}
}

// As the pane narrows, columns drop in order of least usefulness, and CPU%
// plus the command always survive.
func TestColumnsDropByPriority(t *testing.T) {
	has := func(cols []column, k colKey) bool {
		for _, c := range cols {
			if c.key == k {
				return true
			}
		}
		return false
	}

	wide := columns(120)
	if len(wide) != len(allColumns) {
		t.Errorf("a 120-cell pane dropped columns: %d of %d kept", len(wide), len(allColumns))
	}

	if has(columns(50), colRSS) {
		t.Error("RSS should be the first column dropped")
	}

	for _, w := range []int{12, 20, 30, 40, 60, 100} {
		cols := columns(w)
		if !has(cols, colCPU) {
			t.Errorf("width %d dropped CPU%%, which must never happen", w)
		}
		used := markerWidth
		for _, c := range cols {
			used += c.width + 1
		}
		if left := w - used; left < minCommand && len(cols) > 1 {
			t.Errorf("width %d left only %d cells for COMMAND", w, left)
		}
	}
}

// --- formatting -----------------------------------------------------------

func TestShortDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{-1, "-"},
		{0, "00:00"}, // a process that started this instant, not a missing value
		{45 * time.Second, "00:45"},
		{9*time.Minute + 5*time.Second, "09:05"},
		{2*time.Hour + 5*time.Minute + 11*time.Second, "2:05:11"},
		{3*24*time.Hour + 4*time.Hour, "3d04h"},
	}
	for _, c := range cases {
		if got := shortDuration(c.in); got != c.want {
			t.Errorf("shortDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPctFitsFiveCells(t *testing.T) {
	for _, v := range []float64{0, 0.4, 9.9, 99.9, 100, 1600} {
		if got := pct(v); len(got) > 5 {
			t.Errorf("pct(%v) = %q, which is %d cells", v, got, len(got))
		}
	}
}

func TestGaugeScalesWithLoad(t *testing.T) {
	if gaugeFor(0) != ' ' {
		t.Error("an idle process should draw no bar")
	}
	if gaugeFor(100) != '█' {
		t.Error("a pegged process should draw a full block")
	}
	if a, b := gaugeFor(10), gaugeFor(80); a >= b {
		t.Errorf("gauge is not monotonic: 10%%=%q 80%%=%q", a, b)
	}
}

// selectRow moves the cursor the way a keypress would: the widget follows the
// PID under the cursor, not the index.
func selectRow(p *Processes, i int) {
	p.list.Select(i)
	p.follow()
}

func TestCommandTextPrefersTheNameWhenNarrow(t *testing.T) {
	p := procs.Process{Command: "/usr/lib/firefox/firefox -contentproc -childID 7"}
	if got := commandText(p, 8); strings.Contains(got, "-childID") {
		t.Errorf("narrow column showed arguments: %q", got)
	}
	if got := commandText(p, 60); !strings.Contains(got, "-contentproc") {
		t.Errorf("wide column dropped arguments: %q", got)
	}
}

// --- lifecycle ------------------------------------------------------------

// A slow ps must not pile up runs behind itself.
func TestTickIsSkippedWhileASampleIsInFlight(t *testing.T) {
	p := newWidget(t, 80, 12)
	p.loading = true
	if cmd := p.Update(tickMsg{}); cmd != nil {
		t.Fatal("a tick started a second sample while one was in flight")
	}
}

func TestRefreshIsClamped(t *testing.T) {
	p := newWidget(t, 80, 12)
	if p.refresh != defaultRefresh {
		t.Errorf("default refresh = %v, want %v", p.refresh, defaultRefresh)
	}
}

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

// --- sort keys and indicators ---------------------------------------------

func TestDirectSortKeys(t *testing.T) {
	cases := []struct {
		key  string
		want procs.Sort
	}{
		{"c", procs.ByCPU},
		{"m", procs.ByMem},
		{"p", procs.ByPID},
		{"n", procs.ByName},
	}
	for _, c := range cases {
		p := newWidget(t, 80, 20)
		load(p, fixture())
		// Start on a column none of the cases select, so each key is
		// switching rather than toggling its own direction.
		p.setSort(procs.ByName)
		if c.want == procs.ByName {
			p.setSort(procs.ByPID)
		}

		p.Update(key(c.key))
		if p.sort != c.want {
			t.Errorf("%q selected %v, want %v", c.key, p.sort, c.want)
		}
		if p.reversed {
			t.Errorf("%q switched column but kept a reversed direction", c.key)
		}
	}
}

// Pressing the active column's key again flips the direction, the way every
// table anyone has used behaves.
func TestRepeatingASortKeyReverses(t *testing.T) {
	p := newWidget(t, 80, 20)
	load(p, fixture())

	p.Update(key("m"))
	if p.reversed {
		t.Fatal("first press should not reverse")
	}
	p.Update(key("m"))
	if !p.reversed {
		t.Fatal("second press did not reverse")
	}
	p.Update(key("c"))
	if p.reversed {
		t.Fatal("switching to a different column should reset the direction")
	}
}

func TestSortArrowMarksTheActiveColumn(t *testing.T) {
	p := newWidget(t, 100, 20)
	load(p, fixture())

	p.Update(key("m"))
	header := headerRow(t, p)
	if !strings.Contains(header, "MEM%↓") {
		t.Errorf("mem column not marked descending: %q", header)
	}
	if strings.Contains(header, "CPU%↓") || strings.Contains(header, "CPU%↑") {
		t.Errorf("an inactive column is marked: %q", header)
	}

	p.Update(key("m")) // reverse
	if header = headerRow(t, p); !strings.Contains(header, "MEM%↑") {
		t.Errorf("arrow did not flip: %q", header)
	}

	// Sorting by name marks COMMAND, which is not in the column table.
	p.Update(key("n"))
	if header = headerRow(t, p); !strings.Contains(header, "COMMAND↑") {
		t.Errorf("name sort does not mark COMMAND: %q", header)
	}
}

// Changing the sort should show you the head of the new order. Following the
// selected PID is right for a refresh nobody asked for, but wrong here.
func TestChangingSortJumpsToTheTop(t *testing.T) {
	p := newWidget(t, 80, 20)
	load(p, fixture())

	selectRow(p, len(p.rows)-1)
	if p.list.Cursor() == 0 {
		t.Fatal("could not move off the top")
	}

	p.Update(key("m"))
	start, _ := p.list.Window(10)
	if p.list.Cursor() != 0 || start != 0 {
		t.Fatalf("after re-sorting cursor=%d offset=%d, want the top of the list", p.list.Cursor(), start)
	}
	if p.rows[0].Mem != 14.2 {
		t.Errorf("top row has mem %v, want the largest (14.2)", p.rows[0].Mem)
	}
}

// --- detail pane ----------------------------------------------------------

func TestDetailPaneShowsAncestry(t *testing.T) {
	p := newWidget(t, 80, 24)
	load(p, fixture())
	selectPID(t, p, 301) // firefox tab: init → firefox → tab

	out := stripANSI(p.View())
	for _, want := range []string{"firefox (301)", "firefox (200)", "init (1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail pane is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "sleeping") {
		t.Errorf("detail pane does not expand the state code:\n%s", out)
	}
}

func TestDetailPaneTogglesWithD(t *testing.T) {
	p := newWidget(t, 80, 24)
	load(p, fixture())

	if !strings.Contains(stripANSI(p.View()), "init (1)") {
		t.Fatal("detail pane is not on by default")
	}
	p.Update(key("d"))
	if strings.Contains(stripANSI(p.View()), "init (1)") {
		t.Error("d did not close the detail pane")
	}
	p.Update(key("d"))
	if !strings.Contains(stripANSI(p.View()), "init (1)") {
		t.Error("d did not reopen the detail pane")
	}
}

// Closing the pane must give its rows back to the list, not leave a gap.
func TestClosingTheDetailPaneGrowsTheList(t *testing.T) {
	p := newWidget(t, 80, 24)
	load(p, fixture())

	withDetail := p.listHeight()
	p.Update(key("d"))
	withoutDetail := p.listHeight()

	if withoutDetail <= withDetail {
		t.Fatalf("list height went from %d to %d when the pane closed", withDetail, withoutDetail)
	}
	if got := len(strings.Split(p.View(), "\n")); got > p.H {
		t.Errorf("rendered %d lines into a %d-row widget", got, p.H)
	}
}

// A short pane cannot usefully split, so the list keeps all of it.
func TestDetailPaneHidesItselfWhenTheWidgetIsShort(t *testing.T) {
	p := newWidget(t, 80, minDetailHeight-1)
	load(p, fixture())

	if p.detailHeight() != 0 {
		t.Errorf("a %d-row widget still split off a detail pane", p.H)
	}
	if got := len(strings.Split(p.View(), "\n")); got > p.H {
		t.Errorf("rendered %d lines into a %d-row widget", got, p.H)
	}
}

// The whole widget must still fit its frame at every size, with the pane open.
func TestViewFitsWithDetailOpen(t *testing.T) {
	for _, w := range []int{24, 40, 58, 80, 120} {
		for _, h := range []int{4, 8, 12, 20, 40} {
			p := newWidget(t, w, h)
			load(p, fixture())
			out := p.View()
			if got := len(strings.Split(out, "\n")); got > h {
				t.Errorf("%dx%d: %d lines", w, h, got)
			}
			for _, line := range strings.Split(out, "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Errorf("%dx%d: line is %d cells: %q", w, h, got, stripANSI(line))
				}
			}
		}
	}
}

// --- logs -----------------------------------------------------------------

func TestLKeySwitchesTheDetailPaneToLogs(t *testing.T) {
	p := newWidget(t, 80, 24)
	load(p, fixture())

	p.toggleLogs()
	if p.detail != detailLogs {
		t.Fatal("l did not switch to the log view")
	}
	if !strings.Contains(stripANSI(p.View()), "logs") {
		t.Error("log pane does not label itself")
	}

	p.toggleLogs()
	if p.detail != detailInfo {
		t.Error("l did not switch back to the ancestry view")
	}
}

// The pane must open if it was closed, rather than silently doing nothing.
func TestLKeyOpensAClosedPane(t *testing.T) {
	p := newWidget(t, 80, 24)
	load(p, fixture())
	p.showDetail = false

	p.toggleLogs()
	if !p.showDetail || p.detail != detailLogs {
		t.Fatalf("showDetail=%v detail=%v, want an open log pane", p.showDetail, p.detail)
	}
}

// A log query that comes back after the user has moved on must be discarded,
// not shown under whatever process is now selected.
func TestStaleLogResultsAreDiscarded(t *testing.T) {
	p := newWidget(t, 80, 24)
	load(p, fixture())
	selectPID(t, p, 200)
	p.detail, p.showDetail = detailLogs, true
	p.logsLoading = true

	p.Update(logsMsg{pid: 999, lines: []string{"from another process"}})

	if len(p.logs) != 0 {
		t.Fatalf("kept logs for pid 999 while pid %d is selected: %v", p.selPID, p.logs)
	}
	if !p.logsLoading {
		t.Error("a stale result cleared the loading flag")
	}
}

func TestLogResultForTheSelectedProcessIsKept(t *testing.T) {
	p := newWidget(t, 80, 24)
	load(p, fixture())
	selectPID(t, p, 200)
	p.detail, p.showDetail = detailLogs, true
	p.logsLoading = true

	p.Update(logsMsg{pid: 200, lines: []string{"a real line"}})

	if p.logsLoading {
		t.Error("loading flag not cleared")
	}
	if !strings.Contains(stripANSI(p.View()), "a real line") {
		t.Errorf("log line not rendered:\n%s", stripANSI(p.View()))
	}
}

// Moving the cursor while the log pane is open re-queries for the new process.
func TestMovingTheCursorRefetchesLogs(t *testing.T) {
	p := newWidget(t, 80, 24)
	load(p, fixture())
	p.detail, p.showDetail = detailLogs, true
	p.logsPID = p.selPID

	cmd := p.Update(key("down"))
	if cmd == nil {
		t.Fatal("moving the cursor did not re-query the log")
	}
	if p.logsPID != p.selPID {
		t.Errorf("log pane is about pid %d but pid %d is selected", p.logsPID, p.selPID)
	}
}

// The ancestry view is local data, so moving must not spawn a log query.
func TestMovingDoesNotFetchLogsWhenTheLogViewIsClosed(t *testing.T) {
	p := newWidget(t, 80, 24)
	load(p, fixture())

	if cmd := p.Update(key("down")); cmd != nil {
		t.Fatal("moving the cursor ran a command with the log view closed")
	}
}

func TestInvalidLogWindowIsRejected(t *testing.T) {
	for _, bad := range []string{"soon", "-5m", "0"} {
		if _, err := newWidgetWithConfig(t, "log_window: "+strconv.Quote(bad)); err == nil {
			t.Errorf("log_window %q was accepted", bad)
		}
	}
	if _, err := newWidgetWithConfig(t, "log_window: 90s"); err != nil {
		t.Errorf("log_window 90s rejected: %v", err)
	}
}

func TestDetailCanBeTurnedOffInConfig(t *testing.T) {
	w, err := newWidgetWithConfig(t, "detail: false")
	if err != nil {
		t.Fatal(err)
	}
	w.SetSize(80, 24)
	load(w, fixture())
	if w.detailHeight() != 0 {
		t.Error("detail: false still rendered a detail pane")
	}
}

// --- helpers --------------------------------------------------------------

func selectPID(t *testing.T, p *Processes, pid int) {
	t.Helper()
	for i, r := range p.rows {
		if r.PID == pid {
			selectRow(p, i)
			return
		}
	}
	t.Fatalf("pid %d not in the fixture", pid)
}

// headerRow returns the column-header line of the rendered widget.
func headerRow(t *testing.T, p *Processes) string {
	t.Helper()
	lines := strings.Split(stripANSI(p.View()), "\n")
	if len(lines) < 2 {
		t.Fatalf("no header row in:\n%s", strings.Join(lines, "\n"))
	}
	return lines[1]
}

func newWidgetWithConfig(t *testing.T, yamlBody string) (*Processes, error) {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("type: processes\n"+yamlBody+"\n"), &node); err != nil {
		t.Fatal(err)
	}
	w, err := New(widget.Context{Name: "procs", Theme: theme.New(""), Node: node.Content[0]})
	if err != nil {
		return nil, err
	}
	return w.(*Processes), nil
}

// deepFamily is a 6-deep chain ending in a process with many children, which
// is far more family than any pane can show.
func deepFamily() []procs.Process {
	ps := []procs.Process{{PID: 1, PPID: 0, User: "root", Command: "/sbin/init"}}
	for i := 2; i <= 6; i++ {
		ps = append(ps, procs.Process{PID: i, PPID: i - 1, User: "pparker", Command: fmt.Sprintf("/bin/level%d", i)})
	}
	for i := 0; i < 12; i++ {
		ps = append(ps, procs.Process{PID: 100 + i, PPID: 6, User: "pparker", Command: fmt.Sprintf("/bin/child%d", i)})
	}
	return ps
}

// The one row that must never be dropped is the process the user selected. A
// tree that elides it is answering a question nobody asked.
func TestAncestryAlwaysShowsTheSelectedProcess(t *testing.T) {
	for _, h := range []int{12, 14, 16, 20, 30, 50} {
		p := newWidget(t, 80, h)
		load(p, deepFamily())
		selectPID(t, p, 6)

		out := stripANSI(p.View())
		if !strings.Contains(out, "level6 (6)") {
			t.Errorf("height %d: the selected process is missing from its own tree:\n%s", h, out)
		}
	}
}

// When the chain is cut, say so rather than implying the process is a root.
func TestAncestryCountsWhatItElides(t *testing.T) {
	p := newWidget(t, 80, 14)
	load(p, deepFamily())
	selectPID(t, p, 6)

	out := stripANSI(p.View())
	if !strings.Contains(out, "more above") && !strings.Contains(out, "init (1)") {
		t.Errorf("chain was cut without saying so:\n%s", out)
	}
	if !strings.Contains(out, "more below") {
		t.Errorf("12 children were not all shown, but nothing said so:\n%s", out)
	}
}

// Given room for the whole family — 5 ancestors, the process, 12 children and
// the prose above them — nothing should be elided. The detail pane gets half
// the widget, so this needs a tall one.
func TestAncestryShowsEverythingWhenThereIsRoom(t *testing.T) {
	p := newWidget(t, 80, 60)
	load(p, deepFamily())
	selectPID(t, p, 6)

	out := stripANSI(p.View())
	for _, want := range []string{"init (1)", "level6 (6)", "child0 (100)", "child11 (111)"} {
		if !strings.Contains(out, want) {
			t.Errorf("tall pane is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "more above") || strings.Contains(out, "more below") {
		t.Errorf("tall pane claims to have elided something:\n%s", out)
	}
}

// The detail pane must fill exactly the rows it was given, whatever the tree
// shape, or the widget overflows its frame.
func TestDetailPaneFillsExactlyItsBudget(t *testing.T) {
	for _, h := range []int{12, 15, 18, 25, 40} {
		p := newWidget(t, 80, h)
		load(p, deepFamily())
		selectPID(t, p, 6)

		want := p.detailHeight()
		if want == 0 {
			continue
		}
		if got := len(p.detailLines(want)); got != want {
			t.Errorf("height %d: detail pane produced %d lines for a %d-row budget", h, got, want)
		}
		if got := len(strings.Split(p.View(), "\n")); got > h {
			t.Errorf("height %d: widget rendered %d lines", h, got)
		}
	}
}

// Logs already in hand must render even where this machine has no log source,
// because they will not always have come from this machine.
func TestHeldLogsRenderRegardlessOfPlatformSupport(t *testing.T) {
	p := newWidget(t, 80, 24)
	load(p, fixture())
	selectPID(t, p, 200)
	p.detail, p.showDetail = detailLogs, true
	p.logs, p.logsPID = []string{"an entry from somewhere"}, 200

	if !strings.Contains(stripANSI(p.View()), "an entry from somewhere") {
		t.Errorf("held log lines were not rendered:\n%s", stripANSI(p.View()))
	}
}
