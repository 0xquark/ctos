package tui

import (
	"strings"
	"testing"

	"github.com/0xquark/ctos/internal/config"
	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// stub is a widget with a fixed body, used to assert layout without pulling a
// real widget's data-fetching into the test.
type stub struct {
	widget.Base
	body  string
	lines int // when non-zero, the widget implements Liner and asks for this
}

func (s *stub) Init() tea.Cmd            { return nil }
func (s *stub) Update(tea.Msg) tea.Cmd   { return nil }
func (s *stub) View() string             { return s.body }
func (s *stub) Actions() []widget.Action { return nil }

type sizedStub struct {
	stub
}

func (s *sizedStub) Lines(int) int { return s.lines }

// barModel builds a model with one bar widget and one row widget, bypassing
// the registry so the test owns exactly what each renders.
func barModel(t *testing.T, barBody string, barLines, w, h int) *Model {
	t.Helper()
	return placedBarModel(t, config.Bar{Position: config.BarTop, Start: []string{"vitals"}}, barBody, barLines, w, h)
}

// placedBarModel is barModel with the bar pinned wherever the test wants it.
func placedBarModel(t *testing.T, bar config.Bar, barBody string, barLines, w, h int) *Model {
	t.Helper()

	var barWidget widget.Widget
	base := stub{body: barBody, lines: barLines}
	if barLines > 0 {
		barWidget = &sizedStub{stub: base}
	} else {
		barWidget = &base
	}

	m := &Model{
		cfg:   &config.Config{},
		dash:  &config.Dashboard{Bar: bar, Rows: [][]string{{"panel"}}},
		theme: theme.New(""),
		byName: map[string]widget.Widget{
			"vitals": barWidget,
			"panel":  &stub{body: "panel body"},
		},
	}
	m.rebuild("")
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// The bar is chrome: tab must never land on it, or the user would be moving
// focus onto something with no actions and no cursor.
func TestBarWidgetsAreNotInTheFocusOrder(t *testing.T) {
	m := barModel(t, "vitals", 1, 80, 24)

	if len(m.order) != 1 || m.names[0] != "panel" {
		t.Fatalf("focus order = %v, want just the row widget", m.names)
	}
	for range 5 {
		m.moveFocus(1)
		if m.focusedName() != "panel" {
			t.Fatalf("focus reached %q, which is in the bar", m.focusedName())
		}
	}
}

// A bar widget still has to be started and still has to receive its own
// results, even though it is outside the grid.
func TestBarWidgetsAreStartedAndAddressed(t *testing.T) {
	m := barModel(t, "vitals", 1, 80, 24)
	if _, ok := m.byName["vitals"]; !ok {
		t.Fatal("the bar widget should be in byName so messages can reach it")
	}
	if len(m.barStart) != 1 {
		t.Fatalf("bar = %v, want one widget", m.barStart)
	}
}

func TestBarIsRenderedAboveTheRows(t *testing.T) {
	m := barModel(t, "CPU 22% | MEM 67%", 1, 80, 24)

	lines := strings.Split(m.View(), "\n")
	if !strings.Contains(lines[0], "CPU 22%") {
		t.Errorf("first line should be the bar, got %q", lines[0])
	}
	// Frameless: no border characters around it.
	c := theme.New("").Chrome
	if strings.ContainsAny(lines[0], c.TopLeft+c.TopRight+c.Vertical) {
		t.Errorf("the bar should have no frame, got %q", lines[0])
	}
}

// The rows have to give back exactly the space the bar took, or the dashboard
// runs off the bottom of the terminal.
func TestBarHeightComesOutOfTheRows(t *testing.T) {
	const h = 24
	for _, barLines := range []int{1, 2, 3} {
		body := strings.TrimSuffix(strings.Repeat("x\n", barLines), "\n")
		m := barModel(t, body, barLines, 80, h)

		if got := lipgloss.Height(m.View()); got != h {
			t.Errorf("bar of %d lines: view is %d lines, want %d", barLines, got, h)
		}
	}
}

// Without a bar the dashboard must be exactly as it was before the feature.
func TestNoBarChangesNothing(t *testing.T) {
	m := barModel(t, "", 0, 80, 24)
	m.dash.Bar = config.Bar{}
	m.rebuild("")

	if got := lipgloss.Height(m.View()); got != 24 {
		t.Errorf("view is %d lines, want 24", got)
	}
	if strings.HasPrefix(m.View(), "\n") {
		t.Error("no bar should mean no leading blank line")
	}
}

// A widget that asks for more than the ceiling is capped, so a broken or
// runaway bar cannot push the dashboard off the screen.
func TestBarIsCappedAtMaxLines(t *testing.T) {
	m := barModel(t, strings.Repeat("x\n", 9), 9, 80, 24)

	if got := m.barHeight(); got > maxBarLines {
		t.Errorf("bar asked for %d lines, want at most %d", got, maxBarLines)
	}
}

// On a terminal short enough that the bar and the dashboard cannot both have
// what they want, the dashboard keeps a row.
func TestBarLeavesRoomForOneRow(t *testing.T) {
	m := barModel(t, "x\ny\nz", 3, 80, minHeight)

	if _, got := m.sizeBar(); got > minHeight-m.footerHeight()-minRowsHeight {
		t.Errorf("bar took %d lines on a %d-line terminal", got, minHeight)
	}
}

// A widget with no opinion about its height gets one line rather than the
// whole screen.
func TestLinesForDefaults(t *testing.T) {
	plain := &stub{body: "x"}
	if got := widget.LinesFor(plain, 80, 1, maxBarLines); got != 1 {
		t.Errorf("LinesFor(no Liner) = %d, want the default of 1", got)
	}

	asks := &sizedStub{stub: stub{lines: 2}}
	if got := widget.LinesFor(asks, 80, 1, maxBarLines); got != 2 {
		t.Errorf("LinesFor = %d, want 2", got)
	}

	greedy := &sizedStub{stub: stub{lines: 99}}
	if got := widget.LinesFor(greedy, 80, 1, maxBarLines); got != maxBarLines {
		t.Errorf("LinesFor = %d, want the %d ceiling", got, maxBarLines)
	}

	zero := &sizedStub{stub: stub{lines: 0}}
	if got := widget.LinesFor(zero, 80, 1, maxBarLines); got != 1 {
		t.Errorf("LinesFor = %d, want at least 1", got)
	}
}

// A bar can be pinned to any edge. Wherever it goes, the view still fills the
// terminal exactly: the space it takes has to come out of the grid, not out of
// the screen.
func TestBarFitsTheTerminalAtEveryPosition(t *testing.T) {
	const w, h = 80, 24

	for _, pos := range []config.BarPosition{config.BarTop, config.BarBottom, config.BarLeft, config.BarRight} {
		t.Run(string(pos), func(t *testing.T) {
			m := placedBarModel(t, config.Bar{
				Position: pos,
				Start:    []string{"vitals"},
			}, "CPU 22%", 1, w, h)

			out := m.View()
			if got := lipgloss.Height(out); got != h {
				t.Errorf("view is %d lines, want %d", got, h)
			}
			for i, line := range strings.Split(out, "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Fatalf("line %d is %d cells wide, want at most %d", i, got, w)
				}
			}
			if !strings.Contains(out, "CPU 22%") {
				t.Error("the bar's content is missing from the view")
			}
		})
	}
}

// Top and bottom are the same strip at opposite ends: the bar is the first
// line, or the last one above the footer.
func TestHorizontalBarSitsAtItsEnd(t *testing.T) {
	top := placedBarModel(t, config.Bar{Position: config.BarTop, Start: []string{"vitals"}}, "VITALS", 1, 80, 24)
	if first, _, _ := strings.Cut(top.View(), "\n"); !strings.Contains(first, "VITALS") {
		t.Errorf("top bar: first line = %q", first)
	}

	bottom := placedBarModel(t, config.Bar{Position: config.BarBottom, Start: []string{"vitals"}}, "VITALS", 1, 80, 24)
	lines := strings.Split(bottom.View(), "\n")
	if strings.Contains(lines[0], "VITALS") {
		t.Error("a bottom bar should not be drawn at the top")
	}
	// The footer is the shell's own line and stays below everything, so the
	// bar is the last line before it.
	barLine := len(lines) - 1 - bottom.footerHeight()
	if !strings.Contains(lines[barLine], "VITALS") {
		t.Errorf("bottom bar: line %d = %q, want the strip above the footer", barLine, lines[barLine])
	}
}

// A vertical bar takes columns rather than lines, and the frames beside it
// start where it ends.
func TestVerticalBarTakesColumnsFromTheGrid(t *testing.T) {
	const w = 80

	for _, pos := range []config.BarPosition{config.BarLeft, config.BarRight} {
		t.Run(string(pos), func(t *testing.T) {
			m := placedBarModel(t, config.Bar{
				Position: pos,
				Width:    20,
				Start:    []string{"vitals"},
			}, "VITALS", 1, w, 24)

			if got, _ := m.sizeBar(); got != 20 {
				t.Fatalf("bar took %d columns, want 20", got)
			}

			// The row widget is framed in what is left over.
			panel := m.byName["panel"].(*stub)
			if got := panel.W + FrameOverheadX; got != w-20 {
				t.Errorf("the grid got %d columns, want %d", got, w-20)
			}

			line := strings.Split(m.View(), "\n")[0]
			atStart := strings.Index(line, "VITALS") < 20
			if want := pos == config.BarLeft; atStart != want {
				t.Errorf("%s bar: the strip is on the wrong side of %q", pos, line)
			}
		})
	}
}

// The trailing group of a vertical bar is pinned to the bottom of the column,
// the way the trailing group of a strip is pinned to its right.
func TestVerticalBarPinsItsTrailingGroupToTheBottom(t *testing.T) {
	m := placedBarModel(t, config.Bar{
		Position: config.BarLeft,
		Start:    []string{"vitals"},
		End:      []string{"clock"},
	}, "VITALS", 1, 80, 24)
	m.byName["clock"] = &stub{body: "12:00:00"}
	m.rebuild("")

	lines := strings.Split(m.columnView(), "\n")
	if !strings.Contains(lines[0], "VITALS") {
		t.Errorf("leading group should be at the top, line 0 = %q", lines[0])
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "12:00:00") {
		t.Errorf("trailing group should be at the bottom, last line = %q", last)
	}
}

// A vertical bar is chrome too: on a terminal too narrow to carry both, the
// grid keeps its columns and the bar is what disappears.
func TestVerticalBarGivesWayOnANarrowTerminal(t *testing.T) {
	m := placedBarModel(t, config.Bar{
		Position: config.BarLeft,
		Width:    40,
		Start:    []string{"vitals"},
	}, "VITALS", 1, minWidth, 24)

	cols, _ := m.sizeBar()
	if cols > minWidth-minRowsWidth {
		t.Errorf("bar took %d of %d columns, leaving the grid %d", cols, minWidth, minWidth-cols)
	}
	if got := lipgloss.Height(m.View()); got != 24 {
		t.Errorf("view is %d lines, want 24", got)
	}
}

// The bar is not in the grid and the arrows are busy moving a widget around
// it, so layout mode gives it a key of its own that cycles the four edges.
func TestLayoutModeCyclesTheBar(t *testing.T) {
	m := barModel(t, "VITALS", 1, 80, 24)
	m.enterLayoutMode()

	want := []config.BarPosition{config.BarRight, config.BarBottom, config.BarLeft, config.BarTop}
	for _, pos := range want {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
		if m.dash.Bar.Position != pos {
			t.Fatalf("after b: position = %q, want %q", m.dash.Bar.Position, pos)
		}
		if got := lipgloss.Height(m.View()); got != 24 {
			t.Errorf("%s: view is %d lines, want 24", pos, got)
		}
	}
}

// Moving the bar is an unsaved change like any other, and esc puts it back.
func TestCancellingLayoutModeRestoresTheBar(t *testing.T) {
	m := barModel(t, "VITALS", 1, 80, 24)
	m.enterLayoutMode()

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if !m.layoutDirty() {
		t.Error("moving the bar should count as an unsaved change")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.dash.Bar.Position != config.BarTop {
		t.Errorf("esc left the bar at %q, want it back at %q", m.dash.Bar.Position, config.BarTop)
	}
	if m.layoutMode {
		t.Error("esc should leave layout mode")
	}
}

// On a dashboard with no bar the key says so rather than doing nothing.
func TestCyclingWithNoBarSaysSo(t *testing.T) {
	m := barModel(t, "", 0, 80, 24)
	m.dash.Bar = config.Bar{}
	m.rebuild("")
	m.enterLayoutMode()

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if m.status == "" {
		t.Error("cycling a dashboard with no bar should explain itself")
	}
}
