package widget

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Addressed is a message bound for a single widget.
//
// Widget commands run off the UI goroutine and report back as messages, but
// bubbletea has one message stream for the whole program: without an address,
// every widget sees every result and two widgets of the same type on one
// dashboard overwrite each other. Base.Cmd and Base.Tick wrap results in an
// Addressed so the dashboard can deliver them to the one widget that asked.
//
// Widgets never construct or inspect this: they send with Base.Cmd and
// receive their own plain message type.
type Addressed struct {
	Name string
	Msg  tea.Msg
}

// Cmd runs f off the UI goroutine and delivers its result to this widget
// alone. Use it for every command whose message the widget handles itself.
//
// A nil message is dropped rather than addressed, so a fire-and-forget
// command (opening a browser, say) costs no round trip.
func (b *Base) Cmd(f func() tea.Msg) tea.Cmd {
	name := b.name
	return func() tea.Msg { return address(name, f()) }
}

// Tick delivers f's message to this widget alone once d has elapsed.
func (b *Base) Tick(d time.Duration, f func(time.Time) tea.Msg) tea.Cmd {
	name := b.name
	return tea.Tick(d, func(t time.Time) tea.Msg { return address(name, f(t)) })
}

// Every schedules msg for this widget alone once d has elapsed. It is the
// common case of Tick, where the message carries no timestamp.
func (b *Base) Every(d time.Duration, msg tea.Msg) tea.Cmd {
	return b.Tick(d, func(time.Time) tea.Msg { return msg })
}

// Address labels a message for this widget. Prefer Cmd and Tick; reach for
// Address only when the message is produced inside a callback ctOS does not
// own, such as tea.ExecProcess's exit handler.
func (b *Base) Address(msg tea.Msg) tea.Msg { return address(b.name, msg) }

// Unwrap returns the message inside an Addressed, or msg unchanged. The
// dashboard unwraps on delivery; tests use it to read a command's result
// without a running dashboard.
func Unwrap(msg tea.Msg) tea.Msg {
	if a, ok := msg.(Addressed); ok {
		return a.Msg
	}
	return msg
}

func address(name string, msg tea.Msg) tea.Msg {
	if msg == nil {
		return nil
	}
	return Addressed{Name: name, Msg: msg}
}
