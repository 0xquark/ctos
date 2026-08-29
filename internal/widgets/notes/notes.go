// Package notes lists markdown files in a directory and opens them in the
// user's editor.
package notes

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0xquark/ctos/internal/humanize"
	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func init() { widget.Register("notes", New) }

type config struct {
	Path       string   `yaml:"path"`
	Recursive  bool     `yaml:"recursive"`
	Extensions []string `yaml:"extensions"`
	Limit      int      `yaml:"limit"`
	Title      string   `yaml:"title"`

	// Preview shows the selected note's contents below the list.
	Preview bool `yaml:"preview"`
	// PreviewLines fixes the preview height; 0 splits the widget in half.
	PreviewLines int `yaml:"preview_lines"`
}

type note struct {
	path    string
	display string
	mod     time.Time
	size    int64
}

// loadedMsg carries a directory scan back to the widget that asked for it.
type loadedMsg struct {
	name  string
	notes []note
	err   error
}

// editedMsg reports that the editor exited, so the list can be rescanned.
type editedMsg struct {
	name string
	err  error
}

// previewMsg carries a note's contents back for the preview pane.
type previewMsg struct {
	name  string
	path  string
	lines []string
	err   error
}

// Notes lists note files, newest first.
type Notes struct {
	widget.Base
	name   string
	cfg    config
	theme  theme.Theme
	editor string

	notes  []note
	cursor int
	offset int
	err    error
	loaded bool

	// Preview of the selected note, loaded lazily and cached by path.
	previewPath  string
	previewLines []string
	previewErr   error
}

// New builds a notes widget from its dashboard configuration.
func New(ctx widget.Context) (widget.Widget, error) {
	cfg := config{
		Path:       "~/notes",
		Extensions: []string{".md", ".txt"},
		Limit:      200,
		Title:      "notes",
		Preview:    true,
	}
	if ctx.Node != nil {
		if err := ctx.Node.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("notes %q: %w", ctx.Name, err)
		}
	}
	if cfg.Path == "" {
		return nil, fmt.Errorf("notes %q: \"path:\" is required", ctx.Name)
	}
	if cfg.Limit <= 0 {
		cfg.Limit = 200
	}
	return &Notes{name: ctx.Name, cfg: cfg, theme: ctx.Theme, editor: ctx.Editor}, nil
}

// Title is the label drawn in the widget frame.
func (n *Notes) Title() string { return n.cfg.Title }

// Init kicks off the first directory scan.
func (n *Notes) Init() tea.Cmd { return n.scan() }

// Update handles navigation keys and scan results.
func (n *Notes) Update(msg tea.Msg) (widget.Widget, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.name != n.name {
			return n, nil
		}
		n.loaded = true
		n.notes, n.err = msg.notes, msg.err
		if n.cursor >= len(n.notes) {
			n.cursor = max(0, len(n.notes)-1)
		}
		return n, n.syncPreview()

	case previewMsg:
		if msg.name != n.name {
			return n, nil
		}
		// A slower read may land after the cursor has moved on.
		if msg.path == n.selectedPath() {
			n.previewPath = msg.path
			n.previewLines, n.previewErr = msg.lines, msg.err
		}
		return n, nil

	case editedMsg:
		if msg.name != n.name {
			return n, nil
		}
		// The file may have been renamed, or its contents changed.
		n.previewPath = ""
		return n, n.scan()

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			n.move(-1)
		case "down", "j":
			n.move(1)
		case "home", "g":
			n.cursor = 0
		case "end", "G":
			n.cursor = max(0, len(n.notes)-1)
		case "r":
			n.previewPath = ""
			return n, n.scan()
		}
		n.clampScroll()
		return n, n.syncPreview()
	}
	return n, nil
}

// selectedPath is the path under the cursor, or "" when the list is empty.
func (n *Notes) selectedPath() string {
	if n.cursor < 0 || n.cursor >= len(n.notes) {
		return ""
	}
	return n.notes[n.cursor].path
}

// syncPreview loads the selected note when the selection has moved to a file
// the preview pane is not already showing.
func (n *Notes) syncPreview() tea.Cmd {
	if !n.cfg.Preview {
		return nil
	}
	path := n.selectedPath()
	if path == "" || path == n.previewPath {
		return nil
	}
	name := n.name
	return func() tea.Msg {
		lines, err := readPreview(path)
		return previewMsg{name: name, path: path, lines: lines, err: err}
	}
}

func (n *Notes) move(delta int) {
	if len(n.notes) == 0 {
		return
	}
	n.cursor = min(max(n.cursor+delta, 0), len(n.notes)-1)
}

// clampScroll keeps the cursor inside the list pane.
func (n *Notes) clampScroll() {
	listH, _ := n.split()
	n.clampScrollTo(listH)
}

// clampScrollTo scrolls so the cursor is visible in a pane of the given height.
func (n *Notes) clampScrollTo(height int) {
	if height <= 0 {
		return
	}
	if n.cursor < n.offset {
		n.offset = n.cursor
	}
	if n.cursor >= n.offset+height {
		n.offset = n.cursor - height + 1
	}
	if n.offset < 0 {
		n.offset = 0
	}
}

// Actions exposes opening the selected note in the editor.
func (n *Notes) Actions() []widget.Action {
	if len(n.notes) == 0 {
		return nil
	}
	return []widget.Action{{
		Name: "edit",
		Run:  n.edit,
	}}
}

// edit hands the terminal to the editor and takes it back on exit.
func (n *Notes) edit() tea.Cmd {
	if n.cursor >= len(n.notes) {
		return nil
	}
	path := n.notes[n.cursor].path
	name := n.name

	// The editor command may carry flags, e.g. `code -w`.
	fields := strings.Fields(n.editor)
	if len(fields) == 0 {
		fields = []string{"vi"}
	}
	args := append(fields[1:], path)

	c := exec.Command(fields[0], args...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editedMsg{name: name, err: err}
	})
}

// scan reads the notes directory off the UI goroutine.
func (n *Notes) scan() tea.Cmd {
	cfg, name := n.cfg, n.name
	return func() tea.Msg {
		notes, err := readNotes(cfg)
		return loadedMsg{name: name, notes: notes, err: err}
	}
}

func readNotes(cfg config) ([]note, error) {
	root := cfg.Path
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s does not exist", root)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	allowed := map[string]bool{}
	for _, e := range cfg.Extensions {
		allowed[strings.ToLower(e)] = true
	}

	var out []note
	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting
		}
		if d.IsDir() {
			if path != root && (!cfg.Recursive || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if len(allowed) > 0 && !allowed[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = d.Name()
		}
		out = append(out, note{path: path, display: rel, mod: fi.ModTime(), size: fi.Size()})
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	if len(out) > cfg.Limit {
		out = out[:cfg.Limit]
	}
	return out, nil
}

// maxPreviewBytes caps how much of a note is read. Previews are a glance, and
// an accidentally huge file should not stall the dashboard.
const maxPreviewBytes = 64 << 10

// maxPreviewLines caps retained lines so a minified one-line file cannot blow
// up memory for a pane that shows a dozen rows.
const maxPreviewLines = 500

// readPreview reads the head of a note for the preview pane.
func readPreview(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, maxPreviewBytes)
	nRead, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	content := buf[:nRead]

	// A NUL byte means this is not text; say so rather than spraying the
	// terminal with control characters.
	if bytes.IndexByte(content, 0) >= 0 {
		return nil, fmt.Errorf("binary file")
	}

	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	if len(lines) > maxPreviewLines {
		lines = lines[:maxPreviewLines]
	}
	return lines, nil
}

// split divides the widget between the list and the preview pane, returning
// the rows each gets. A preview height of 0 means no preview is drawn.
func (n *Notes) split() (listH, previewH int) {
	// Below this there is no room for a list, a rule and a useful preview.
	const minForPreview = 8

	if !n.cfg.Preview || n.H < minForPreview {
		return n.H, 0
	}

	previewH = n.cfg.PreviewLines
	if previewH <= 0 {
		previewH = n.H / 2
	}

	// The list keeps at least three rows; the preview gets what is left.
	const minList = 3
	if n.H-previewH-1 < minList {
		previewH = n.H - minList - 1
	}
	if previewH < 1 {
		return n.H, 0
	}
	return n.H - previewH - 1, previewH
}

// View renders the note list, and below it a preview of the selection.
func (n *Notes) View() string {
	switch {
	case n.err != nil:
		return n.theme.BadStyle().Render("⚠ " + n.err.Error())
	case !n.loaded:
		return n.theme.DimStyle().Render("loading…")
	case len(n.notes) == 0:
		return n.theme.DimStyle().Render("no notes in " + n.cfg.Path)
	}

	listH, previewH := n.split()

	// A short list would otherwise leave dead space above the rule; give
	// those rows to the preview instead.
	if previewH > 0 && len(n.notes) < listH {
		previewH += listH - len(n.notes)
		listH = len(n.notes)
	}

	out := n.listView(listH)
	if previewH > 0 {
		out += "\n" + n.rule() + "\n" + n.previewView(previewH)
	}
	return out
}

// listView renders the file list, scrolled to keep the cursor visible.
func (n *Notes) listView(height int) string {
	if height <= 0 {
		return ""
	}
	n.clampScrollTo(height)
	end := min(n.offset+height, len(n.notes))

	lines := make([]string, 0, height)
	for i := n.offset; i < end; i++ {
		lines = append(lines, n.line(n.notes[i], i == n.cursor))
	}
	// Pad so the rule stays put as the list shrinks.
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// rule separates the list from the preview.
func (n *Notes) rule() string {
	if n.W <= 0 {
		return ""
	}
	return n.theme.FaintStyle().Render(strings.Repeat("─", n.W))
}

// previewView renders the head of the selected note.
func (n *Notes) previewView(height int) string {
	var lines []string

	switch {
	case n.previewErr != nil:
		lines = []string{n.theme.BadStyle().Render("⚠ " + n.previewErr.Error())}
	case n.previewPath != n.selectedPath():
		lines = []string{n.theme.FaintStyle().Render("…")}
	default:
		lines = n.renderPreviewLines(height)
	}

	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:height], "\n")
}

// renderPreviewLines applies light markdown styling, enough to make structure
// visible without pulling in a renderer.
func (n *Notes) renderPreviewLines(height int) []string {
	out := make([]string, 0, height)
	inCodeFence := false

	for _, raw := range n.previewLines {
		if len(out) == height {
			break
		}
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)

		// Skip leading blank lines so the pane starts with content.
		if len(out) == 0 && trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "```") {
			inCodeFence = !inCodeFence
			out = append(out, n.theme.FaintStyle().Render(humanize.Truncate(line, n.W)))
			continue
		}

		text := humanize.Truncate(line, n.W)
		switch {
		case inCodeFence:
			out = append(out, n.theme.DimStyle().Render(text))
		case strings.HasPrefix(trimmed, "#"):
			out = append(out, n.theme.AccentStyle().Bold(true).Render(text))
		case strings.HasPrefix(trimmed, "> "):
			out = append(out, n.theme.DimStyle().Italic(true).Render(text))
		case isBullet(trimmed):
			out = append(out, n.bulletLine(line))
		default:
			out = append(out, n.theme.TextStyle().Render(text))
		}
	}
	return out
}

func isBullet(trimmed string) bool {
	for _, p := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

// bulletLine recolours the list marker so structure reads at a glance, keeping
// the original indentation.
func (n *Notes) bulletLine(line string) string {
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	rest := strings.TrimLeft(line, " \t")
	body := humanize.Truncate(indent+rest[2:], max(0, n.W-2))
	return n.theme.AccentStyle().Render(indent+"•") + " " + n.theme.TextStyle().Render(body)
}

// line renders one list row: marker, filename, then a right-aligned age.
func (n *Notes) line(nt note, selected bool) string {
	age := humanize.RelTime(nt.mod)

	marker := "  "
	nameStyle := n.theme.TextStyle()
	if selected {
		marker = "▸ "
		nameStyle = n.theme.AccentStyle().Bold(true)
		if !n.Focused() {
			nameStyle = n.theme.TextStyle().Bold(true)
		}
	}

	// Reserve room for the marker, a gap, and the age column. Widths are
	// measured in display cells: "▸ " is two cells but four bytes.
	nameWidth := n.W - lipgloss.Width(marker) - lipgloss.Width(age) - 1
	if nameWidth < 1 {
		return nameStyle.Render(humanize.Truncate(marker+nt.display, n.W))
	}

	name := humanize.Truncate(nt.display, nameWidth)
	pad := strings.Repeat(" ", max(0, nameWidth-lipgloss.Width(name)))

	return n.theme.FaintStyle().Render(marker) +
		nameStyle.Render(name) + pad + " " +
		n.theme.FaintStyle().Render(age)
}
