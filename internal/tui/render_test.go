package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xquark/ctos/internal/config"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	_ "github.com/0xquark/ctos/internal/widgets/clock"
	_ "github.com/0xquark/ctos/internal/widgets/hackernews"
	_ "github.com/0xquark/ctos/internal/widgets/notes"
)

// buildDashboard writes a throwaway config and returns a ready model.
func buildDashboard(t *testing.T, w, h int) *Model {
	t.Helper()

	dir := t.TempDir()
	notes := filepath.Join(dir, "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{
		"today.md":      "# Wednesday\n\n- shipped the notes preview\n- fixed the marker width bug\n\n> layout mode next\n",
		"standup.md":    "# Standup\n\n- yesterday: config loader\n- today: ssh runner\n",
		"ctos-ideas.md": "# Ideas\n\n- aggregate process table\n- command palette\n",
	}
	for i, name := range []string{"today.md", "standup.md", "ctos-ideas.md"} {
		path := filepath.Join(notes, name)
		if err := os.WriteFile(path, []byte(contents[name]), 0o644); err != nil {
			t.Fatal(err)
		}
		// Stagger mtimes so the newest-first sort is deterministic.
		mod := time.Now().Add(-time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}

	dashPath := filepath.Join(dir, "home.yaml")
	dash := fmt.Sprintf(`
name: home
widgets:
  clock:
    type: clock
    format: "15:04:05"
  notes:
    type: notes
    path: %s
  hackernews:
    type: hackernews
rows:
  - [clock, notes]
  - [hackernews]
`, notes)
	if err := os.WriteFile(dashPath, []byte(dash), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := config.LoadDashboard(dashPath)
	if err != nil {
		t.Fatalf("load dashboard: %v", err)
	}

	m, err := New(&config.Config{}, d)
	if err != nil {
		t.Fatalf("build model: %v", err)
	}

	m.w, m.h, m.ready = w, h, true
	m.resize()

	// Drive the notes widget through its startup messages by hand, so the
	// list and its preview are populated without a running bubbletea loop.
	if w, ok := m.byName["notes"]; ok {
		cmd := w.Init()
		for range 3 {
			if cmd == nil {
				break
			}
			msg := cmd()
			if msg == nil {
				break
			}
			// Commands address their results; the dashboard normally
			// unwraps them before delivery.
			if a, ok := msg.(widget.Addressed); ok {
				msg = a.Msg
			}
			cmd = w.Update(msg)
		}
	}
	return m
}

// TestRenderFitsTerminal is the end-to-end guard: whatever the widgets draw,
// the dashboard must occupy exactly the terminal it was given. A widget that
// overflows its frame would silently break every other widget's alignment.
func TestRenderFitsTerminal(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 24}, {120, 40}, {100, 30}, {60, 20},
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			m := buildDashboard(t, size.w, size.h)
			out := m.View()

			lines := strings.Split(out, "\n")
			if got := len(lines); got != size.h {
				t.Errorf("rendered %d lines, terminal is %d tall", got, size.h)
			}
			for i, line := range lines {
				if got := lipgloss.Width(line); got > size.w {
					t.Errorf("line %d is %d cells wide, terminal is %d", i, got, size.w)
				}
			}
		})
	}
}

// TestRenderTooSmall checks the graceful message rather than a broken layout.
func TestRenderTooSmall(t *testing.T) {
	m := buildDashboard(t, 20, 5)
	if out := m.View(); !strings.Contains(out, "terminal too small") {
		t.Errorf("expected a too-small message, got:\n%s", out)
	}
}

// TestLayoutModeRendersWithinTerminal checks layout mode obeys the same size
// contract as the normal view — its footer is taller, so the grid must shrink.
func TestLayoutModeRendersWithinTerminal(t *testing.T) {
	m := buildDashboard(t, 100, 30)
	m.enterLayoutMode()

	lines := strings.Split(m.View(), "\n")
	if len(lines) != 30 {
		t.Errorf("layout mode rendered %d lines, terminal is 30 tall", len(lines))
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got > 100 {
			t.Errorf("line %d is %d cells wide, terminal is 100", i, got)
		}
	}
	if !strings.Contains(m.View(), "LAYOUT") {
		t.Error("layout mode should announce itself in the footer")
	}
}

// TestLayoutModeMovesAndCancels covers the round trip a user actually makes:
// rearrange, look at it, then back out without saving.
func TestLayoutModeMovesAndCancels(t *testing.T) {
	m := buildDashboard(t, 100, 30)
	before := clone(m.dash.Rows)

	m.enterLayoutMode()
	m.layoutKey(tea.KeyMsg{Type: tea.KeyRight})

	if sameLayout(m.dash.Rows, before) {
		t.Fatal("moving right did not change the layout")
	}
	// The moved widget keeps focus so it can be pushed further.
	if m.focusedName() != "clock" {
		t.Errorf("focus moved to %q during a layout move, want clock", m.focusedName())
	}

	m.layoutKey(tea.KeyMsg{Type: tea.KeyEsc})

	if !sameLayout(m.dash.Rows, before) {
		t.Errorf("esc left the layout as %v, want %v restored", m.dash.Rows, before)
	}
	if m.layoutMode {
		t.Error("esc did not leave layout mode")
	}
}

// TestLayoutModeSavesToDisk covers the other exit: save, and confirm the file
// on disk now describes the new arrangement.
func TestLayoutModeSavesToDisk(t *testing.T) {
	m := buildDashboard(t, 100, 30)

	m.enterLayoutMode()
	m.layoutKey(tea.KeyMsg{Type: tea.KeyRight})
	moved := clone(m.dash.Rows)
	m.layoutKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	if m.layoutMode {
		t.Error("saving should leave layout mode")
	}
	if !strings.Contains(m.status, "saved") {
		t.Errorf("status = %q, want it to confirm the save", m.status)
	}

	reloaded, err := config.LoadDashboard(m.dash.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !sameLayout(reloaded.Rows, moved) {
		t.Errorf("file has %v, want %v", reloaded.Rows, moved)
	}
}

// TestWidgetStateSurvivesRearranging matters because moving a widget rebuilds
// the grid: the widget itself must be the same object, not a fresh one that
// lost its loaded notes and cursor position.
func TestWidgetStateSurvivesRearranging(t *testing.T) {
	m := buildDashboard(t, 100, 30)
	notesBefore := m.byName["notes"]

	m.enterLayoutMode()
	m.layoutKey(tea.KeyMsg{Type: tea.KeyDown})

	if m.byName["notes"] != notesBefore {
		t.Error("rearranging rebuilt the notes widget instead of moving it")
	}
}

// TestShowDashboard prints a dashboard for eyeballing: go test ./internal/tui -run ShowDashboard -v
func TestShowDashboard(t *testing.T) {
	m := buildDashboard(t, 100, 30)
	t.Log("\n" + m.View())

	m.enterLayoutMode()
	t.Log("\nlayout mode:\n" + m.View())
}
