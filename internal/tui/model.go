// Package tui is the dashboard shell: it owns layout, focus, global keys and
// the frame around each widget. It must not import a concrete widget package.
package tui

import (
	"fmt"
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

// Model is the root bubbletea model.
type Model struct {
	cfg   *config.Config
	dash  *config.Dashboard
	theme theme.Theme

	// byName owns the widgets. rows and order are views onto it, rebuilt
	// whenever the layout changes, so rearranging never reconstructs a
	// widget and never loses its state.
	byName map[string]widget.Widget
	rows   [][]widget.Widget
	order  []widget.Widget
	names  []string

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
			return nil, fmt.Errorf("%s: widget %q: %w", dash.Path, name, err)
		}
		m.byName[name] = w
	}

	m.rebuild("")
	if len(m.order) == 0 {
		return nil, fmt.Errorf("%s: dashboard has no widgets", dash.Path)
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
	cmds := make([]tea.Cmd, 0, len(m.order))
	for _, w := range m.order {
		if cmd := w.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// Update routes messages. Key messages go only to the focused widget;
// everything else is broadcast so background refreshes reach their owner.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ready = true
		m.resize()
		return m, nil

	case tea.KeyMsg:
		// In layout mode the arrows rearrange widgets rather than
		// scrolling inside one, so nothing falls through.
		if m.layoutMode {
			return m, m.layoutKey(msg)
		}
		// A widget typing a filter query owns the whole keyboard; otherwise
		// "q" would quit mid-word. ctrl+c still gets out.
		if widget.Grabbing(m.focused()) {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, m.updateFocused(msg)
		}
		if cmd, handled := m.globalKey(msg); handled {
			return m, cmd
		}
		return m, m.updateFocused(msg)
	}

	return m, m.broadcast(msg)
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
	name := m.focusedName()
	if name == "" {
		return nil
	}
	updated, cmd := m.byName[name].Update(msg)
	m.byName[name] = updated
	m.syncViews()
	return cmd
}

// broadcast delivers a message to every widget.
func (m *Model) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for name, w := range m.byName {
		updated, cmd := w.Update(msg)
		m.byName[name] = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	m.syncViews()
	return tea.Batch(cmds...)
}

// syncViews refreshes rows and order after Update returned new widget values.
func (m *Model) syncViews() {
	k := 0
	for i, row := range m.rows {
		for j := range row {
			w := m.byName[m.names[k]]
			m.rows[i][j] = w
			m.order[k] = w
			k++
		}
	}
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
func (m *Model) resize() {
	if !m.ready {
		return
	}
	boxes := Layout(m.dash.Rows, m.w, m.h-m.footerHeight())

	for i, row := range m.rows {
		for j, w := range row {
			if i < len(boxes) && j < len(boxes[i]) {
				iw, ih := boxes[i][j].Inner()
				w.SetSize(iw, ih)
			}
		}
	}
}

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

	boxes := Layout(m.dash.Rows, m.w, m.h-m.footerHeight())

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
	return strings.TrimRight(dashboard, "\n") + "\n" + m.footerView()
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
