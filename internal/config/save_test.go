package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const annotatedDashboard = `# My home dashboard.
# Second comment line.

name: home

widgets:
  clock:
    type: clock
    # Keep seconds visible.
    format: "15:04:05"
  notes:
    type: notes
    path: ~/notes
  hackernews:
    type: hackernews
    token: ${HN_TOKEN}

# Layout: top row is glanceable, bottom row is the feed.
rows:
  - [clock, notes]
  - [hackernews]
`

// TestSaveRowsPreservesEverythingElse is the guard that makes layout mode safe
// to use on a real, hand-commented dashboard.
func TestSaveRowsPreservesEverythingElse(t *testing.T) {
	t.Setenv("HN_TOKEN", "should-not-be-written")

	path := filepath.Join(t.TempDir(), "home.yaml")
	if err := os.WriteFile(path, []byte(annotatedDashboard), 0o644); err != nil {
		t.Fatal(err)
	}

	// Load first: this expands ${HN_TOKEN} and ~ in memory, and the save
	// must not write those resolved values back.
	if _, err := LoadDashboard(path); err != nil {
		t.Fatal(err)
	}

	newRows := [][]string{{"hackernews"}, {"notes", "clock"}}
	if err := SaveLayout(path, newRows, Bar{}); err != nil {
		t.Fatal(err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(saved)

	for _, want := range []string{
		"# My home dashboard.",
		"# Second comment line.",
		"# Keep seconds visible.",
		"# Layout: top row is glanceable",
		`format: "15:04:05"`,
		"path: ~/notes",
		"token: ${HN_TOKEN}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("save dropped %q from the file:\n%s", want, got)
		}
	}

	if strings.Contains(got, "should-not-be-written") {
		t.Errorf("save wrote a resolved environment variable into the file:\n%s", got)
	}
	if strings.Contains(got, os.Getenv("HOME")+"/notes") {
		t.Errorf("save wrote an expanded home directory into the file:\n%s", got)
	}

	// And the new layout round-trips.
	d, err := LoadDashboard(path)
	if err != nil {
		t.Fatalf("saved file no longer loads: %v", err)
	}
	if !reflect.DeepEqual(d.Rows, newRows) {
		t.Errorf("rows = %v, want %v", d.Rows, newRows)
	}
}

// TestSaveRowsUsesFlowStyle keeps the saved file readable by hand.
func TestSaveRowsUsesFlowStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.yaml")
	if err := os.WriteFile(path, []byte(annotatedDashboard), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveLayout(path, [][]string{{"clock", "notes"}, {"hackernews"}}, Bar{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "- [clock, notes]") {
		t.Errorf("expected inline row syntax, got:\n%s", got)
	}
}

// TestSaveRowsAddsMissingKey covers a dashboard that never declared rows.
func TestSaveRowsAddsMissingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.yaml")
	src := "name: home\nwidgets:\n  clock:\n    type: clock\n  notes:\n    type: notes\n    path: /tmp\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	want := [][]string{{"clock", "notes"}}
	if err := SaveLayout(path, want, Bar{}); err != nil {
		t.Fatal(err)
	}

	d, err := LoadDashboard(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Rows, want) {
		t.Errorf("rows = %v, want %v", d.Rows, want)
	}
}

func TestSaveRowsKeepsFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.yaml")
	if err := os.WriteFile(path, []byte(annotatedDashboard), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveLayout(path, [][]string{{"clock"}, {"notes"}, {"hackernews"}}, Bar{}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
}

func TestSaveRowsErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		err := SaveLayout(filepath.Join(t.TempDir(), "nope.yaml"), [][]string{{"a"}}, Bar{})
		if err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("not a mapping", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "d.yaml")
		if err := os.WriteFile(path, []byte("- just\n- a list\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := SaveLayout(path, [][]string{{"a"}}, Bar{}); err == nil {
			t.Fatal("expected an error for a non-dashboard file")
		}
	})
}

// TestSaveRowsLeavesNoTempFiles checks the atomic write cleans up after itself.
func TestSaveRowsLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.yaml")
	if err := os.WriteFile(path, []byte(annotatedDashboard), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := SaveLayout(path, [][]string{{"clock"}, {"notes", "hackernews"}}, Bar{}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory has %v, want only d.yaml", names)
	}
}

// An empty Bar means the caller has nothing to say about it, so the file's own
// bar — and everything else in the file — has to come through untouched.
func TestSaveKeepsTheBarItWasNotGiven(t *testing.T) {
	path := write(t, t.TempDir(), "d.yaml", `# my dashboard
name: home
widgets:
  vitals:
    type: system
    style: bar
  notes:
    type: notes
    path: ${HOME}/notes

# the strip up top
bar: [vitals]

rows:
  - [notes]
`)
	if err := SaveLayout(path, [][]string{{"notes"}}, Bar{}); err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"bar: [vitals]", "# the strip up top", "${HOME}/notes", "# my dashboard"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("save dropped %q:\n%s", want, out)
		}
	}
}

// Moving the bar in layout mode has to survive the save, which means writing
// "position:" and the group keys that position actually takes.
func TestSaveRewritesTheBar(t *testing.T) {
	src := `# my dashboard
name: home
widgets:
  vitals:
    type: system
  clock:
    type: clock
  notes:
    type: notes
    path: ${HOME}/notes

# the strip
bar:
  left: [vitals]
  right: [clock]

rows:
  - [notes]
`
	path := write(t, t.TempDir(), "d.yaml", src)

	moved := Bar{Position: BarLeft, Width: 30, Start: []string{"vitals"}, End: []string{"clock"}}
	if err := SaveLayout(path, [][]string{{"notes"}}, moved); err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"position: left", "width: 30", "top: [vitals]", "bottom: [clock]"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("save is missing %q:\n%s", want, out)
		}
	}
	// The horizontal spellings must be gone, or the file will not load.
	if strings.Contains(string(out), "left: [vitals]") {
		t.Errorf("save kept the horizontal group keys:\n%s", out)
	}
	// Comments and unexpanded values survive, as they do for rows.
	for _, want := range []string{"# my dashboard", "# the strip", "${HOME}/notes"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("save dropped %q:\n%s", want, out)
		}
	}

	// And what was written is what loads back.
	d, err := LoadDashboard(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if d.Bar.Position != BarLeft || d.Bar.Columns() != 30 {
		t.Errorf("reloaded bar = %+v", d.Bar)
	}
}

// A plain top bar stays a plain top bar: a save should not expand the short
// form into a mapping just to state the default position.
func TestSaveKeepsTheShortBarForm(t *testing.T) {
	path := write(t, t.TempDir(), "d.yaml", "name: d\nwidgets:\n  a:\n    type: clock\n  b:\n    type: notes\nbar: [a]\nrows:\n  - [b]\n")

	if err := SaveLayout(path, [][]string{{"b"}}, Bar{Position: BarTop, Start: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "bar: [a]") {
		t.Errorf("short form was expanded:\n%s", out)
	}
}
