// Package theme holds the shared colour palette and text styles.
//
// It is imported by both the TUI shell and individual widgets, so it must not
// depend on either.
//
// A theme is written down as a [Palette] — colour roles as light/dark hex
// pairs, plus the frame chrome that goes with them — and resolved into a
// [Theme] of lipgloss colours by [Resolve]. The named palettes live in
// themes.go.
package theme

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Default is the theme used when config.yaml names none.
//
// It stays the palette ctOS shipped before it had themes: an upgrade should
// not repaint a dashboard somebody has already settled into. The house look
// the project is named for is "ctos", one keystroke or one config line away.
const Default = "ember"

// Pair is one colour role written for both kinds of terminal. Leaving one side
// empty uses the other on both.
type Pair struct{ Light, Dark string }

// color resolves the pair for whichever background the terminal reports.
func (p Pair) color() lipgloss.TerminalColor {
	switch {
	case p.Light == "" && p.Dark == "":
		return lipgloss.NoColor{}
	case p.Light == "":
		return lipgloss.Color(p.Dark)
	case p.Dark == "":
		return lipgloss.Color(p.Light)
	}
	return lipgloss.AdaptiveColor{Light: p.Light, Dark: p.Dark}
}

// Chrome is the line-drawing set a theme frames its widgets with.
//
// It is part of the palette rather than a constant in the TUI because the
// corners carry as much of a theme's character as its colours do: a rounded
// box reads as friendly, a bracketed one as instrumentation.
type Chrome struct {
	TopLeft, TopRight       string
	BottomLeft, BottomRight string
	Horizontal, Vertical    string

	// TitleOpen and TitleClose bracket a frame's title. They are drawn in
	// the border colour, the title between them in the title colour.
	TitleOpen, TitleClose string
}

// Rounded is the Charm-ecosystem default: soft corners, a spaced title.
var Rounded = Chrome{
	TopLeft: "╭", TopRight: "╮",
	BottomLeft: "╰", BottomRight: "╯",
	Horizontal: "─", Vertical: "│",
	TitleOpen: "─ ", TitleClose: " ",
}

// Bracketed is the instrument-panel variant: square corners and a title in
// brackets, so a frame reads as a labelled readout rather than a card.
var Bracketed = Chrome{
	TopLeft: "┌", TopRight: "┐",
	BottomLeft: "└", BottomRight: "┘",
	Horizontal: "─", Vertical: "│",
	TitleOpen: "─[ ", TitleClose: " ]",
}

// Palette is a named theme as it is written down.
type Palette struct {
	// Name is what config.yaml and `ctos themes` call it.
	Name string
	// Summary is the one line `ctos themes` prints beside the name.
	Summary string

	// Accent marks selections, focus and highlights.
	Accent Pair

	// Text weights, lightest to heaviest.
	Faint, Dim, Text Pair

	// Semantic colours.
	Good, Warn, Bad Pair

	// Border is an unfocused frame. A focused one uses Accent.
	Border Pair

	// Ported marks a palette transcribed from someone else's published
	// scheme, so `ctos themes` can group and credit them separately from
	// the ones ctOS designed.
	Ported bool

	// Chrome is the frame's line-drawing set.
	Chrome Chrome
}

// Theme is the resolved palette for a running dashboard.
type Theme struct {
	// Name is the palette this was resolved from.
	Name string

	Accent lipgloss.TerminalColor

	// Text weights, lightest to heaviest.
	Faint lipgloss.TerminalColor
	Dim   lipgloss.TerminalColor
	Text  lipgloss.TerminalColor

	// Semantic colours.
	Good lipgloss.TerminalColor
	Warn lipgloss.TerminalColor
	Bad  lipgloss.TerminalColor

	// Frame colours and line-drawing set.
	Border      lipgloss.TerminalColor
	BorderFocus lipgloss.TerminalColor
	Chrome      Chrome
}

// Resolve builds the named theme, with accent overriding its accent colour.
//
// Both arguments are optional: an empty name is [Default], and an empty accent
// keeps the palette's own. An unknown name is an error naming the alternatives,
// because a typo in config.yaml should say so rather than silently render in
// somebody else's colours.
func Resolve(name, accent string) (Theme, error) {
	if name == "" {
		name = Default
	}
	p, ok := Lookup(name)
	if !ok {
		return Theme{}, fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(Names(), ", "))
	}
	return p.Theme(accent), nil
}

// New is the accent-only path: the default theme, optionally recoloured. It
// cannot fail, so widget tests and other callers with nothing to configure can
// take a theme in one expression.
func New(accent string) Theme {
	return palettes[Default].Theme(accent)
}

// Theme resolves a palette's colours. A non-empty accent replaces the
// palette's own on both kinds of terminal: an accent is a deliberate choice,
// and second-guessing it per background would surprise whoever set it.
func (p Palette) Theme(accent string) Theme {
	acc := p.Accent
	if accent != "" {
		acc = Pair{Light: accent, Dark: accent}
	}
	chrome := p.Chrome
	if chrome == (Chrome{}) {
		chrome = Rounded
	}
	return Theme{
		Name:   p.Name,
		Accent: acc.color(),

		Faint: p.Faint.color(),
		Dim:   p.Dim.color(),
		Text:  p.Text.color(),

		Good: p.Good.color(),
		Warn: p.Warn.color(),
		Bad:  p.Bad.color(),

		Border:      p.Border.color(),
		BorderFocus: acc.color(),
		Chrome:      chrome,
	}
}

// Lookup returns a palette by name.
func Lookup(name string) (Palette, bool) {
	p, ok := palettes[name]
	return p, ok
}

// Names lists every theme, sorted, for error messages.
func Names() []string {
	out := make([]string, 0, len(palettes))
	for name := range palettes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Palettes returns every theme in listing order: the ones ctOS designed first,
// then the ports of published schemes, each group alphabetical.
//
// This is also the order ctrl+t cycles in, so that pressing it walks the list
// `ctos themes` prints rather than an unrelated alphabetical interleaving of
// the two groups.
func Palettes() []Palette {
	out := make([]Palette, 0, len(palettes))
	for _, ported := range []bool{false, true} {
		for _, name := range Names() {
			if p := palettes[name]; p.Ported == ported {
				out = append(out, p)
			}
		}
	}
	return out
}

// Cycle is every theme name in listing order.
func Cycle() []string {
	ps := Palettes()
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

// Style helpers. Each returns a fresh style so callers may chain freely.

// TextStyle colours primary body text.
func (t Theme) TextStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.Text) }

// DimStyle colours secondary text, such as metadata columns.
func (t Theme) DimStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.Dim) }

// FaintStyle colours the least important text, such as separators.
func (t Theme) FaintStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.Faint) }

// AccentStyle colours selections and other highlights.
func (t Theme) AccentStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.Accent) }

// GoodStyle colours healthy or successful states.
func (t Theme) GoodStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.Good) }

// WarnStyle colours states that deserve attention but are not failures.
func (t Theme) WarnStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.Warn) }

// BadStyle colours errors and failed states.
func (t Theme) BadStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.Bad) }
