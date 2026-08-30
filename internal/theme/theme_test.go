package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Every palette must fill in every role. A zero colour is invisible on some
// backgrounds, and a theme is only worth shipping if it works on both.
func TestPalettesAreComplete(t *testing.T) {
	for _, p := range Palettes() {
		t.Run(p.Name, func(t *testing.T) {
			if p.Summary == "" {
				t.Error("no summary; `ctos themes` would print a blank line")
			}
			roles := map[string]Pair{
				"accent": p.Accent,
				"faint":  p.Faint,
				"dim":    p.Dim,
				"text":   p.Text,
				"good":   p.Good,
				"warn":   p.Warn,
				"bad":    p.Bad,
				"border": p.Border,
			}
			for name, pair := range roles {
				if pair.Light == "" || pair.Dark == "" {
					t.Errorf("%s: want a light and a dark value, got %+v", name, pair)
				}
				for _, hex := range []string{pair.Light, pair.Dark} {
					if !strings.HasPrefix(hex, "#") || len(hex) != 7 {
						t.Errorf("%s: %q is not a #rrggbb colour", name, hex)
					}
				}
			}
		})
	}
}

// The map key and the palette's own name have to agree, or a theme resolved by
// name reports itself as something else.
func TestPaletteNamesMatchTheirKeys(t *testing.T) {
	for key, p := range palettes {
		if p.Name != key {
			t.Errorf("palette under key %q calls itself %q", key, p.Name)
		}
	}
}

// A theme's semantic colours are the whole "is anything wrong?" story, so no
// theme — not even the near-monochrome one — may collapse them into each other.
func TestSemanticColoursStayDistinct(t *testing.T) {
	for _, p := range Palettes() {
		t.Run(p.Name, func(t *testing.T) {
			for _, side := range []struct {
				name            string
				good, warn, bad string
			}{
				{"light", p.Good.Light, p.Warn.Light, p.Bad.Light},
				{"dark", p.Good.Dark, p.Warn.Dark, p.Bad.Dark},
			} {
				if side.good == side.warn || side.warn == side.bad || side.good == side.bad {
					t.Errorf("%s: good/warn/bad are not three colours: %q %q %q",
						side.name, side.good, side.warn, side.bad)
				}
			}
		})
	}
}

// The accent marks selection and focus; good/warn/bad report state. A palette
// where one is literally the other cannot say "this row is selected" and "this
// reading is healthy" at the same time.
//
// This is equality, not a hue distance: ember's orange accent sits near its
// amber warn by design, and noir's accent is near-white, where hue means
// nothing. A metric strict enough to separate those would be measuring the
// wrong thing.
func TestAccentIsNotASemanticColour(t *testing.T) {
	for _, p := range Palettes() {
		t.Run(p.Name, func(t *testing.T) {
			for _, role := range []struct {
				name string
				pair Pair
			}{{"good", p.Good}, {"warn", p.Warn}, {"bad", p.Bad}} {
				if p.Accent == role.pair {
					t.Errorf("the accent is also %s (%v): a selected row and a %s reading would look identical",
						role.name, role.pair, role.name)
				}
			}
		})
	}
}

// Chrome must be single-cell everywhere, or a frame stops being exactly as wide
// as the box it was given.
func TestChromeIsSingleCell(t *testing.T) {
	for _, p := range Palettes() {
		c := p.Chrome
		for _, r := range []string{c.TopLeft, c.TopRight, c.BottomLeft, c.BottomRight, c.Horizontal, c.Vertical} {
			if lipgloss.Width(r) != 1 {
				t.Errorf("%s: %q is %d cells wide, want 1", p.Name, r, lipgloss.Width(r))
			}
		}
	}
}

// ctrl+t walks Cycle, so a palette missing from it is one nobody can reach
// without editing YAML.
func TestCycleReachesEveryTheme(t *testing.T) {
	cycle := Cycle()
	if len(cycle) != len(palettes) {
		t.Fatalf("cycle has %d themes, the registry has %d", len(cycle), len(palettes))
	}
	seen := map[string]bool{}
	for _, name := range cycle {
		if seen[name] {
			t.Errorf("%q appears in the cycle twice", name)
		}
		if _, ok := palettes[name]; !ok {
			t.Errorf("cycle names %q, which is not a theme", name)
		}
		seen[name] = true
	}
}

// The listing and the cycle share an order, so the ports must come after the
// ones ctOS designed rather than interleaving alphabetically with them.
func TestCycleGroupsPortsLast(t *testing.T) {
	var seenPort bool
	for _, p := range Palettes() {
		if p.Ported {
			seenPort = true
			continue
		}
		if seenPort {
			t.Errorf("%q is not a port but comes after one", p.Name)
		}
	}
	if !seenPort {
		t.Error("no palette is marked as a port; the grouping is not being exercised")
	}
}

func TestResolve(t *testing.T) {
	t.Run("empty name is the default", func(t *testing.T) {
		th, err := Resolve("", "")
		if err != nil {
			t.Fatal(err)
		}
		if th.Name != Default {
			t.Errorf("got theme %q, want %q", th.Name, Default)
		}
	})

	t.Run("unknown name lists the alternatives", func(t *testing.T) {
		_, err := Resolve("watchdogs", "")
		if err == nil {
			t.Fatal("want an error for an unknown theme")
		}
		for _, name := range Names() {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error should name %q: %v", name, err)
			}
		}
	})

	t.Run("accent overrides the palette's own", func(t *testing.T) {
		th, err := Resolve("noir", "#ff00ff")
		if err != nil {
			t.Fatal(err)
		}
		want := lipgloss.AdaptiveColor{Light: "#ff00ff", Dark: "#ff00ff"}
		if th.Accent != want {
			t.Errorf("accent = %v, want %v", th.Accent, want)
		}
		// The focus border follows the accent, so an override moves it too.
		if th.BorderFocus != want {
			t.Errorf("focus border = %v, want %v", th.BorderFocus, want)
		}
		// Everything else stays the theme's.
		if th.Text != palettes["noir"].Text.color() {
			t.Error("an accent override should not touch the text colour")
		}
	})

	t.Run("no accent keeps the palette's own", func(t *testing.T) {
		th, err := Resolve("dedsec", "")
		if err != nil {
			t.Fatal(err)
		}
		if th.Accent != palettes["dedsec"].Accent.color() {
			t.Errorf("accent = %v, want the palette's own", th.Accent)
		}
	})
}

// New is the path every widget test takes, so it has to hand back a usable
// theme rather than a zero one.
func TestNewIsTheDefaultTheme(t *testing.T) {
	th := New("")
	if th.Name != Default {
		t.Errorf("New(\"\") gave theme %q, want %q", th.Name, Default)
	}
	if th.Chrome == (Chrome{}) {
		t.Error("New(\"\") gave a theme with no chrome; frames would draw blank")
	}
}
