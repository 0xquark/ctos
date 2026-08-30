package processes

import (
	"fmt"
	"strings"
	"time"

	"github.com/0xquark/ctos/internal/humanize"
	"github.com/0xquark/ctos/internal/procs"
	"github.com/charmbracelet/lipgloss"
)

// detailLines renders exactly h rows for the pane below the list. Returning a
// fixed count keeps the widget's total height predictable, which the frame
// depends on.
func (p *Processes) detailLines(h int) []string {
	var lines []string
	if !p.list.Empty() {
		selected := p.rows[p.list.Cursor()]
		if p.detail == detailLogs {
			lines = p.logLines(selected, h)
		} else {
			lines = p.infoLines(selected, h)
		}
	}

	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines
}

// infoLines describes the selected process and where it sits in the tree.
//
// The tree gets its budget first. The prose above it — the expanded state, the
// full command — largely restates the table row the cursor is already on,
// whereas the tree is the one thing the pane adds, so in a short pane it is
// the prose that gives way rather than the tree.
func (p *Processes) infoLines(proc procs.Process, h int) []string {
	if h <= 0 {
		return nil
	}

	preamble := []string{
		p.identityLine(proc),
		p.factsLine(proc),
		"",
		p.theme.DimStyle().Render(truncate(" "+strings.TrimSpace(proc.Command), p.W)),
		"",
	}

	if p.index == nil {
		return preamble[:min(len(preamble), h)]
	}

	minTree := min(3, max(1, h-2))
	lines := preamble[:min(len(preamble), max(1, h-minTree))]

	if tree := h - len(lines); tree > 0 {
		lines = append(lines, p.ancestryLines(proc, tree)...)
	}
	return lines
}

// identityLine is the pane's title: what this process is.
func (p *Processes) identityLine(proc procs.Process) string {
	line := p.theme.AccentStyle().Bold(true).Render(" "+proc.Name()) +
		p.theme.FaintStyle().Render(" · ") +
		p.theme.TextStyle().Render(fmt.Sprintf("pid %d", proc.PID)) +
		p.theme.FaintStyle().Render(" · ") +
		p.theme.DimStyle().Render(proc.User)
	return truncate(line, p.W)
}

// factsLine is the numbers that did not fit in the table row.
func (p *Processes) factsLine(proc procs.Process) string {
	facts := []string{
		p.theme.DimStyle().Render("state ") + p.theme.TextStyle().Render(describeState(proc.State)),
		p.theme.DimStyle().Render(humanize.Bytes(proc.RSS)) + p.theme.FaintStyle().Render(fmt.Sprintf(" (%.1f%%)", proc.Mem)),
		p.theme.DimStyle().Render(fmt.Sprintf("cpu %.1f%%", proc.CPU)),
	}
	if proc.Elapsed > 0 {
		started := time.Now().Add(-proc.Elapsed)
		facts = append(facts, p.theme.FaintStyle().Render("up "+shortDuration(proc.Elapsed)))
		facts = append(facts, p.theme.FaintStyle().Render(started.Format("02 Jan 15:04")))
	}

	sep := p.theme.FaintStyle().Render(" · ")
	for n := len(facts); n > 0; n-- {
		line := " " + strings.Join(facts[:n], sep)
		if lipgloss.Width(line) <= p.W {
			return line
		}
	}
	return ""
}

// ancestryLines draws the selected process in its family: the chain of
// parents above it and its direct children below.
//
// The pane is usually shorter than the family, so the budget is spent from the
// middle outward. The selected process always gets its line; then the nearest
// ancestors, since a distant grandparent explains less than the immediate one;
// then the children. Whatever is dropped is counted rather than silently cut,
// so the tree never implies a process has fewer relatives than it does.
func (p *Processes) ancestryLines(proc procs.Process, budget int) []string {
	if budget < 1 {
		return nil
	}
	if budget == 1 {
		// Only room for the answer to "which process is this".
		return []string{p.treeLine(proc, 0, false, false, true)}
	}

	chain := p.index.Ancestors(proc.PID)
	kids := p.index.Children(proc.PID)
	total := p.index.Descendants(proc.PID)

	above, hiddenAbove := fitAncestors(len(chain), len(kids), budget)
	chain = chain[len(chain)-above:]

	// One row goes to the selected process; one more to the "N above" note.
	used := 1 + above
	if hiddenAbove > 0 {
		used++
	}
	below, hiddenBelow := fitChildren(len(kids), total, budget-used)

	lines := make([]string, 0, budget)
	depth := 0

	if hiddenAbove > 0 {
		lines = append(lines, p.elisionLine(depth, fmt.Sprintf("… %d more above", hiddenAbove)))
		depth++
	}
	for _, a := range chain {
		lines = append(lines, p.treeLine(a, depth, depth > 0, false, false))
		depth++
	}
	lines = append(lines, p.treeLine(proc, depth, depth > 0, false, true))

	for i := 0; i < below; i++ {
		lines = append(lines, p.treeLine(kids[i], depth+1, true, i < len(kids)-1, false))
	}
	if hiddenBelow > 0 {
		lines = append(lines, p.elisionLine(depth+1, fmt.Sprintf("… %d more below", hiddenBelow)))
	}
	return lines
}

// fitAncestors decides how many parents fit above the selected process,
// keeping the nearest and reporting how many were left out.
func fitAncestors(have, kidCount, budget int) (show, hidden int) {
	// Reserve the selected process's own row, plus one for a child if it has
	// any: seeing that it has children matters more than a fourth ancestor.
	room := budget - 1
	if kidCount > 0 {
		room--
	}
	// Ancestors take at most half of what is left, so a deep chain cannot
	// crowd out the children entirely.
	if room > budget/2 {
		room = budget / 2
	}
	if room < 0 {
		room = 0
	}

	if have <= room {
		return have, 0
	}
	if room > 0 {
		room-- // a row is needed to say what was dropped
	}
	return room, have - room
}

// fitChildren decides how many children fit below. The hidden count covers the
// whole subtree, not just the direct children, since that is the number the
// reader wants when deciding whether to look closer.
func fitChildren(have, subtree, room int) (show, hidden int) {
	if room <= 0 {
		if subtree > 0 {
			return 0, subtree
		}
		return 0, 0
	}
	if have <= room && subtree == have {
		return have, 0
	}
	if have > room {
		room-- // a row is needed to say what was dropped
	}
	show = min(max(room, 0), have)
	return show, subtree - show
}

// elisionLine marks where the tree was cut.
func (p *Processes) elisionLine(depth int, text string) string {
	return p.theme.FaintStyle().Render(truncate(" "+strings.Repeat("   ", depth)+text, p.W))
}

// treeLine renders one node. The selected process is highlighted so the eye
// finds it in the middle of the chain.
func (p *Processes) treeLine(proc procs.Process, depth int, branch, moreSiblings, self bool) string {
	connector := ""
	switch {
	case branch && moreSiblings:
		connector = "├─ "
	case branch:
		connector = "└─ "
	}

	// Indent by the connector's own width so a child's name starts directly
	// under its parent's.
	prefix := " " + strings.Repeat("   ", depth) + connector
	width := p.W - lipgloss.Width(prefix)
	if width < 1 {
		return ""
	}

	style := p.theme.DimStyle()
	if self {
		style = p.theme.AccentStyle().Bold(true)
	}
	label := fmt.Sprintf("%s (%d)", proc.Name(), proc.PID)

	return p.theme.FaintStyle().Render(prefix) + style.Render(humanize.Truncate(label, width))
}

// describeState expands the ps state code, which is otherwise a single letter
// nobody remembers.
func describeState(code string) string {
	if code == "" {
		return "?"
	}
	name := map[byte]string{
		'R': "running",
		'S': "sleeping",
		'I': "idle",
		'D': "waiting on I/O",
		'T': "stopped",
		'U': "uninterruptible",
		'Z': "zombie",
	}[code[0]]
	if name == "" {
		return code
	}
	return name
}

// logLines renders the log pane: a header saying what is being shown, then the
// newest entries that fit.
func (p *Processes) logLines(proc procs.Process, h int) []string {
	head := p.theme.DimStyle().Render(" logs ") +
		p.theme.FaintStyle().Render(fmt.Sprintf("· pid %d · last %s", proc.PID, procs.CompactDuration(p.logWindow)))

	// Order matters: lines we actually hold win over any explanation of why
	// we might not have them. LogsSupported describes this machine, and the
	// lines will not always have come from it.
	switch {
	case p.logsLoading:
		return []string{head, "", p.theme.DimStyle().Render(" reading the system log…")}
	case p.logsErr != nil:
		return append([]string{head, ""}, p.wrapNotice(p.theme.BadStyle(), "⚠ "+p.logsErr.Error())...)
	case len(p.logs) > 0:
		// fall through to the renderer below
	case !procs.LogsSupported():
		return append([]string{head, ""}, p.wrapNotice(p.theme.DimStyle(),
			"no system log source on this machine. macOS uses `log show`; Linux needs journalctl.")...)
	default:
		return append([]string{head, ""}, p.wrapNotice(p.theme.DimStyle(),
			"nothing logged in the last "+procs.CompactDuration(p.logWindow)+". A process that writes to stdout under a service manager often records nothing here.")...)
	}

	head += p.theme.FaintStyle().Render(fmt.Sprintf(" · %d lines", len(p.logs)))

	// A log is read newest-last, and the pane is shorter than the buffer, so
	// show the tail rather than the head.
	tail := p.logs
	if room := h - 1; room > 0 && len(tail) > room {
		tail = tail[len(tail)-room:]
	}

	lines := make([]string, 0, len(tail)+1)
	lines = append(lines, head)
	for _, l := range tail {
		lines = append(lines, p.theme.TextStyle().Render(truncate(" "+trimLogLine(l), p.W)))
	}
	return lines
}

// wrapNotice breaks an explanatory sentence across the pane width.
func (p *Processes) wrapNotice(style lipgloss.Style, text string) []string {
	width := max(1, p.W-1)
	var out []string
	for _, chunk := range wrap(text, width) {
		out = append(out, style.Render(" "+chunk))
	}
	return out
}

func wrap(s string, width int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// trimLogLine drops the date from a log timestamp, keeping the time. Both
// `log show` and `journalctl` lead with a date that is almost always today's,
// and the pane needs those cells for the message.
func trimLogLine(s string) string {
	// "2026-08-27 00:23:17.578 Df launchd[1:1b13a] …"
	if len(s) > 11 && s[4] == '-' && s[7] == '-' && s[10] == ' ' {
		return s[11:]
	}
	return s
}
