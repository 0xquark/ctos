package widget

import (
	"testing"

	"github.com/0xquark/ctos/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// painter renders through Base.Theme, the way every real widget does.
type painter struct {
	Base
	seen int // how many times View has been called
}

func (p *painter) Init() tea.Cmd          { return nil }
func (p *painter) Update(tea.Msg) tea.Cmd { return nil }
func (p *painter) View() string           { p.seen++; return p.Theme().Name }

// A widget must take its palette from Base, so that a theme switch reaches it
// without the widget author having to do anything.
func TestRethemeChangesWhatAWidgetRenders(t *testing.T) {
	w := &painter{}
	w.bind("w", "w", theme.New(""))

	if got := w.View(); got != theme.Default {
		t.Fatalf("a bound widget renders theme %q, want %q", got, theme.Default)
	}

	other, err := theme.Resolve("dedsec", "")
	if err != nil {
		t.Fatal(err)
	}
	Retheme(w, other)

	if got := w.View(); got != "dedsec" {
		t.Errorf("after Retheme the widget renders theme %q, want %q", got, "dedsec")
	}
}

// Retheme repaints; it must not rebuild. A widget that lost its state on every
// theme change would drop scroll positions and loaded data.
func TestRethemeKeepsWidgetState(t *testing.T) {
	w := &painter{}
	w.bind("w", "the title", theme.New(""))
	w.SetSize(40, 10)
	w.Focus()
	w.View()

	Retheme(w, theme.New("#ff00ff"))

	if w.seen != 1 {
		t.Error("Retheme should not have re-rendered or reset the widget")
	}
	if w.Title() != "the title" || w.Name() != "w" {
		t.Error("Retheme lost the widget's identity")
	}
	if w.W != 40 || w.H != 10 || !w.Focused() {
		t.Error("Retheme lost the widget's size or focus")
	}
}

// A widget that does not embed Base is not repaintable, and Retheme must let it
// be rather than panicking on the dashboard's behalf.
func TestRethemeIgnoresAWidgetWithoutBase(t *testing.T) {
	Retheme(bare{}, theme.New(""))
}

type bare struct{}

func (bare) Init() tea.Cmd          { return nil }
func (bare) Update(tea.Msg) tea.Cmd { return nil }
func (bare) View() string           { return "" }
func (bare) SetSize(int, int)       {}
func (bare) Focus()                 {}
func (bare) Blur()                  {}
func (bare) Title() string          { return "" }
func (bare) Actions() []Action      { return nil }
