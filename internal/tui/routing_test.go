package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xquark/ctos/internal/config"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
)

// pingMsg stands in for any result a widget's own command produces.
type pingMsg struct{}

// shoutMsg stands in for a message from outside any widget, which every widget
// should still see.
type shoutMsg struct{}

// counter is a widget that records what reached it.
type counter struct {
	widget.Base
	pings  int
	shouts int
}

func init() {
	widget.Register(widget.Spec{
		Name:    "test-counter",
		Summary: "counts the messages it is given",
		New:     func(widget.Context) (widget.Widget, error) { return &counter{}, nil },
	})
}

func (c *counter) Init() tea.Cmd { return c.Cmd(func() tea.Msg { return pingMsg{} }) }
func (c *counter) View() string  { return "" }
func (c *counter) Title() string { return "counter" }

func (c *counter) Update(msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case pingMsg:
		c.pings++
	case shoutMsg:
		c.shouts++
	}
	return nil
}

// buildCounters returns a model holding two identically-typed widgets, which
// is the case that used to need a name check inside every widget.
func buildCounters(t *testing.T) *Model {
	t.Helper()

	path := filepath.Join(t.TempDir(), "counters.yaml")
	dash := `
name: counters
widgets:
  left:
    type: test-counter
  right:
    type: test-counter
rows:
  - [left, right]
`
	if err := os.WriteFile(path, []byte(dash), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := config.LoadDashboard(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(&config.Config{}, d)
	if err != nil {
		t.Fatal(err)
	}
	m.w, m.h, m.ready = 80, 24, true
	m.resize()
	return m
}

func widgets(t *testing.T, m *Model) (left, right *counter) {
	t.Helper()
	l, ok := m.byName["left"].(*counter)
	if !ok {
		t.Fatal("left is not a counter")
	}
	r, ok := m.byName["right"].(*counter)
	if !ok {
		t.Fatal("right is not a counter")
	}
	return l, r
}

// TestCmdAddressesItsWidget checks the half a widget author relies on: a
// command built with Base.Cmd carries the widget's config name.
func TestCmdAddressesItsWidget(t *testing.T) {
	m := buildCounters(t)
	left, _ := widgets(t, m)

	msg := left.Init()()
	addressed, ok := msg.(widget.Addressed)
	if !ok {
		t.Fatalf("Base.Cmd produced %T, want widget.Addressed", msg)
	}
	if addressed.Name != "left" {
		t.Errorf("addressed to %q, want left", addressed.Name)
	}
	if _, ok := addressed.Msg.(pingMsg); !ok {
		t.Errorf("wrapped %T, want pingMsg", addressed.Msg)
	}
}

// TestAddressedMessageReachesOneWidget is the guarantee that replaces the name
// check widgets used to carry: two widgets of the same type on one dashboard
// must not see each other's results.
func TestAddressedMessageReachesOneWidget(t *testing.T) {
	m := buildCounters(t)
	left, right := widgets(t, m)

	m.Update(widget.Addressed{Name: "left", Msg: pingMsg{}})

	if left.pings != 1 {
		t.Errorf("left got %d pings, want 1", left.pings)
	}
	if right.pings != 0 {
		t.Errorf("right got %d pings, want 0 — a widget saw another's result", right.pings)
	}
}

// Messages from outside any widget still reach everything, since that is how
// a resize or a global refresh gets delivered.
func TestUnaddressedMessageIsBroadcast(t *testing.T) {
	m := buildCounters(t)
	left, right := widgets(t, m)

	m.Update(shoutMsg{})

	if left.shouts != 1 || right.shouts != 1 {
		t.Errorf("shouts = %d/%d, want 1/1", left.shouts, right.shouts)
	}
}

// A result can outlive the widget it was meant for; dropping it beats panicking.
func TestAddressedMessageForAMissingWidgetIsDropped(t *testing.T) {
	m := buildCounters(t)
	left, right := widgets(t, m)

	m.Update(widget.Addressed{Name: "gone", Msg: pingMsg{}})

	if left.pings != 0 || right.pings != 0 {
		t.Errorf("pings = %d/%d, want 0/0", left.pings, right.pings)
	}
}
