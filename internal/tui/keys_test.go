package tui

import (
	"strings"
	"testing"

	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
)

// grabbingWidget is a minimal Widget that can claim the keyboard.
type grabbingWidget struct {
	widget.Base
	grab bool
}

func (g *grabbingWidget) Init() tea.Cmd          { return nil }
func (g *grabbingWidget) Update(tea.Msg) tea.Cmd { return nil }
func (g *grabbingWidget) View() string           { return "" }
func (g *grabbingWidget) Title() string          { return "grabby" }
func (g *grabbingWidget) GrabsKeys() bool        { return g.grab }

// While a widget is taking text input it has swallowed the global keys, so the
// footer must stop advertising them.
func TestFooterHidesGlobalKeysWhileAWidgetGrabs(t *testing.T) {
	th := theme.New("")

	normal := footer(th, &grabbingWidget{grab: false}, false)
	if !strings.Contains(normal, "quit") {
		t.Fatalf("normal footer lost the quit hint: %q", normal)
	}

	grabbed := footer(th, &grabbingWidget{grab: true}, false)
	if strings.Contains(grabbed, "quit") {
		t.Errorf("footer still offers q/quit while a widget owns the keyboard: %q", grabbed)
	}
	if !strings.Contains(grabbed, "cancel") {
		t.Errorf("footer does not offer a way out: %q", grabbed)
	}
}

// Grabbing must be false for a widget that does not implement KeyGrabber at
// all, so the three widgets that never take text input keep working.
func TestGrabbingIsFalseForOrdinaryWidgets(t *testing.T) {
	if widget.Grabbing(nil) {
		t.Error("a nil widget must not grab keys")
	}
}
