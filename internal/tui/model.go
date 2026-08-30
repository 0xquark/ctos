// Package tui is the dashboard shell: it owns layout, focus, global keys and
// the frame around each widget. It must not import a concrete widget package.
package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/0xquark/ctos/internal/config"
	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// minWidth and minHeight are the smallest terminal we try to draw in.
const (
	minWidth  = 40
	minHeight = 12
)

// maxBarLines bounds the status bar. Past three lines it has stopped being
// chrome and become a widget that happens to have no border.
const maxBarLines = 3

// minRowsHeight is the space the dashboard proper keeps whatever the bar asks
// for: one row, framed.
const minRowsHeight = FrameOverheadY + 1

// minRowsWidth is the same promise sideways, for a bar down one side: the grid
// keeps enough columns to frame something legible, and a vertical bar wider
// than the dashboard it annotates gives up its own columns instead.
const minRowsWidth = 24

// minBarWidth is the narrowest a vertical bar is worth drawing.
const minBarWidth = 8

// barGutter is the blank column kept between a vertical bar and the grid, so
// the strip never runs into the border it sits beside.
const barGutter = 1

// Model is the root bubbletea model.
type Model struct {
	cfg   *config.Config
	dash  *config.Dashboard
	theme theme.Theme

	// byName owns the widgets. rows and order hold the same pointers,
	// rebuilt whenever the layout changes, so rearranging never
	// reconstructs a widget and never loses its state.
	byName map[string]widget.Widget
	rows   [][]widget.Widget
	order  []widget.Widget
	names  []string

	// barStart and barEnd hold the frameless status widgets pinned to one
	// edge of the screen: start is the leading group (the left of a
	// horizontal bar, the top of a vertical one), end the trailing one.
	// They are deliberately not in order — the bar never takes focus, so
	// tab cannot land on something the user cannot act on.
	barStart, barEnd []widget.Widget

	focus    int
	w, h     int
	ready    bool
	showHelp bool

	// Layout mode state. beforeEdit and barBeforeEdit are what a cancel
	// restores.
	layoutMode    bool
	beforeEdit    [][]string
	barBeforeEdit config.Bar
	status        string
}

// New builds every widget on the dashboard and returns the root model.
func New(cfg *config.Config, dash *config.Dashboard) (*Model, error) {
	th := theme.New(cfg.Theme.Accent)

	m := &Model{
		cfg:    cfg,
		dash:   dash,
		theme:  th,
		byName: make(map[string]widget.Widget, len(dash.Widgets)),
	}

	for name, spec := range dash.Widgets {
		w, err := widget.New(spec.Type, widget.Context{
			Name:           name,
			Node:           spec.Node,
			Theme:          th,
			Editor:         cfg.ResolveEditor(),
			DefaultRefresh: cfg.DefaultRefresh(),
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dash.Path, err)
		}
		m.byName[name] = w
	}

	m.rebuild("")
	if len(m.order) == 0 {
		return nil, fmt.Errorf("%s: dashboard has no widgets in \"rows:\"", dash.Path)
	}
	m.order[0].Focus()
	return m, nil
}

// rebuild derives the widget grid and focus order from dash.Rows. Focus follows
// keepFocus by name when given, so a widget stays selected as it is moved.
func (m *Model) rebuild(keepFocus string) {
	m.rows = m.rows[:0]
	m.order = m.order[:0]
	m.names = m.names[:0]
	m.barStart = m.pick(m.dash.Bar.Start)
	m.barEnd = m.pick(m.dash.Bar.End)

	for _, row := range m.dash.Rows {
		built := make([]widget.Widget, 0, len(row))
		for _, name := range row {
			w, ok := m.byName[name]
			if !ok {
				continue // validated at load; defensive only
			}
			built = append(built, w)
			m.order = append(m.order, w)
			m.names = append(m.names, name)
		}
		m.rows = append(m.rows, built)
	}

	if keepFocus != "" {
		for i, name := range m.names {
			if name == keepFocus {
				m.focus = i
				break
			}
		}
	}
	if m.focus >= len(m.order) {
		m.focus = max(0, len(m.order)-1)
	}
	m.resize()
}

// focusedName is the config name of the focused widget.
func (m *Model) focusedName() string {
	if m.focus < 0 || m.focus >= len(m.names) {
		return ""
	}
	return m.names[m.focus]
}

// Init starts every widget.
func (m *Model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.byName))
	for _, w := range m.byName {
		if cmd := w.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// Update routes messages. Key messages go only to the focused widget, a
// widget's own results (widget.Addressed) go only to the widget that asked for
// them, and anything else — resize, and messages from outside any widget — is
// broadcast.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	// The status bar is as tall as it currently needs to be, so the rows
	// below it are re-measured after every message rather than only when
	// the terminal changes size. Without this a bar that grows from one
	// line to two on its first sample would overlap the row beneath it.
	if !m.dash.Bar.Empty() {
		m.resize()
	}
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ready = true
		m.resize()
		return nil

	case tea.KeyMsg:
		// In layout mode the arrows rearrange widgets rather than
		// scrolling inside one, so nothing falls through.
		if m.layoutMode {
			return m.layoutKey(msg)
		}
		// A widget typing a filter query owns the whole keyboard; otherwise
		// "q" would quit mid-word. ctrl+c still gets out.
		if widget.Grabbing(m.focused()) {
			if msg.String() == "ctrl+c" {
				return tea.Quit
			}
			return m.updateFocused(msg)
		}
		if cmd, handled := m.globalKey(msg); handled {
			return cmd
		}
		return m.updateFocused(msg)

	case widget.Addressed:
		return m.deliver(msg.Name, msg.Msg)
	}

	return m.broadcast(msg)
}

// globalKey handles dashboard-level bindings. It reports whether the key was
// consumed, so unhandled keys can fall through to the focused widget.
func (m *Model) globalKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c", "q":
		return tea.Quit, true

	case "tab":
		m.moveFocus(1)
		return nil, true

	case "shift+tab":
		m.moveFocus(-1)
		return nil, true

	case "ctrl+l":
		m.enterLayoutMode()
		return nil, true

	case "?":
		m.showHelp = !m.showHelp
		m.resize()
		return nil, true

	case "esc":
		if m.showHelp {
			m.showHelp = false
			m.resize()
			return nil, true
		}
		return nil, false

	case "enter":
		if w := m.focused(); w != nil {
			if actions := w.Actions(); len(actions) > 0 && actions[0].Run != nil {
				return actions[0].Run(), true
			}
		}
		return nil, true
	}
	return nil, false
}

// enterLayoutMode snapshots the layout so the edit can be cancelled.
func (m *Model) enterLayoutMode() {
	m.layoutMode = true
	m.showHelp = false
	m.beforeEdit = clone(m.dash.Rows)
	m.barBeforeEdit = m.dash.Bar
	m.status = ""
	m.resize()
}

// cycleBar moves the status bar to the next edge, in the order a reader would
// try them: across the top, down the right, across the bottom, down the left.
//
// The bar is not part of the grid and the arrow keys are busy moving a widget
// around it, so it gets a key of its own rather than a place in the same
// traversal. Cycling beats four bindings: there are only four edges, and
// seeing each one is the point.
func (m *Model) cycleBar() {
	if m.dash.Bar.Empty() {
		m.status = "no bar on this dashboard"
		return
	}
	order := []config.BarPosition{config.BarTop, config.BarRight, config.BarBottom, config.BarLeft}
	at := max(0, slices.Index(order, m.dash.Bar.Position))
	m.dash.Bar.Position = order[(at+1)%len(order)]
	m.status = ""
	m.resize()
}

// layoutKey handles rearranging. Every move keeps the same widget focused, so
// the user can push it across the dashboard without re-selecting it.
func (m *Model) layoutKey(msg tea.KeyMsg) tea.Cmd {
	name := m.focusedName()
	if name == "" {
		m.layoutMode = false
		return nil
	}

	before := m.dash.Rows

	switch msg.String() {
	case "left", "h":
		m.dash.Rows = MoveLeft(m.dash.Rows, name)
	case "right", "l":
		m.dash.Rows = MoveRight(m.dash.Rows, name)
	case "up", "k":
		m.dash.Rows = MoveUp(m.dash.Rows, name)
	case "down", "j":
		m.dash.Rows = MoveDown(m.dash.Rows, name)
	case "enter":
		m.dash.Rows = SplitOut(m.dash.Rows, name)
	case "tab":
		// Pick a different widget to move.
		m.moveFocus(1)
		return nil
	case "shift+tab":
		m.moveFocus(-1)
		return nil

	case "b":
		m.cycleBar()
		return nil

	case "s":
		return m.saveLayout()

	case "esc":
		m.dash.Rows = m.beforeEdit
		m.dash.Bar = m.barBeforeEdit
		m.layoutMode = false
		m.status = ""
		m.rebuild(name)
		return nil

	case "ctrl+l":
		// Leave the arrangement in place for this session, unsaved.
		m.layoutMode = false
		if m.layoutDirty() {
			m.status = "layout changed — not saved"
		}
		m.resize()
		return nil

	case "ctrl+c", "q":
		return tea.Quit
	}

	if !sameLayout(before, m.dash.Rows) {
		m.status = ""
		m.rebuild(name)
	}
	return nil
}

// saveLayout writes the arrangement back to the dashboard file.
func (m *Model) saveLayout() tea.Cmd {
	if err := config.SaveLayout(m.dash.Path, m.dash.Rows, m.dash.Bar); err != nil {
		m.status = "save failed: " + err.Error()
		return nil
	}
	m.beforeEdit = clone(m.dash.Rows)
	m.barBeforeEdit = m.dash.Bar
	m.layoutMode = false
	m.status = "layout saved to " + m.dash.Path
	m.resize()
	return nil
}

// layoutDirty reports whether anything layout mode owns has been changed.
func (m *Model) layoutDirty() bool {
	return !sameLayout(m.dash.Rows, m.beforeEdit) ||
		m.dash.Bar.Position != m.barBeforeEdit.Position
}

// sameLayout reports whether two arrangements are identical.
func sameLayout(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

func (m *Model) focused() widget.Widget {
	if m.focus < 0 || m.focus >= len(m.order) {
		return nil
	}
	return m.order[m.focus]
}

func (m *Model) moveFocus(delta int) {
	if len(m.order) == 0 {
		return
	}
	m.order[m.focus].Blur()
	m.focus = (m.focus + delta + len(m.order)) % len(m.order)
	m.order[m.focus].Focus()
	if m.showHelp || m.layoutMode {
		// The footer's height depends on the focused widget.
		m.resize()
	}
}

// updateFocused delivers a message to the focused widget only.
func (m *Model) updateFocused(msg tea.Msg) tea.Cmd {
	return m.deliver(m.focusedName(), msg)
}

// deliver routes a message to one widget by name. A message for a widget that
// is not on this dashboard is dropped, which is what makes a late result from
// a removed widget harmless.
func (m *Model) deliver(name string, msg tea.Msg) tea.Cmd {
	w, ok := m.byName[name]
	if !ok {
		return nil
	}
	return w.Update(msg)
}

// broadcast delivers a message to every widget.
func (m *Model) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, w := range m.byName {
		if cmd := w.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// footerHeight is how many rows the footer occupies, including any status line.
func (m *Model) footerHeight() int {
	n := helpHeight(m.showHelp, m.focused())
	if m.layoutMode {
		n = 2
	}
	if m.status != "" && !m.layoutMode {
		n++
	}
	return n
}

// resize recomputes every widget's content area.
//
// The bar is sized first and the rows get what is left, because the bar's size
// is not the layout's to choose: a strip is however many lines it needs to say
// what it has, and a column is however many the dashboard asked to spend.
func (m *Model) resize() {
	if !m.ready {
		return
	}
	barCols, barLines := m.sizeBar()
	boxes := Layout(m.dash.Rows, m.w-barCols, m.h-m.footerHeight()-barLines)

	for i, row := range m.rows {
		for j, w := range row {
			if i < len(boxes) && j < len(boxes[i]) {
				iw, ih := boxes[i][j].Inner()
				w.SetSize(iw, ih)
			}
		}
	}
}

// pick resolves widget names to the widgets themselves, skipping any the
// dashboard does not define. Names are validated at load; this is defensive.
func (m *Model) pick(names []string) []widget.Widget {
	out := make([]widget.Widget, 0, len(names))
	for _, name := range names {
		if w, ok := m.byName[name]; ok {
			out = append(out, w)
		}
	}
	return out
}

// barWidgets is every widget in the bar, both groups.
func (m *Model) barWidgets() []widget.Widget {
	return append(slices.Clone(m.barStart), m.barEnd...)
}

// barHeight is how many lines a horizontal strip takes: the most any one
// widget in it wants, since they share the line rather than stacking.
//
// A bar is chrome, so it never grows so far that the dashboard beneath it has
// nowhere to go.
func (m *Model) barHeight() int {
	all := m.barWidgets()
	if len(all) == 0 || !m.dash.Bar.Horizontal() {
		return 0
	}

	h := 0
	for _, w := range all {
		h = max(h, widget.LinesFor(w, m.w, 1, maxBarLines))
	}
	return min(h, max(0, m.h-m.footerHeight()-minRowsHeight))
}

// barWidth is how many columns a vertical bar takes, including the indent.
//
// The height of a horizontal strip is its content's to choose, because a line
// of vitals either fits or does not. A column's width is not: it is how much
// of the screen the reader is willing to spend on chrome, so it comes from
// "width:" and is only ever clamped downwards — never so far that the grid
// beside it stops being a dashboard.
func (m *Model) barWidth() int {
	if len(m.barWidgets()) == 0 || m.dash.Bar.Horizontal() {
		return 0
	}
	w := min(m.dash.Bar.Columns(), max(0, m.w-minRowsWidth))
	if w < minBarWidth {
		return 0
	}
	return w
}

// sizeBar hands every bar widget its room and reports what the bar took from
// the dashboard: columns down one side, or lines across an end. Only one is
// ever non-zero — a bar occupies an edge, not a corner.
func (m *Model) sizeBar() (cols, lines int) {
	if len(m.barWidgets()) == 0 {
		return 0, 0
	}
	if m.dash.Bar.Horizontal() {
		return 0, m.sizeStrip()
	}
	return m.sizeColumn(), 0
}

// sizeStrip sizes a top or bottom bar and returns its height.
//
// The trailing group is measured first and the leading group gets what is
// left, because the trailing group is the fixed part: a clock is as wide as a
// clock, while a vitals strip will fill whatever it is given. Doing it the
// other way round would leave the clock to be squeezed by a value that could
// have given up a digit instead.
func (m *Model) sizeStrip() int {
	// Each widget is offered a single line first, and asked what it would
	// rather have. Offering the ceiling instead would put an adaptive
	// widget in the position of describing what it would do with three
	// lines it is not going to get — and a strip that answers "one" only
	// because it was asked while one line tall is the answer we want.
	for _, w := range m.barWidgets() {
		w.SetSize(m.w, 1)
	}
	h := m.barHeight()
	if h == 0 {
		return 0
	}

	// The trailing group may take at most a third of the strip: it is a
	// trailing detail, not the point of the bar.
	for _, w := range m.barEnd {
		w.SetSize(m.w/3, h)
	}
	end := m.renderGroup(m.barEnd)

	startW := m.w
	if end != "" {
		startW = max(0, m.w-lipgloss.Width(end)-barGroupGap)
	}
	for _, w := range m.barStart {
		w.SetSize(startW, h)
	}
	return h
}

// sizeColumn sizes a left or right bar and returns its width.
//
// The same rule applies turned ninety degrees: the trailing group is measured
// first, so a clock pinned to the bottom keeps the one line it wants and the
// panel above it takes the rest. The cap is half the column rather than a
// third of the line, because a vertical bar has fewer widgets sharing it and
// an even split is a reasonable thing to ask for.
func (m *Model) sizeColumn() int {
	w := m.barWidth()
	if w == 0 {
		return 0
	}
	content := barContentWidth(w)
	colH := max(0, m.h-m.footerHeight())

	for _, wd := range m.barWidgets() {
		wd.SetSize(content, colH)
	}

	endH := 0
	for _, wd := range m.barEnd {
		endH += widget.LinesFor(wd, content, 1, colH)
	}
	endH = min(endH, colH/2)

	shareLines(m.barEnd, content, endH)
	shareLines(m.barStart, content, colH-endH)
	return w
}

// shareLines divides h lines between the widgets of one vertical group. The
// remainder goes to the first widgets, so no line is lost to rounding.
func shareLines(ws []widget.Widget, w, h int) {
	if len(ws) == 0 {
		return
	}
	each, extra := h/len(ws), h%len(ws)
	for i, wd := range ws {
		n := each
		if i < extra {
			n++
		}
		wd.SetSize(w, max(0, n))
	}
}

// barGroupGap is the least space kept between the two groups of a horizontal
// bar, so a full strip does not run its last value into the clock.
const barGroupGap = 2

// renderGroup draws one side of a horizontal bar, trimming the padding a
// widget may have added: the strip positions its own content, so a widget that
// centred itself in the width it was given would arrive pre-padded and could
// not be aligned.
func (m *Model) renderGroup(ws []widget.Widget) string {
	parts := make([]string, 0, len(ws))
	for _, w := range ws {
		if v := strings.TrimSpace(w.View()); v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, m.theme.FaintStyle().Render(" │ "))
}

// barView renders the bar in whichever direction it runs.
func (m *Model) barView() string {
	if m.dash.Bar.Horizontal() {
		return m.stripView()
	}
	return m.columnView()
}

// stripView renders a top or bottom bar: the leading group from the left edge,
// the trailing group flush against the right.
func (m *Model) stripView() string {
	start, end := m.renderGroup(m.barStart), m.renderGroup(m.barEnd)
	switch {
	case start == "" && end == "":
		return ""
	case end == "":
		return " " + start
	case start == "":
		return lipgloss.PlaceHorizontal(m.w, lipgloss.Right, end)
	}

	gap := max(barGroupGap, m.w-lipgloss.Width(start)-lipgloss.Width(end)-indent)
	return lipgloss.JoinHorizontal(lipgloss.Top, " "+start, strings.Repeat(" ", gap), end)
}

// columnView renders a left or right bar: the leading group flush to the top
// of the column, the trailing group pushed to the bottom.
func (m *Model) columnView() string {
	w := m.barWidth()
	if w == 0 {
		return ""
	}
	start, end := m.stackGroup(m.barStart), m.stackGroup(m.barEnd)
	if len(start)+len(end) == 0 {
		return ""
	}
	colH := max(0, m.h-m.footerHeight())

	out := slices.Clone(start)
	for len(out)+len(end) < colH {
		out = append(out, "")
	}
	out = append(out, end...)
	if len(out) > colH {
		out = out[:colH]
	}

	// Every line is cut and padded to exactly the column, so the grid beside
	// it starts in the same place on every row and a widget that overran
	// the width it was given cannot push a border sideways.
	lead, trail := strings.Repeat(" ", indent), strings.Repeat(" ", barGutter)
	for i, line := range out {
		out[i] = lead + padLine(line, barContentWidth(w)) + trail
	}
	return strings.Join(out, "\n")
}

// stackGroup renders one group of a vertical bar as its individual lines.
// Widgets in a column stack, where widgets in a strip sit side by side.
func (m *Model) stackGroup(ws []widget.Widget) []string {
	var out []string
	for _, w := range ws {
		v := strings.TrimRight(w.View(), "\n")
		if strings.TrimSpace(v) == "" {
			continue
		}
		out = append(out, strings.Split(v, "\n")...)
	}
	return out
}

// barContentWidth is what a vertical bar's widgets get: the column, less the
// indent that lines them up with the frames and the gutter that keeps them off
// the border.
func barContentWidth(col int) int { return max(0, col-indent-barGutter) }

// padLine cuts or pads one line to exactly w display cells, counting escape
// sequences as the zero width they occupy.
func padLine(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	if d := w - lipgloss.Width(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// indent is the leading space the dashboard's widgets start their content
// with, matched here so the bar lines up with the frames beside it.
const indent = 1

// View renders the whole dashboard.
func (m *Model) View() string {
	if !m.ready {
		return "starting ctOS…"
	}
	if m.w < minWidth || m.h < minHeight {
		return m.theme.DimStyle().Render(fmt.Sprintf(
			"terminal too small\n\nctOS needs at least %d×%d, this one is %d×%d",
			minWidth, minHeight, m.w, m.h))
	}

	bar := m.barView()
	barCols, barLines := 0, 0
	switch {
	case bar == "":
	case m.dash.Bar.Horizontal():
		barLines = lipgloss.Height(bar)
	default:
		barCols = lipgloss.Width(bar)
	}
	boxes := Layout(m.dash.Rows, m.w-barCols, m.h-m.footerHeight()-barLines)

	rendered := make([]string, 0, len(m.rows))
	for i, row := range m.rows {
		cells := make([]string, 0, len(row))
		for j, w := range row {
			cells = append(cells, Frame(
				m.theme, w.Title(), m.frameState(w),
				boxes[i][j].W, boxes[i][j].H, w.View(),
			))
		}
		rendered = append(rendered, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}

	dashboard := strings.TrimRight(lipgloss.JoinVertical(lipgloss.Left, rendered...), "\n")
	return m.place(dashboard, bar) + "\n" + m.footerView()
}

// place puts the bar on its edge. The footer is not an edge the bar can take:
// it is the shell's own line and always sits below everything.
func (m *Model) place(dashboard, bar string) string {
	if bar == "" {
		return dashboard
	}
	switch m.dash.Bar.Position {
	case config.BarBottom:
		return dashboard + "\n" + bar
	case config.BarLeft:
		return lipgloss.JoinHorizontal(lipgloss.Top, bar, dashboard)
	case config.BarRight:
		return lipgloss.JoinHorizontal(lipgloss.Top, dashboard, bar)
	default:
		return bar + "\n" + dashboard
	}
}

// frameState decides how a widget's border is drawn.
func (m *Model) frameState(w widget.Widget) FrameState {
	if w != m.focused() {
		return FrameIdle
	}
	if m.layoutMode {
		return FrameMoving
	}
	return FrameFocused
}

// footerView renders the key hints, plus any transient status message.
func (m *Model) footerView() string {
	if m.layoutMode {
		return layoutFooter(m.theme, m.focusedName(), m.layoutDirty(), !m.dash.Bar.Empty())
	}

	out := footer(m.theme, m.focused(), m.showHelp)
	if m.status != "" {
		out = " " + m.theme.DimStyle().Render(m.status) + "\n" + out
	}
	return out
}
