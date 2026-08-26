// Package theme holds the shared colour palette and text styles.
//
// It is imported by both the TUI shell and individual widgets, so it must not
// depend on either.
package theme

import "github.com/charmbracelet/lipgloss"

// DefaultAccent is used when config.yaml sets no accent colour.
const DefaultAccent = "#ff6b35"

// Theme is the resolved palette for a running dashboard.
type Theme struct {
	Accent lipgloss.TerminalColor

	// Text weights, lightest to heaviest.
	Faint lipgloss.TerminalColor
	Dim   lipgloss.TerminalColor
	Text  lipgloss.TerminalColor

	// Semantic colours.
	Good lipgloss.TerminalColor
	Warn lipgloss.TerminalColor
	Bad  lipgloss.TerminalColor

	// Frame colours.
	Border      lipgloss.TerminalColor
	BorderFocus lipgloss.TerminalColor
}

// New builds a theme from an accent colour. An empty or malformed accent falls
// back to DefaultAccent.
func New(accent string) Theme {
	if accent == "" {
		accent = DefaultAccent
	}
	return Theme{
		Accent: lipgloss.Color(accent),

		Faint: lipgloss.AdaptiveColor{Light: "#b0b0b0", Dark: "#4a4a4a"},
		Dim:   lipgloss.AdaptiveColor{Light: "#777777", Dark: "#8a8a8a"},
		Text:  lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e4e4e4"},

		Good: lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"},
		Warn: lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"},
		Bad:  lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"},

		Border:      lipgloss.AdaptiveColor{Light: "#d0d0d0", Dark: "#3a3a3a"},
		BorderFocus: lipgloss.Color(accent),
	}
}

// Style helpers. Each returns a fresh style so callers may chain freely.

func (t Theme) TextStyle() lipgloss.Style   { return lipgloss.NewStyle().Foreground(t.Text) }
func (t Theme) DimStyle() lipgloss.Style    { return lipgloss.NewStyle().Foreground(t.Dim) }
func (t Theme) FaintStyle() lipgloss.Style  { return lipgloss.NewStyle().Foreground(t.Faint) }
func (t Theme) AccentStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.Accent) }
func (t Theme) GoodStyle() lipgloss.Style   { return lipgloss.NewStyle().Foreground(t.Good) }
func (t Theme) WarnStyle() lipgloss.Style   { return lipgloss.NewStyle().Foreground(t.Warn) }
func (t Theme) BadStyle() lipgloss.Style    { return lipgloss.NewStyle().Foreground(t.Bad) }
