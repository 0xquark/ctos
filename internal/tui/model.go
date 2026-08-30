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

	// barLeft and barRight hold the frameless status widgets pinned above
	// the rows. They are deliberately not in order: the bar never takes
	// focus, so tab cannot land on something the user cannot act on.
	barLeft, barRight []widget.Widget

	focus    int
	w, h     int
	ready    bool
	showHelp bool

	// Layout mode state. beforeEdit is the layout to restore on cancel.
	layoutMode bool
	beforeEdit [][]string
	status     string
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
	m.barLeft = m.pick(m.dash.Bar.Left)
	m.barRight = m.pick(m.dash.Bar.Right)

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

	case "s":
		return m.saveLayout()

	case "esc":
		m.dash.Rows = m.beforeEdit
		m.layoutMode = false
		m.status = ""
		m.rebuild(name)
		return nil

	case "ctrl+l":
		// Leave the arrangement in place for this session, unsaved.
		m.layoutMode = false
		if !sameLayout(m.dash.Rows, m.beforeEdit) {
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
	if err := config.SaveRows(m.dash.Path, m.dash.Rows); err != nil {
		m.status = "save failed: " + err.Error()
		return nil
	}
	m.beforeEdit = clone(m.dash.Rows)
	m.layoutMode = false
	m.status = "layout saved to " + m.dash.Path
	m.resize()
	return nil
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
// The bar is sized first and the rows get what is left, because the bar's
// height is not the layout's to choose: it is however many lines the strip
// needs to say what it has.
func (m *Model) resize() {
	if !m.ready {
		return
	}
	boxes := Layout(m.dash.Rows, m.w, m.h-m.footerHeight()-m.sizeBar())

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
	return append(slices.Clone(m.barLeft), m.barRight...)
}

// barHeight is how many lines the strip takes: the most any one widget in it
// wants, since they share the line rather than stacking.
//
// A bar is chrome, so it never grows so far that the dashboard beneath it has
// nowhere to go.
func (m *Model) barHeight() int {
	all := m.barWidgets()
	if len(all) == 0 {
		return 0
	}

	h := 0
	for _, w := range all {
		h = max(h, widget.LinesFor(w, m.w, 1, maxBarLines))
	}
	return min(h, max(0, m.h-m.footerHeight()-minRowsHeight))
}

// sizeBar hands each bar widget its room and returns the height of the strip.
//
// The right-hand group is measured first and the left-hand group gets what is
// left, because the right is the fixed part: a clock is as wide as a clock,
// while a vitals strip will fill whatever it is given. Doing it the other way
// round would leave the clock to be squeezed by a value that could have given
// up a digit instead.
func (m *Model) sizeBar() int {
	all := m.barWidgets()
	if len(all) == 0 {
		return 0
	}

	// Each widget is offered the ceiling first, so what it reports back is
	// what it wants rather than what it was last given.
	for _, w := range all {
		w.SetSize(m.w, maxBarLines)
	}
	h := m.barHeight()
	if h == 0 {
		return 0
	}

	// The right group may take at most a third of the strip: it is a
	// trailing detail, not the point of the bar.
	for _, w := range m.barRight {
		w.SetSize(m.w/3, h)
	}
	right := m.renderGroup(m.barRight)

	leftW := m.w
	if right != "" {
		leftW = max(0, m.w-lipgloss.Width(right)-barGroupGap)
	}
	for _, w := range m.barLeft {
		w.SetSize(leftW, h)
	}
	return h
}

// barGroupGap is the least space kept between the two groups, so a full bar
// does not run its last value into the clock.
const barGroupGap = 2

// renderGroup draws one side of the bar, trimming the padding a widget may
// have added: the strip positions its own content, so a widget that centred
// itself in the width it was given would arrive pre-padded and could not be
// aligned.
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

// barView renders the strip: the left group from the left edge, the right
// group flush against the right.
func (m *Model) barView() string {
	left, right := m.renderGroup(m.barLeft), m.renderGroup(m.barRight)
	switch {
	case left == "" && right == "":
		return ""
	case right == "":
		return " " + left
	case left == "":
		return lipgloss.PlaceHorizontal(m.w, lipgloss.Right, right)
	}

	gap := max(barGroupGap, m.w-lipgloss.Width(left)-lipgloss.Width(right)-indent)
	return lipgloss.JoinHorizontal(lipgloss.Top, " "+left, strings.Repeat(" ", gap), right)
}

// indent is the leading space the dashboard's widgets start their content
// with, matched here so the bar lines up with the frames below it.
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
	barH := 0
	if bar != "" {
		barH = lipgloss.Height(bar)
	}
	boxes := Layout(m.dash.Rows, m.w, m.h-m.footerHeight()-barH)

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

	dashboard := lipgloss.JoinVertical(lipgloss.Left, rendered...)
	out := strings.TrimRight(dashboard, "\n") + "\n" + m.footerView()
	if bar != "" {
		out = bar + "\n" + out
	}
	return out
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
		return layoutFooter(m.theme, m.focusedName(), !sameLayout(m.dash.Rows, m.beforeEdit))
	}

	out := footer(m.theme, m.focused(), m.showHelp)
	if m.status != "" {
		out = " " + m.theme.DimStyle().Render(m.status) + "\n" + out
	}
	return out
}
