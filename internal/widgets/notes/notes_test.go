package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	"gopkg.in/yaml.v3"
)

// seed builds a notes directory with staggered modification times.
func seed(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := []struct {
		rel string
		age time.Duration
	}{
		{"newest.md", 0},
		{"middle.md", time.Hour},
		{"oldest.md", 48 * time.Hour},
		{"notes.txt", 2 * time.Hour},
		{"ignore.pdf", time.Minute},
		{".hidden.md", time.Minute},
		{"sub/nested.md", 30 * time.Minute},
		{".git/config.md", time.Minute},
	}

	for _, f := range files {
		path := filepath.Join(root, f.rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mod := time.Now().Add(-f.age)
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func names(notes []note) []string {
	out := make([]string, len(notes))
	for i, n := range notes {
		out[i] = n.display
	}
	return out
}

func TestReadNotesSortsNewestFirst(t *testing.T) {
	root := seed(t)

	got, err := readNotes(config{Path: root, Extensions: []string{".md"}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"newest.md", "middle.md", "oldest.md"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", names(got), want)
	}
	for i := range want {
		if got[i].display != want[i] {
			t.Errorf("position %d = %q, want %q", i, got[i].display, want[i])
		}
	}
}

func TestReadNotesFiltersExtensions(t *testing.T) {
	root := seed(t)

	got, err := readNotes(config{Path: root, Extensions: []string{".md", ".txt"}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		if ext := filepath.Ext(n.display); ext != ".md" && ext != ".txt" {
			t.Errorf("unexpected file %q", n.display)
		}
	}
	if len(got) != 4 {
		t.Errorf("got %v, want the 3 .md files plus notes.txt", names(got))
	}
}

func TestReadNotesRecursion(t *testing.T) {
	root := seed(t)

	flat, err := readNotes(config{Path: root, Extensions: []string{".md"}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range flat {
		if filepath.Dir(n.display) != "." {
			t.Errorf("non-recursive scan descended into %q", n.display)
		}
	}

	deep, err := readNotes(config{Path: root, Recursive: true, Extensions: []string{".md"}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var foundNested bool
	for _, n := range deep {
		if n.display == filepath.Join("sub", "nested.md") {
			foundNested = true
		}
		// Dot-directories stay skipped even when recursing.
		if filepath.Dir(n.display) == ".git" {
			t.Errorf("recursive scan descended into a dot-directory: %q", n.display)
		}
	}
	if !foundNested {
		t.Errorf("recursive scan missed sub/nested.md, got %v", names(deep))
	}
}

func TestReadNotesLimit(t *testing.T) {
	root := seed(t)
	got, err := readNotes(config{Path: root, Extensions: []string{".md"}, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d notes, want the limit of 2", len(got))
	}
}

func TestReadNotesMissingDirectory(t *testing.T) {
	_, err := readNotes(config{Path: filepath.Join(t.TempDir(), "nope"), Limit: 10})
	if err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestReadNotesRejectsAFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readNotes(config{Path: f, Limit: 10}); err == nil {
		t.Fatal("expected an error when path is a file, not a directory")
	}
}

func newNotes(t *testing.T, yamlSrc string) (widget.Widget, error) {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(yamlSrc), &node); err != nil {
		t.Fatal(err)
	}
	return New(widget.Context{
		Name:  "notes",
		Node:  node.Content[0],
		Theme: theme.New(""),
	})
}

func TestNewRequiresPath(t *testing.T) {
	if _, err := newNotes(t, "type: notes\npath: \"\"\n"); err == nil {
		t.Error("expected an error when path is empty")
	}
}

// TestNoActionsWhenEmpty checks the footer will not advertise "edit" with
// nothing to edit.
func TestNoActionsWhenEmpty(t *testing.T) {
	w, err := newNotes(t, "type: notes\npath: /tmp\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Actions(); len(got) != 0 {
		t.Errorf("got %d actions with no notes loaded, want 0", len(got))
	}
}

// TestCursorSurvivesShrink guards against an out-of-range selection when a
// rescan returns fewer notes than before.
func TestCursorSurvivesShrink(t *testing.T) {
	w, err := newNotes(t, "type: notes\npath: /tmp\n")
	if err != nil {
		t.Fatal(err)
	}
	n := w.(*Notes)
	n.SetSize(40, 10)

	n.Update(loadedMsg{notes: []note{
		{display: "a.md"}, {display: "b.md"}, {display: "c.md"},
	}})
	n.list.Select(2)

	n.Update(loadedMsg{notes: []note{{display: "a.md"}}})
	if n.list.Cursor() != 0 {
		t.Errorf("cursor = %d after the list shrank to 1 note, want 0", n.list.Cursor())
	}
}

// TestSplitReservesRoom checks the list/preview divide keeps both panes usable
// and never exceeds the widget.
func TestSplitReservesRoom(t *testing.T) {
	tests := []struct {
		name              string
		h, previewLines   int
		preview           bool
		wantList, wantPrv int
	}{
		{"preview off", 20, 0, false, 20, 0},
		{"too short for a preview", 7, 0, true, 7, 0},
		{"even split", 20, 0, true, 9, 10},
		{"fixed preview height", 20, 4, true, 15, 4},
		{"preview too tall is clamped", 20, 50, true, 3, 16},
		{"exactly at the threshold", 8, 0, true, 3, 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := &Notes{cfg: config{Preview: tc.preview, PreviewLines: tc.previewLines}}
			n.SetSize(40, tc.h)

			listH, previewH := n.split()
			if listH != tc.wantList || previewH != tc.wantPrv {
				t.Errorf("split() = %d,%d; want %d,%d", listH, previewH, tc.wantList, tc.wantPrv)
			}
			if previewH > 0 && listH+previewH+1 != tc.h {
				t.Errorf("panes plus rule = %d, want %d", listH+previewH+1, tc.h)
			}
		})
	}
}

// TestViewFitsItsBox is the size contract: whatever the note contents, the
// widget must render exactly the rows it was given.
func TestViewFitsItsBox(t *testing.T) {
	root := seed(t)

	for _, h := range []int{4, 8, 12, 20, 30} {
		w, err := newNotes(t, "type: notes\npath: "+root+"\n")
		if err != nil {
			t.Fatal(err)
		}
		n := w.(*Notes)
		n.SetSize(40, h)

		notes, err := readNotes(n.cfg)
		if err != nil {
			t.Fatal(err)
		}
		n.Update(loadedMsg{notes: notes})
		n.Update(previewMsg{
			path:  n.selectedPath(),
			lines: strings.Split(strings.Repeat("a line\n", 40), "\n"),
		})

		got := strings.Count(n.View(), "\n") + 1
		if got != h {
			t.Errorf("height %d: rendered %d lines", h, got)
		}
	}
}

// TestPreviewIgnoresStaleReads checks a slow read landing after the cursor has
// moved does not overwrite the current selection's preview.
func TestPreviewIgnoresStaleReads(t *testing.T) {
	w, err := newNotes(t, "type: notes\npath: /tmp\n")
	if err != nil {
		t.Fatal(err)
	}
	n := w.(*Notes)
	n.SetSize(40, 20)

	n.Update(loadedMsg{notes: []note{
		{path: "/a.md", display: "a.md"}, {path: "/b.md", display: "b.md"},
	}})

	n.Update(previewMsg{path: "/b.md", lines: []string{"stale"}})
	if n.previewPath == "/b.md" {
		t.Error("accepted a preview for a note that is not selected")
	}

	n.Update(previewMsg{path: "/a.md", lines: []string{"fresh"}})
	if n.previewPath != "/a.md" {
		t.Error("rejected the preview for the selected note")
	}
}

func TestReadPreviewRejectsBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blob.md")
	if err := os.WriteFile(path, []byte{0x1b, 0x00, 0x5b, 0x41}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPreview(path); err == nil {
		t.Error("expected binary content to be refused rather than printed")
	}
}

func TestReadPreviewNormalisesLineEndings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crlf.md")
	if err := os.WriteFile(path, []byte("# Title\r\n\r\n- one\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readPreview(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range lines {
		if strings.Contains(l, "\r") {
			t.Errorf("line %d still has a carriage return: %q", i, l)
		}
	}
}
