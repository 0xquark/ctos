package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xquark/ctos/internal/config"
)

// The frame label comes from the shell, not from each widget's config struct:
// a dashboard's "title:" wins, then the type's own default, then its name.
func TestFrameTitles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "titles.yaml")
	dash := `
name: titles
widgets:
  feed:
    type: hackernews
  named:
    type: hackernews
    title: orange site
  bare:
    type: clock
    title: ""
rows:
  - [feed, named, bare]
`
	if err := os.WriteFile(path, []byte(dash), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := config.LoadDashboard(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(&config.Config{}, d)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"feed":  "hacker news", // the type's default, not its name
		"named": "orange site", // the dashboard overrides it
		"bare":  "",            // an explicit empty title is honoured
	}
	for name, title := range want {
		if got := m.byName[name].Title(); got != title {
			t.Errorf("%s title = %q, want %q", name, got, title)
		}
	}

	m.w, m.h, m.ready = 120, 24, true
	m.resize()
	view := m.View()
	for _, title := range []string{"hacker news", "orange site"} {
		if !strings.Contains(view, title) {
			t.Errorf("rendered dashboard does not show %q", title)
		}
	}
}
