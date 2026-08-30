package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The whole point of rewriting one key is that the rest of the file survives:
// the comments explaining it, and every other setting.
func TestSaveThemeKeepsTheRestOfTheFile(t *testing.T) {
	dir := t.TempDir()
	path := SettingsFile(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := `# ctOS global settings.
editor: nvim
default_dashboard: work

theme:
  # Pick one with ctrl+t.
  name: ember
  accent: "#ff6b35"

refresh:
  default: 45s
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SaveTheme(path, "dedsec"); err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	for _, want := range []string{
		"name: dedsec",
		"# ctOS global settings.",
		"# Pick one with ctrl+t.",
		"editor: nvim",
		"default_dashboard: work",
		"default: 45s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten config lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ember") {
		t.Errorf("the old theme name is still in the file:\n%s", got)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("the rewritten file no longer loads: %v", err)
	}
	if cfg.Theme.Name != "dedsec" {
		t.Errorf("reloaded theme name = %q", cfg.Theme.Name)
	}
}

// An accent left over from the previous theme would tint the new palette in
// the old one's colour, which is every theme reduced to one of them. Switching
// theme takes the accent with it.
func TestSaveThemeDropsTheAccentOverride(t *testing.T) {
	dir := t.TempDir()
	path := SettingsFile(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The shape `ctos init` wrote before themes existed.
	original := `editor: vi
theme:
  # Any hex colour. Used for focus borders, selections and highlights.
  accent: "#ff6b35"
  name: dedsec
refresh:
  default: 30s
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SaveTheme(path, "blume"); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Accent != "" {
		t.Errorf("accent override survived the theme switch: %q", cfg.Theme.Accent)
	}
	if cfg.Theme.Name != "blume" {
		t.Errorf("theme name = %q, want %q", cfg.Theme.Name, "blume")
	}

	out, _ := os.ReadFile(path)
	if strings.Contains(string(out), "#ff6b35") {
		t.Errorf("the old accent is still in the file:\n%s", out)
	}
	// The rest of the file is still not ours to touch.
	for _, want := range []string{"editor: vi", "default: 30s"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("rewritten config lost %q:\n%s", want, out)
		}
	}
}

// ctOS runs without a config file, so choosing a theme must not require having
// run `ctos init` first.
func TestSaveThemeWritesTheKeysItNeeds(t *testing.T) {
	cases := []struct {
		name    string
		content *string // nil means the file does not exist
	}{
		{"no file at all", nil},
		{"empty file", ptr("")},
		{"comments only", ptr("# nothing set yet\n")},
		{"no theme block", ptr("editor: vi\n")},
		{"empty theme block", ptr("theme:\nrefresh:\n  default: 10s\n")},
		{"accent but no name", ptr("theme:\n  accent: \"#123456\"\n")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := SettingsFile(dir)
			if tc.content != nil {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(*tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			if err := SaveTheme(path, "noir"); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Theme.Name != "noir" {
				out, _ := os.ReadFile(path)
				t.Errorf("theme name = %q, want %q; file is:\n%s", cfg.Theme.Name, "noir", out)
			}
		})
	}
}

// Saving twice must not stack up theme blocks or duplicate the key.
func TestSaveThemeIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := SettingsFile(dir)

	for _, name := range []string{"ctos", "blume", "blume"} {
		if err := SaveTheme(path, name); err != nil {
			t.Fatal(err)
		}
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(out), "theme:"); n != 1 {
		t.Errorf("want one theme block, got %d:\n%s", n, out)
	}
	if n := strings.Count(string(out), "name:"); n != 1 {
		t.Errorf("want one name key, got %d:\n%s", n, out)
	}
}

func ptr(s string) *string { return &s }
