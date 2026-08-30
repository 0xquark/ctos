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
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func init() {
	widget.Register(widget.Spec{
		Name:    "notes",
		Summary: "markdown files in a directory, newest first, opened in your editor",
		New:     New,
		Example: `type: notes
path: ~/notes             # required
recursive: false
extensions: [".md", ".txt"]
limit: 200
preview: true             # show the selected note below the list
preview_lines: 0          # 0 splits the widget in half
title: notes`,
	})
}

type config struct {
	Path       string   `yaml:"path"`
	Recursive  bool     `yaml:"recursive"`
	Extensions []string `yaml:"extensions"`
	Limit      int      `yaml:"limit"`

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
	notes []note
	err   error
}

// editedMsg reports that the editor exited, so the list can be rescanned.
type editedMsg struct {
	err error
}

// previewMsg carries a note's contents back for the preview pane.
type previewMsg struct {
	path  string
	lines []string
	err   error
}

// Notes lists note files, newest first.
type Notes struct {
	widget.Base
	cfg    config
	editor string

	notes  []note
	list   widget.List
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
		Preview:    true,
	}
	if err := ctx.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Path == "" {
		return nil, errors.New("\"path:\" is required")
	}
	if cfg.Limit <= 0 {
		cfg.Limit = 200
	}
	return &Notes{cfg: cfg, editor: ctx.Editor}, nil
}

// Init kicks off the first directory scan.
func (n *Notes) Init() tea.Cmd { return n.scan() }

// Update handles navigation keys and scan results.
func (n *Notes) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case loadedMsg:
		n.loaded = true
		n.notes, n.err = msg.notes, msg.err
		n.list.SetLen(len(n.notes))
		return n.syncPreview()

	case previewMsg:
		// A slower read may land after the cursor has moved on.
		if msg.path == n.selectedPath() {
			n.previewPath = msg.path
			n.previewLines, n.previewErr = msg.lines, msg.err
		}
		return nil

	case editedMsg:
		// The file may have been renamed, or its contents changed.
		n.previewPath = ""
		return n.scan()

	case tea.KeyMsg:
		listH, _ := n.split()
		if n.list.HandleKey(msg, listH) {
			return n.syncPreview()
		}
		if msg.String() == "r" {
			n.previewPath = ""
			return n.scan()
		}
	}
	return nil
}

// selectedPath is the path under the cursor, or "" when the list is empty.
func (n *Notes) selectedPath() string {
	if n.list.Empty() {
		return ""
	}
	return n.notes[n.list.Cursor()].path
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
	return n.Cmd(func() tea.Msg {
		lines, err := readPreview(path)
		return previewMsg{path: path, lines: lines, err: err}
	})
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
	if n.list.Empty() {
		return nil
	}
	path := n.notes[n.list.Cursor()].path

	// The editor command may carry flags, e.g. `code -w`.
	fields := strings.Fields(n.editor)
	if len(fields) == 0 {
		fields = []string{"vi"}
	}
	args := append(fields[1:], path)

	c := exec.Command(fields[0], args...)
	// ExecProcess's callback is bubbletea's, not ours, so the result is
	// addressed by hand rather than through Cmd.
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return n.Address(editedMsg{err: err})
	})
}

// scan reads the notes directory off the UI goroutine.
func (n *Notes) scan() tea.Cmd {
	cfg := n.cfg
	return n.Cmd(func() tea.Msg {
		notes, err := readNotes(cfg)
		return loadedMsg{notes: notes, err: err}
	})
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
		return n.Theme().BadStyle().Render("⚠ " + n.err.Error())
	case !n.loaded:
		return n.Theme().DimStyle().Render("loading…")
	case len(n.notes) == 0:
		return n.Theme().DimStyle().Render("no notes in " + n.cfg.Path)
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
	start, end := n.list.Window(height)

	lines := make([]string, 0, height)
	for i := start; i < end; i++ {
		lines = append(lines, n.line(n.notes[i], i == n.list.Cursor()))
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
	return n.Theme().FaintStyle().Render(strings.Repeat("─", n.W))
}

// previewView renders the head of the selected note.
func (n *Notes) previewView(height int) string {
	var lines []string

	switch {
	case n.previewErr != nil:
		lines = []string{n.Theme().BadStyle().Render("⚠ " + n.previewErr.Error())}
	case n.previewPath != n.selectedPath():
		lines = []string{n.Theme().FaintStyle().Render("…")}
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
			out = append(out, n.Theme().FaintStyle().Render(humanize.Truncate(line, n.W)))
			continue
		}

		text := humanize.Truncate(line, n.W)
		switch {
		case inCodeFence:
			out = append(out, n.Theme().DimStyle().Render(text))
		case strings.HasPrefix(trimmed, "#"):
			out = append(out, n.Theme().AccentStyle().Bold(true).Render(text))
		case strings.HasPrefix(trimmed, "> "):
			out = append(out, n.Theme().DimStyle().Italic(true).Render(text))
		case isBullet(trimmed):
			out = append(out, n.bulletLine(line))
		default:
			out = append(out, n.Theme().TextStyle().Render(text))
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
	return n.Theme().AccentStyle().Render(indent+"•") + " " + n.Theme().TextStyle().Render(body)
}

// line renders one list row: marker, filename, then a right-aligned age.
func (n *Notes) line(nt note, selected bool) string {
	age := humanize.RelTime(nt.mod)

	marker := "  "
	nameStyle := n.Theme().TextStyle()
	if selected {
		marker = "▸ "
		nameStyle = n.Theme().AccentStyle().Bold(true)
		if !n.Focused() {
			nameStyle = n.Theme().TextStyle().Bold(true)
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

	return n.Theme().FaintStyle().Render(marker) +
		nameStyle.Render(name) + pad + " " +
		n.Theme().FaintStyle().Render(age)
}
