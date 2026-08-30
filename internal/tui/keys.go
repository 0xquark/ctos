package tui

import (
	"fmt"
	"strings"

	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
)

// helpEntry is one key and what it does.
type helpEntry struct{ key, desc string }

// globalHelp lists keys the dashboard itself handles. Widget-local keys are
// added contextually from the focused widget's actions.
var globalHelp = []helpEntry{
	{"tab", "next widget"},
	{"shift+tab", "previous widget"},
	{"↑ ↓", "move within widget"},
	{"r", "refresh widget"},
	{"ctrl+l", "rearrange layout"},
	{"?", "toggle help"},
	{"q", "quit"},
}

// footer renders the one-line key hints under the dashboard.
func footer(t theme.Theme, w widget.Widget, expanded bool) string {
	if expanded {
		return expandedHelp(t, w)
	}

	// A widget taking text input has swallowed the global keys, so offering
	// them would be a lie.
	if widget.Grabbing(w) {
		entries := []helpEntry{{"↵", "apply"}, {"esc", "cancel"}}
		parts := make([]string, len(entries))
		for i, e := range entries {
			parts[i] = t.AccentStyle().Render(e.key) + " " + t.DimStyle().Render(e.desc)
		}
		return " " + strings.Join(parts, t.FaintStyle().Render("  ·  "))
	}

	entries := []helpEntry{{"tab", "focus"}, {"↑↓", "move"}}
	if w != nil {
		if actions := w.Actions(); len(actions) > 0 {
			entries = append(entries, helpEntry{"enter", actions[0].Name})
		}
	}
	entries = append(entries, helpEntry{"?", "help"}, helpEntry{"q", "quit"})

	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = t.AccentStyle().Render(e.key) + " " + t.DimStyle().Render(e.desc)
	}
	return " " + strings.Join(parts, t.FaintStyle().Render("  ·  "))
}

// expandedHelp lists every binding, including the focused widget's actions.
func expandedHelp(t theme.Theme, w widget.Widget) string {
	entries := append([]helpEntry(nil), globalHelp...)
	if w != nil {
		for i, a := range w.Actions() {
			key := "enter"
			if i > 0 {
				// Reaching a second action needs the action menu,
				// which is not built yet. See ADR-008.
				key = "—"
			}
			entries = append(entries, helpEntry{key, fmt.Sprintf("%s (%s)", a.Name, w.Title())})
		}
	}

	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(" " + t.AccentStyle().Render(pad(e.key, 10)) + t.DimStyle().Render(e.desc))
	}
	return b.String()
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s + " "
	}
	return s + strings.Repeat(" ", w-len(s))
}

// layoutFooter renders the key hints for layout mode.
func layoutFooter(t theme.Theme, moving string, dirty, hasBar bool) string {
	entries := []helpEntry{
		{"←→", "reorder"},
		{"↑↓", "row"},
		{"↵", "new row"},
		{"tab", "pick widget"},
	}
	// The bar key is only advertised on a dashboard that has a bar, so the
	// footer never offers a binding that will not do anything.
	if hasBar {
		entries = append(entries, helpEntry{"b", "move bar"})
	}
	entries = append(entries, helpEntry{"s", "save"}, helpEntry{"esc", "cancel"})

	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = t.AccentStyle().Render(e.key) + " " + t.DimStyle().Render(e.desc)
	}

	head := " " + t.WarnStyle().Bold(true).Render("LAYOUT")
	if moving != "" {
		head += " " + t.DimStyle().Render("moving ") + t.TextStyle().Render(moving)
	}
	if dirty {
		head += " " + t.WarnStyle().Render("• unsaved")
	}

	return head + "\n " + strings.Join(parts, t.FaintStyle().Render("  ·  "))
}

// helpHeight is how many terminal rows the footer occupies.
func helpHeight(expanded bool, w widget.Widget) int {
	if !expanded {
		return 1
	}
	n := len(globalHelp)
	if w != nil {
		n += len(w.Actions())
	}
	return n
}
