// Package clock renders the current time, optionally as large block digits.
package clock

import (
	"strings"
	"time"

	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func init() {
	widget.Register(widget.Spec{
		Name:    "clock",
		Summary: "the current time, drawn as large block digits",
		New:     New,
		Example: `type: clock
format: "15:04:05"        # Go time layout
date_format: "Mon 02 Jan 2006"
big: true                 # block digits, falling back to plain text when narrow
title: clock`,
	})
}

// tickMsg drives the once-per-second redraw.
type tickMsg time.Time

type config struct {
	Format     string `yaml:"format"`
	DateFormat string `yaml:"date_format"`
	Big        bool   `yaml:"big"`
}

// Clock shows the current local time.
type Clock struct {
	widget.Base
	cfg   config
	theme theme.Theme
	now   time.Time
}

// New builds a clock widget from its dashboard configuration.
func New(ctx widget.Context) (widget.Widget, error) {
	cfg := config{
		Format:     "15:04:05",
		DateFormat: "Mon 02 Jan 2006",
		Big:        true,
	}
	if err := ctx.Decode(&cfg); err != nil {
		return nil, err
	}
	return &Clock{cfg: cfg, theme: ctx.Theme, now: time.Now()}, nil
}

// Init schedules the first tick.
func (c *Clock) Init() tea.Cmd { return c.tick() }

// Update advances the displayed time.
func (c *Clock) Update(msg tea.Msg) tea.Cmd {
	if t, ok := msg.(tickMsg); ok {
		c.now = time.Time(t)
		return c.tick()
	}
	return nil
}

// tick fires on the next whole second, so the display stays aligned with the
// wall clock instead of drifting by the render time.
func (c *Clock) tick() tea.Cmd {
	next := time.Now().Truncate(time.Second).Add(time.Second)
	return c.Tick(time.Until(next), func(t time.Time) tea.Msg { return tickMsg(t) })
}

// View renders the time, falling back to plain text when the block digits
// would not fit.
func (c *Clock) View() string {
	timeStr := c.now.Format(c.cfg.Format)
	dateStr := c.now.Format(c.cfg.DateFormat)

	var body string
	if big, ok := c.renderBig(timeStr); ok {
		body = big
	} else {
		body = c.theme.AccentStyle().Bold(true).Render(timeStr)
	}

	out := body
	if dateStr != "" && c.H >= lipgloss.Height(body)+2 {
		out = lipgloss.JoinVertical(lipgloss.Center, body, "", c.theme.DimStyle().Render(dateStr))
	}
	if c.W <= 0 || c.H <= 0 {
		return out
	}
	return lipgloss.Place(c.W, c.H, lipgloss.Center, lipgloss.Center, out)
}

// glyphs are 3 rows tall and 3 cells wide, drawn with the same rounded box
// characters as the widget frames so the clock reads as part of the same
// system. Anything not listed renders as a fallback, so an unusual format
// string degrades to plain text instead of breaking the layout.
var glyphs = map[rune][3]string{
	'0': {"╭─╮", "│ │", "╰─╯"},
	'1': {"  ╷", "  │", "  ╵"},
	'2': {"╭─╮", "╭─╯", "╰─╴"},
	'3': {"╭─╮", " ─┤", "╰─╯"},
	'4': {"╷ ╷", "╰─┤", "  ╵"},
	'5': {"╭─╴", "╰─╮", "╰─╯"},
	'6': {"╭─╴", "├─╮", "╰─╯"},
	'7': {"╭─╮", "  │", "  ╵"},
	'8': {"╭─╮", "├─┤", "╰─╯"},
	'9': {"╭─╮", "╰─┤", "╰─╯"},
	':': {"   ", " ▪ ", "   "},
	'.': {"   ", "   ", " ▪ "},
	'-': {"   ", " ─ ", "   "},
	' ': {"   ", "   ", "   "},
	'A': {"╭─╮", "├─┤", "╵ ╵"},
	'P': {"╭─╮", "├─╯", "╵  "},
	'M': {"╭┬╮", "│││", "╵╵╵"},
}

const (
	glyphRows  = 3
	glyphWidth = 3
)

// renderBig draws s as block digits. It reports false when the widget is too
// small, or when the string contains a character with no glyph.
func (c *Clock) renderBig(s string) (string, bool) {
	if !c.cfg.Big {
		return "", false
	}
	runes := []rune(s)
	if len(runes) == 0 {
		return "", false
	}

	width := len(runes)*(glyphWidth+1) - 1
	if c.W < width || c.H < glyphRows {
		return "", false
	}
	for _, r := range runes {
		if _, ok := glyphs[r]; !ok {
			return "", false
		}
	}

	rows := make([]string, glyphRows)
	for i := range rows {
		parts := make([]string, len(runes))
		for j, r := range runes {
			parts[j] = glyphs[r][i]
		}
		rows[i] = strings.Join(parts, " ")
	}
	return c.theme.AccentStyle().Render(strings.Join(rows, "\n")), true
}
