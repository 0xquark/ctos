// Package widget defines the contract every ctOS widget implements, plus the
// registry that maps a YAML "type:" to a constructor.
//
// Widgets depend on this package; this package depends on no other ctOS
// package except theme. Nothing here may import internal/tui.
package widget

import (
	"time"

	"github.com/0xquark/ctos/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"
)

// Widget is a single pane on a dashboard.
//
// Implementations must never block: all I/O belongs in a tea.Cmd that reports
// its result as a message. SetSize is called with the *inner* content area,
// excluding the frame's border and padding, so a widget should render exactly
// that many columns and rows.
type Widget interface {
	// Init returns an optional startup command (first fetch, first tick).
	Init() tea.Cmd

	// Update handles a message and returns the updated widget. Key messages
	// arrive only when the widget is focused; all other messages are
	// broadcast to every widget on the dashboard.
	Update(msg tea.Msg) (Widget, tea.Cmd)

	// View renders the widget's inner content.
	View() string

	// SetSize reports the inner content area in cells.
	SetSize(w, h int)

	// Focus and Blur track whether this widget currently has focus.
	Focus()
	Blur()

	// Title labels the widget's frame.
	Title() string

	// Actions lists what the user can do with the widget right now. The
	// first entry is the primary action, bound to enter.
	Actions() []Action
}

// Action is something the user can trigger on the focused widget.
type Action struct {
	// Name is shown in the help footer, e.g. "edit".
	Name string

	// Run returns the command to execute. Widget-provided actions use this.
	Run func() tea.Cmd

	// Embed describes an external TUI to run full-screen. Config-declared
	// actions use this; it is nil for in-widget actions.
	Embed *EmbedSpec
}

// EmbedSpec describes an external program that takes over the whole terminal
// and returns control to ctOS when it exits.
type EmbedSpec struct {
	Cmd  string   `yaml:"cmd"`
	Args []string `yaml:"args"`
	Dir  string   `yaml:"dir"`
}

// Context carries everything a Factory needs to build a widget.
type Context struct {
	// Name is the widget's key in the dashboard's widgets map. Widgets use
	// it to tag their own async messages so broadcasts can be filtered.
	Name string

	// Node is the raw YAML for this widget, for decoding type-specific keys.
	Node *yaml.Node

	// Theme is the resolved palette.
	Theme theme.Theme

	// Editor is the command to open a file with, already resolved from
	// config, $EDITOR, or the "vi" fallback.
	Editor string

	// DefaultRefresh applies to widgets that poll but set no interval.
	DefaultRefresh time.Duration
}

// Base supplies the boilerplate half of the Widget interface. Embed it by
// value and override only what matters.
type Base struct {
	W, H    int
	focused bool
}

// SetSize records the inner content area assigned by the layout.
func (b *Base) SetSize(w, h int) { b.W, b.H = w, h }

// Focus marks the widget as holding keyboard focus.
func (b *Base) Focus() { b.focused = true }

// Blur marks the widget as no longer holding keyboard focus.
func (b *Base) Blur() { b.focused = false }

// Focused reports whether the widget currently has focus.
func (b *Base) Focused() bool { return b.focused }

// Actions defaults to none, so a read-only widget need not implement it.
func (b *Base) Actions() []Action { return nil }

// KeyGrabber is an optional interface for widgets that sometimes need every
// keystroke, such as while a filter query is being typed. When GrabsKeys
// reports true the dashboard stops interpreting keys itself and forwards them
// all to the focused widget, so that typing "q" types a q rather than quitting.
//
// Only ctrl+c survives, because a way out must always exist. Widgets that
// grab keys must offer their own exit, conventionally esc.
type KeyGrabber interface {
	GrabsKeys() bool
}

// Grabbing reports whether w is a KeyGrabber currently demanding raw keys.
func Grabbing(w Widget) bool {
	g, ok := w.(KeyGrabber)
	return ok && g.GrabsKeys()
}
