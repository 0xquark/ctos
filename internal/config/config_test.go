package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExpandString(t *testing.T) {
	t.Setenv("CTOS_TEST_TOKEN", "s3cret")
	t.Setenv("CTOS_TEST_EMPTY", "")

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"no reference", "plain value", "plain value", false},
		{"set variable", "${CTOS_TEST_TOKEN}", "s3cret", false},
		{"embedded", "Bearer ${CTOS_TEST_TOKEN}!", "Bearer s3cret!", false},
		{"set but empty", "${CTOS_TEST_EMPTY}", "", false},
		{"default used", "${CTOS_TEST_MISSING:-fallback}", "fallback", false},
		{"default empty", "${CTOS_TEST_MISSING:-}", "", false},
		{"default ignored when set", "${CTOS_TEST_TOKEN:-fallback}", "s3cret", false},
		{"missing is an error", "${CTOS_TEST_MISSING}", "", true},
		{"bare dollar untouched", "$NOT_EXPANDED", "$NOT_EXPANDED", false},
		{"time layout untouched", "15:04:05", "15:04:05", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandString(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expandString(%q) succeeded, want an error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("expandString(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("expandString(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(EnvConfigDir, "")

	t.Run("explicit flag wins", func(t *testing.T) {
		t.Setenv(EnvConfigDir, "/from/env")
		got, err := ResolveDir(Options{Dir: "/from/flag"})
		if err != nil {
			t.Fatal(err)
		}
		if got != "/from/flag" {
			t.Errorf("got %q, want /from/flag", got)
		}
	})

	t.Run("env beats defaults", func(t *testing.T) {
		t.Setenv(EnvConfigDir, "/from/env")
		got, err := ResolveDir(Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got != "/from/env" {
			t.Errorf("got %q, want /from/env", got)
		}
	})

	t.Run("home-config forces legacy path", func(t *testing.T) {
		t.Setenv(EnvConfigDir, "")
		got, err := ResolveDir(Options{HomeConfig: true})
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".ctos"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("xdg when set", func(t *testing.T) {
		t.Setenv(EnvConfigDir, "")
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		got, err := ResolveDir(Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join("/xdg", "ctos") {
			t.Errorf("got %q, want /xdg/ctos", got)
		}
	})

	t.Run("defaults to ~/.config/ctos", func(t *testing.T) {
		t.Setenv(EnvConfigDir, "")
		t.Setenv("XDG_CONFIG_HOME", "")
		got, err := ResolveDir(Options{})
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".config", "ctos"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("existing ~/.ctos wins over a new xdg dir", func(t *testing.T) {
		t.Setenv(EnvConfigDir, "")
		t.Setenv("XDG_CONFIG_HOME", "")
		legacy := filepath.Join(home, ".ctos")
		if err := os.MkdirAll(legacy, 0o755); err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(legacy)

		got, err := ResolveDir(Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got != legacy {
			t.Errorf("got %q, want %q", got, legacy)
		}
	})
}

func TestLoadDashboard(t *testing.T) {
	dir := t.TempDir()

	path := write(t, dir, "home.yaml", `
name: home
widgets:
  clock:
    type: clock
    format: "15:04"
  notes:
    type: notes
    path: ~/notes
rows:
  - [clock, notes]
`)

	d, err := LoadDashboard(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "home" {
		t.Errorf("name = %q, want home", d.Name)
	}
	if len(d.Widgets) != 2 {
		t.Fatalf("got %d widgets, want 2", len(d.Widgets))
	}
	if got := d.Widgets["clock"].Type; got != "clock" {
		t.Errorf("clock type = %q, want clock", got)
	}
	if d.Widgets["clock"].Node == nil {
		t.Error("clock node is nil; widget factories cannot decode their config")
	}
	// A leading ~ must already be expanded by load time.
	if strings.HasPrefix(d.Widgets["notes"].Node.Content[3].Value, "~") {
		t.Error("~ in notes path was not expanded")
	}
}

func TestLoadDashboardRowsOptional(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "min.yaml", `
widgets:
  b:
    type: clock
  a:
    type: clock
`)
	d, err := LoadDashboard(path)
	if err != nil {
		t.Fatal(err)
	}
	// Falls back to one widget per row, in name order, so the file renders.
	want := [][]string{{"a"}, {"b"}}
	if len(d.Rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(d.Rows), len(want))
	}
	for i := range want {
		if d.Rows[i][0] != want[i][0] {
			t.Errorf("row %d = %v, want %v", i, d.Rows[i], want[i])
		}
	}
	// The name falls back to the filename.
	if d.Name != "min" {
		t.Errorf("name = %q, want min", d.Name)
	}
}

func TestLoadDashboardErrors(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{
			name:    "unknown widget in rows",
			yaml:    "widgets:\n  clock:\n    type: clock\nrows:\n  - [clock, ghost]\n",
			wantSub: `references widget "ghost"`,
		},
		{
			name:    "widget never placed",
			yaml:    "widgets:\n  clock:\n    type: clock\n  notes:\n    type: notes\nrows:\n  - [clock]\n",
			wantSub: `never placed`,
		},
		{
			name:    "duplicate placement",
			yaml:    "widgets:\n  clock:\n    type: clock\nrows:\n  - [clock, clock]\n",
			wantSub: `more than once`,
		},
		{
			name:    "missing type",
			yaml:    "widgets:\n  clock:\n    format: \"15:04\"\nrows:\n  - [clock]\n",
			wantSub: `has no "type:"`,
		},
		{
			name:    "no widgets",
			yaml:    "name: empty\n",
			wantSub: "no widgets defined",
		},
		{
			name:    "empty row",
			yaml:    "widgets:\n  clock:\n    type: clock\nrows:\n  - []\n  - [clock]\n",
			wantSub: "is empty",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := write(t, t.TempDir(), "d.yaml", tc.yaml)
			_, err := LoadDashboard(path)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

func TestDurationUnmarshal(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.yaml", "refresh:\n  default: 45s\n")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.DefaultRefresh(); got != 45*time.Second {
		t.Errorf("refresh = %v, want 45s", got)
	}
}

func TestDurationRejectsBadValues(t *testing.T) {
	for _, bad := range []string{"soon", "-5s", "0s", "5"} {
		t.Run(bad, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "config.yaml", "refresh:\n  default: \""+bad+"\"\n")
			if _, err := Load(dir); err == nil {
				t.Errorf("accepted invalid duration %q", bad)
			}
		})
	}
}

func TestLoadMissingConfigIsNotAnError(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("missing config.yaml should use defaults, got: %v", err)
	}
	if got := cfg.DefaultRefresh(); got != 30*time.Second {
		t.Errorf("default refresh = %v, want 30s", got)
	}
}

func TestResolveEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	if got := (&Config{Editor: "nvim"}).ResolveEditor(); got != "nvim" {
		t.Errorf("config editor = %q, want nvim", got)
	}
	t.Setenv("EDITOR", "helix")
	if got := (&Config{}).ResolveEditor(); got != "helix" {
		t.Errorf("$EDITOR = %q, want helix", got)
	}
	t.Setenv("EDITOR", "")
	if got := (&Config{}).ResolveEditor(); got != "vi" {
		t.Errorf("fallback = %q, want vi", got)
	}
}

func TestPickDashboard(t *testing.T) {
	dir := t.TempDir()
	write(t, DashboardsDir(dir), "home.yaml", "widgets:\n  c:\n    type: clock\n")
	write(t, DashboardsDir(dir), "work.yaml", "widgets:\n  c:\n    type: clock\n")

	t.Run("explicit name", func(t *testing.T) {
		got, err := PickDashboard(dir, "work", "home")
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != "work.yaml" {
			t.Errorf("got %q, want work.yaml", filepath.Base(got))
		}
	})

	t.Run("falls back to configured default", func(t *testing.T) {
		got, err := PickDashboard(dir, "", "work")
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != "work.yaml" {
			t.Errorf("got %q, want work.yaml", filepath.Base(got))
		}
	})

	t.Run("falls back to first alphabetically", func(t *testing.T) {
		got, err := PickDashboard(dir, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != "home.yaml" {
			t.Errorf("got %q, want home.yaml", filepath.Base(got))
		}
	})

	t.Run("unknown name lists what exists", func(t *testing.T) {
		_, err := PickDashboard(dir, "ghost", "")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "home, work") {
			t.Errorf("error should list available dashboards, got: %v", err)
		}
	})

	t.Run("no dashboards suggests init", func(t *testing.T) {
		_, err := PickDashboard(t.TempDir(), "", "")
		if err == nil || !strings.Contains(err.Error(), "ctos init") {
			t.Errorf("expected a hint to run ctos init, got: %v", err)
		}
	})
}

func TestScaffoldIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	written, skipped, err := Scaffold(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 || len(skipped) != 0 {
		t.Fatalf("first run: wrote %d, skipped %d; want 2 and 0", len(written), len(skipped))
	}

	// A second run must not clobber a user's edited config.
	if err := os.WriteFile(SettingsFile(dir), []byte("editor: mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, skipped, err = Scaffold(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 0 || len(skipped) != 2 {
		t.Fatalf("second run: wrote %d, skipped %d; want 0 and 2", len(written), len(skipped))
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Editor != "mine" {
		t.Errorf("scaffold overwrote the user's config: editor = %q", cfg.Editor)
	}
}

// TestScaffoldedConfigLoads is the guard that the shipped defaults actually
// parse — a broken starter config breaks every first-run experience.
func TestScaffoldedConfigLoads(t *testing.T) {
	t.Setenv("EDITOR", "nvim")
	dir := t.TempDir()

	if _, _, err := Scaffold(dir); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("shipped config.yaml does not load: %v", err)
	}
	if cfg.Editor != "nvim" {
		t.Errorf("editor = %q, want nvim from ${EDITOR}", cfg.Editor)
	}
	if cfg.DefaultDashboard != "home" {
		t.Errorf("default_dashboard = %q, want home", cfg.DefaultDashboard)
	}

	path, err := PickDashboard(dir, "", cfg.DefaultDashboard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDashboard(path); err != nil {
		t.Fatalf("shipped home.yaml does not load: %v", err)
	}
}

// The status bar is a second place a widget can be placed, so every check
// that "rows:" gets has to hold across both.
func TestLoadDashboardBar(t *testing.T) {
	path := write(t, t.TempDir(), "d.yaml", `
name: home
widgets:
  vitals:
    type: system
  notes:
    type: notes
bar: [vitals]
rows:
  - [notes]
`)
	d, err := LoadDashboard(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Bar.Left) != 1 || d.Bar.Left[0] != "vitals" {
		t.Errorf("Bar.Left = %v, want [vitals]", d.Bar.Left)
	}
	if len(d.Rows) != 1 || d.Rows[0][0] != "notes" {
		t.Errorf("Rows = %v, want [[notes]]", d.Rows)
	}
}

func TestLoadDashboardBarErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			"unknown widget in bar",
			"name: d\nwidgets:\n  a:\n    type: clock\nbar: [nope]\nrows:\n  - [a]\n",
			`bar[0] references widget "nope"`,
		},
		{
			"placed twice",
			"name: d\nwidgets:\n  a:\n    type: clock\nbar: [a]\nrows:\n  - [a]\n",
			"more than once",
		},
		{
			// A bar is chrome above a dashboard; it cannot be the
			// whole dashboard, because nothing would take focus.
			"nothing but a bar",
			"name: d\nwidgets:\n  a:\n    type: clock\nbar: [a]\n",
			`needs at least one widget in "rows:"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := write(t, t.TempDir(), "d.yaml", tc.yaml)
			_, err := LoadDashboard(path)
			if err == nil {
				t.Fatal("want an error, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should contain %q", err, tc.want)
			}
		})
	}
}

// A widget in the bar counts as placed, so the "defined but never placed"
// check must not fire on it.
func TestBarWidgetCountsAsPlaced(t *testing.T) {
	path := write(t, t.TempDir(), "d.yaml", `
name: d
widgets:
  vitals:
    type: system
  a:
    type: clock
bar: [vitals]
`)
	d, err := LoadDashboard(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rows were not written, so the remaining widgets stack — but the bar
	// widget is not stacked as well.
	if len(d.Rows) != 1 || d.Rows[0][0] != "a" {
		t.Errorf("Rows = %v, want [[a]]", d.Rows)
	}
}

// The bar takes a plain list when everything sits on the left, and a mapping
// when something belongs at the far end.
func TestLoadDashboardBarGroups(t *testing.T) {
	path := write(t, t.TempDir(), "d.yaml", `
name: home
widgets:
  vitals:
    type: system
  clock:
    type: clock
  notes:
    type: notes
bar:
  left: [vitals]
  right: [clock]
rows:
  - [notes]
`)
	d, err := LoadDashboard(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Bar.Left) != 1 || d.Bar.Left[0] != "vitals" {
		t.Errorf("Bar.Left = %v", d.Bar.Left)
	}
	if len(d.Bar.Right) != 1 || d.Bar.Right[0] != "clock" {
		t.Errorf("Bar.Right = %v", d.Bar.Right)
	}
	// Both groups are validated and both count as placing a widget.
	if want := []string{"vitals", "clock"}; len(d.Bar.Names()) != 2 || d.Bar.Names()[1] != want[1] {
		t.Errorf("Names = %v, want %v", d.Bar.Names(), want)
	}
}

func TestLoadDashboardBarGroupErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			"unknown group",
			"name: d\nwidgets:\n  a:\n    type: clock\nbar:\n  middle: [a]\nrows:\n  - [a]\n",
			`unknown key "middle"`,
		},
		{
			"not a list or a mapping",
			"name: d\nwidgets:\n  a:\n    type: clock\nbar: nonsense\nrows:\n  - [a]\n",
			`takes a list of widget names`,
		},
		{
			"in the bar twice",
			"name: d\nwidgets:\n  a:\n    type: clock\n  b:\n    type: clock\nbar:\n  left: [a]\n  right: [a]\nrows:\n  - [b]\n",
			"more than once",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadDashboard(write(t, t.TempDir(), "d.yaml", tc.yaml))
			if err == nil {
				t.Fatal("want an error, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should contain %q", err, tc.want)
			}
		})
	}
}
