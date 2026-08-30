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

  # The clock sits at the right-hand end of the status bar, where a terminal
  # puts the time. Give it a row of its own and it draws large block digits.
  clock:
    type: clock
    format: "15:04:05"
    date_format: "Mon 02 Jan"
    big: true

  vitals:
    type: system
    # "auto" reads the box: a strip when it is one line tall, the panel of
    # labelled bars when it is taller. Set "bar" or "rows" to pin one.
    style: auto
    refresh: 3s
    # One row per mount point.
    disks: ["/"]
    # Interface to measure. Empty sums every interface but loopback.
    interface: ""

  hackernews:
    type: hackernews
    limit: 20
    refresh: 5m

  processes:
    type: processes
    # cpu, mem, pid or name. In ctOS press c/m/p/n to sort, again to reverse.
    sort: cpu
    refresh: 3s
    # Set to "me" for your own processes only, or a username.
    user: ""
    # Drop processes using no CPU at all.
    hide_idle: false
    # Detail pane under the list: ancestry tree, or logs with "l".
    detail: true
    # Rows given to the detail pane; 0 splits the space in half.
    detail_lines: 0
    # How far back the log view looks.
    log_window: 5m

# Widgets drawn as a frameless strip on one edge: vitals on the left, the clock
# on the right. The strip is chrome, not a pane: no border, no focus, one line.
#
# Press ctrl+l then "b" to move it round the four edges, "s" to keep it. In YAML
# that is "position:": top (the default), bottom, left or right. A vertical bar
# names its groups "top:" and "bottom:" instead, and takes a "width:" in columns.
bar:
  left: [vitals]
  right: [clock]

rows:
  - [notes]
  - [processes, hackernews]
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
		{SettingsFile(dir), defaultConfigYAML},
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
