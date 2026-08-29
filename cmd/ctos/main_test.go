package main

import (
	"strings"
	"testing"

	"github.com/0xquark/ctos/internal/config"
	"github.com/0xquark/ctos/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// The starter config is the first thing a new user sees, so it has to name
// only widget types that this binary actually registers. This test lives in
// cmd because cmd is where the widget packages are wired in; internal/tui must
// never import a concrete widget.
func TestScaffoldedDashboardBuildsAndRenders(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := config.Scaffold(dir); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	path, err := config.PickDashboard(dir, "", cfg.DefaultDashboard)
	if err != nil {
		t.Fatal(err)
	}
	dash, err := config.LoadDashboard(path)
	if err != nil {
		t.Fatal(err)
	}

	m, err := tui.New(cfg, dash)
	if err != nil {
		t.Fatalf("the starter dashboard does not build: %v", err)
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	out := m.View()
	for _, want := range []string{"clock", "notes", "processes", "hacker news"} {
		if !strings.Contains(out, want) {
			t.Errorf("the rendered starter dashboard is missing %q", want)
		}
	}
}
