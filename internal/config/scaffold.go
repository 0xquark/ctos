package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const defaultConfigYAML = `# ctOS global settings.
# Dashboards live in dashboards/*.yaml next to this file.

# Command used to open files. Falls back to $EDITOR, then vi.
editor: ${EDITOR:-vi}

# Dashboard shown at startup. Matches a filename in dashboards/ without .yaml.
default_dashboard: home

theme:
  # Any hex colour. Used for focus borders, selections and highlights.
  accent: "#ff6b35"

refresh:
  # Polling interval for widgets that don't set their own.
  default: 30s
`

const defaultDashboardYAML = `# The default ctOS dashboard.
#
# "widgets:" defines each widget by name; "rows:" arranges them.
# Widgets sharing a row split the available width evenly.
#
# Press ctrl+l in ctOS to rearrange this layout visually and save it back here.

name: home

widgets:
  clock:
    type: clock
    # Go reference time layout. See pkg.go.dev/time#pkg-constants
    format: "15:04:05"
    date_format: "Mon 02 Jan 2006"
    # Draw the time as large digits when there's room.
    big: true

  notes:
    type: notes
    # Point this at your own notes directory.
    path: ~/notes
    # Search sub-directories too.
    recursive: false
    # Only files with these extensions are listed.
    extensions: [".md", ".txt"]
    # Show the selected note's contents below the list.
    preview: true
    # Rows given to the preview; 0 splits the widget in half.
    preview_lines: 0

  hackernews:
    type: hackernews
    limit: 20
    refresh: 5m

rows:
  - [clock, notes]
  - [hackernews]
`

// Scaffold writes a starter config.yaml and dashboards/home.yaml into dir.
// Existing files are never overwritten; their paths are returned as skipped.
func Scaffold(dir string) (written, skipped []string, err error) {
	if err := os.MkdirAll(DashboardsDir(dir), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create %s: %w", dir, err)
	}

	files := []struct {
		path    string
		content string
	}{
		{ConfigFile(dir), defaultConfigYAML},
		{filepath.Join(DashboardsDir(dir), "home.yaml"), defaultDashboardYAML},
	}

	for _, f := range files {
		if _, statErr := os.Stat(f.path); statErr == nil {
			skipped = append(skipped, f.path)
			continue
		}
		if err := os.WriteFile(f.path, []byte(f.content), 0o644); err != nil {
			return written, skipped, fmt.Errorf("write %s: %w", f.path, err)
		}
		written = append(written, f.path)
	}
	return written, skipped, nil
}

// EnsureNotesDir creates the sample notes directory with a starter note, so a
// fresh install has something to show in the notes widget.
func EnsureNotesDir(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	welcome := filepath.Join(dir, "welcome.md")
	if _, err := os.Stat(welcome); err == nil {
		return nil
	}
	return os.WriteFile(welcome, []byte(`# Welcome to ctOS

This note exists so the notes widget has something to show.

- Press tab to move focus between widgets.
- Press enter on a note to open it in your editor.
- Point the widget somewhere real by editing "path:" in
  dashboards/home.yaml.
`), 0o644)
}
