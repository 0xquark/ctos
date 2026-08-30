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

	var bar widget.Widget
	base := stub{body: barBody, lines: barLines}
	if barLines > 0 {
		bar = &sizedStub{stub: base}
	} else {
		bar = &base
	}

	m := &Model{
		cfg:   &config.Config{},
		dash:  &config.Dashboard{Bar: config.Bar{Left: []string{"vitals"}}, Rows: [][]string{{"panel"}}},
		theme: theme.New(""),
		byName: map[string]widget.Widget{
			"vitals": bar,
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
	if len(m.barLeft) != 1 {
		t.Fatalf("bar = %v, want one widget", m.barLeft)
	}
}

func TestBarIsRenderedAboveTheRows(t *testing.T) {
	m := barModel(t, "CPU 22% | MEM 67%", 1, 80, 24)

	lines := strings.Split(m.View(), "\n")
	if !strings.Contains(lines[0], "CPU 22%") {
		t.Errorf("first line should be the bar, got %q", lines[0])
	}
	// Frameless: no border characters around it.
	if strings.ContainsAny(lines[0], "╭╮│") {
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

	if got := m.sizeBar(); got > minHeight-m.footerHeight()-minRowsHeight {
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
