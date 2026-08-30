package main

import (
	"strings"
	"testing"

	"github.com/0xquark/ctos/internal/config"
	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/tui"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"
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
	for _, want := range []string{"tasks", "notes", "processes", "hacker news"} {
		if !strings.Contains(out, want) {
			t.Errorf("the rendered starter dashboard is missing %q", want)
		}
	}

	// The clock and the vitals strip are in the status bar rather than the
	// grid, so neither has a frame title to look for. What the bar must not
	// be is missing: it is the first line, drawn without a border.
	first, _, _ := strings.Cut(out, "\n")
	if strings.ContainsAny(first, "╭╮") {
		t.Errorf("the first line should be the frameless status bar, got %q", first)
	}
	if strings.TrimSpace(first) == "" {
		t.Errorf("the status bar rendered nothing")
	}
}

// Every widget's Example is what `ctos widgets <type>` tells people to paste,
// so it has to be config the widget actually accepts. Decoding is strict about
// unknown keys, which means this also catches an example that has drifted
// behind the widget's config struct.
func TestWidgetExamplesAreValidConfig(t *testing.T) {
	for _, spec := range widget.Specs() {
		t.Run(spec.Name, func(t *testing.T) {
			if spec.Example == "" {
				t.Skip("no example")
			}

			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(spec.Example), &doc); err != nil {
				t.Fatalf("example is not valid YAML: %v", err)
			}
			node := doc.Content[0]

			var got struct {
				Type string `yaml:"type"`
			}
			if err := node.Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Type != spec.Name {
				t.Errorf("example declares type %q, want %q", got.Type, spec.Name)
			}

			if _, err := widget.New(spec.Name, widget.Context{
				Name:  "example",
				Node:  node,
				Theme: theme.New(""),
			}); err != nil {
				t.Errorf("example does not build: %v", err)
			}
		})
	}
}
