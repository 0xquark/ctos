package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/0xquark/ctos/internal/config"
	"github.com/0xquark/ctos/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A frame has to draw in the theme's own line-drawing set, not in one the shell
// hardcodes: the corners are half of what tells two themes apart.
func TestFrameUsesTheThemesChrome(t *testing.T) {
	for _, name := range theme.Names() {
		t.Run(name, func(t *testing.T) {
			th, err := theme.Resolve(name, "")
			if err != nil {
				t.Fatal(err)
			}
			out := Frame(th, "notes", FrameIdle, 40, 5, "x")
			first, _, _ := strings.Cut(out, "\n")

			if !strings.HasPrefix(ansi.Strip(first), th.Chrome.TopLeft) {
				t.Errorf("top border starts %q, want the theme's %q", first, th.Chrome.TopLeft)
			}
			if !strings.Contains(ansi.Strip(out), th.Chrome.BottomRight) {
				t.Errorf("frame is missing the theme's bottom-right corner %q", th.Chrome.BottomRight)
			}
		})
	}
}

// The bracketed chrome pads its title with more decoration than the rounded
// one, so the width arithmetic in topBorder has to come from the chrome rather
// than from a constant.
func TestFrameIsExactWidthInEveryChrome(t *testing.T) {
	titles := []string{"", "notes", "a title long enough to need trimming at this width"}

	for _, name := range theme.Names() {
		th, err := theme.Resolve(name, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, title := range titles {
			for _, w := range []int{8, 20, 40, 120} {
				out := Frame(th, title, FrameFocused, w, 4, "content")
				for i, line := range strings.Split(out, "\n") {
					if got := lipgloss.Width(line); got != w {
						t.Errorf("%s/%q/w=%d: line %d is %d cells", name, title, w, i, got)
					}
				}
			}
		}
	}
}

// A typo in "theme: name:" must stop startup and say what the choices are,
// rather than silently rendering in the default colours.
func TestUnknownThemeIsAStartupError(t *testing.T) {
	d := oneWidgetDashboard(t)

	cfg := &config.Config{}
	cfg.Theme.Name = "watch_dogs"

	_, err := New(cfg, d)
	if err == nil {
		t.Fatal("want an error for an unknown theme name")
	}
	if !strings.Contains(err.Error(), "watch_dogs") || !strings.Contains(err.Error(), theme.Default) {
		t.Errorf("error should name the typo and the alternatives: %v", err)
	}
}

// A named theme reaches the widgets, not just the frames: widgets are handed a
// resolved theme at construction and never see the config.
func TestNamedThemeReachesTheModel(t *testing.T) {
	d := oneWidgetDashboard(t)

	cfg := &config.Config{}
	cfg.Theme.Name = "noir"

	m, err := New(cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	if m.theme.Name != "noir" {
		t.Errorf("model theme is %q, want %q", m.theme.Name, "noir")
	}
}

// oneWidgetDashboard writes the smallest valid dashboard and loads it.
func oneWidgetDashboard(t *testing.T) *config.Dashboard {
	t.Helper()
	path := filepath.Join(t.TempDir(), "home.yaml")
	if err := os.WriteFile(path, []byte("name: home\nwidgets:\n  clock:\n    type: clock\nrows:\n  - [clock]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := config.LoadDashboard(path)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// ctrl+t is the whole theme shortcut: it must move to the next palette, repaint
// the widgets as well as the shell, and say which theme it landed on.
func TestCycleThemeRepaintsEverything(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Dir: dir}
	cfg.Theme.Name = theme.Default

	m, err := New(cfg, oneWidgetDashboard(t))
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	names := theme.Cycle()
	want := names[(slices.Index(names, theme.Default)+1)%len(names)]

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})

	if m.theme.Name != want {
		t.Errorf("shell theme is %q, want %q", m.theme.Name, want)
	}
	// The widget renders through widget.Base.Theme, so it must have moved
	// with the shell rather than keeping the palette it was built with.
	if got := m.byName["clock"].(interface{ Theme() theme.Theme }).Theme().Name; got != want {
		t.Errorf("widget theme is %q, want %q", got, want)
	}
	if !strings.Contains(m.View(), "theme: "+want) {
		t.Error("the footer should name the theme it switched to")
	}
}

// Cycling has to come back round, or the last theme in the list is a trap.
func TestCycleThemeWrapsAndPersists(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Dir: dir}

	m, err := New(cfg, oneWidgetDashboard(t))
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	seen := []string{m.theme.Name}
	for range theme.Cycle() {
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
		seen = append(seen, m.theme.Name)
	}
	if got, want := seen[len(seen)-1], seen[0]; got != want {
		t.Errorf("a full cycle ended on %q, want back at %q", got, want)
	}

	// Every press writes config.yaml, so the choice survives a restart.
	reloaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Theme.Name != m.theme.Name {
		t.Errorf("config.yaml holds %q, dashboard is showing %q", reloaded.Theme.Name, m.theme.Name)
	}
}

// A status line costs the dashboard a row, so it must not outlive the keystroke
// after it — except another ctrl+t, which is how you cycle past a theme you do
// not want without losing the label naming them.
func TestThemeStatusIsTransient(t *testing.T) {
	cfg := &config.Config{Dir: t.TempDir()}
	m, err := New(cfg, oneWidgetDashboard(t))
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.status == "" {
		t.Fatal("ctrl+t should report the theme it switched to")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.status == "" {
		t.Error("a second ctrl+t should still name a theme, not blank the line")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.status != "" {
		t.Errorf("any other key should dismiss the status, got %q", m.status)
	}
}

// The shortcut must not be able to break the dashboard: an unsaveable config
// directory is worth a note, not a refusal to change colour.
func TestCycleThemeRepaintsEvenWhenTheSaveFails(t *testing.T) {
	dir := t.TempDir()
	// A file where the config directory should be, so writing into it fails.
	if err := os.WriteFile(filepath.Join(dir, "wall"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Dir: filepath.Join(dir, "wall")}

	m, err := New(cfg, oneWidgetDashboard(t))
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	before := m.theme.Name
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})

	if m.theme.Name == before {
		t.Error("a failed save should not stop the theme changing for this session")
	}
	if !strings.Contains(m.status, "not saved") {
		t.Errorf("the user should be told the choice was not persisted, got %q", m.status)
	}
}

// The bug this guards: a config written before themes existed carries
// "accent: #ff6b35", and resolving every palette with it made all five come out
// orange — the focus border, the selections, the memory bar's used segment.
// Asking for a theme asks for its whole look.
func TestCycleThemeDropsTheAccentOverride(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Dir: dir}
	cfg.Theme.Name = "ember"
	cfg.Theme.Accent = "#ff6b35"

	m, err := New(cfg, oneWidgetDashboard(t))
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})

	want, err := theme.Resolve(m.theme.Name, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.theme.Accent != want.Accent {
		t.Errorf("%s renders accent %v, want its own %v", m.theme.Name, m.theme.Accent, want.Accent)
	}
	if cfg.Theme.Accent != "" {
		t.Errorf("the override is still in the loaded config: %q", cfg.Theme.Accent)
	}
	reloaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Theme.Accent != "" {
		t.Errorf("the override is still in config.yaml: %q", reloaded.Theme.Accent)
	}
}

// Every palette must actually reach the screen as itself: no two themes may
// render a focused frame in the same colour.
func TestEveryThemePaintsItsOwnAccent(t *testing.T) {
	seen := map[string]string{}
	for _, name := range theme.Names() {
		th, err := theme.Resolve(name, "")
		if err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprint(th.BorderFocus)
		if other, dup := seen[key]; dup {
			t.Errorf("%s and %s draw a focused frame in the same colour %v", other, name, th.BorderFocus)
		}
		seen[key] = name
	}
}
