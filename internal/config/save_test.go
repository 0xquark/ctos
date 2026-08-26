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
	if err := SaveRows(path, newRows); err != nil {
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
	if err := SaveRows(path, [][]string{{"clock", "notes"}, {"hackernews"}}); err != nil {
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
	if err := SaveRows(path, want); err != nil {
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
	if err := SaveRows(path, [][]string{{"clock"}, {"notes"}, {"hackernews"}}); err != nil {
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
		err := SaveRows(filepath.Join(t.TempDir(), "nope.yaml"), [][]string{{"a"}})
		if err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("not a mapping", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "d.yaml")
		if err := os.WriteFile(path, []byte("- just\n- a list\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := SaveRows(path, [][]string{{"a"}}); err == nil {
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
		if err := SaveRows(path, [][]string{{"clock"}, {"notes", "hackernews"}}); err != nil {
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
