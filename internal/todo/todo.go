// Package todo reads and writes a markdown checklist: the "- [ ] buy milk"
// lines a person already writes in their notes.
//
// The file is the store. There is no database and no state directory, because
// a task list you can only reach through one program is a worse task list —
// this one greps, syncs, diffs, and opens in any editor. Everything here is a
// pure function over the file's text except Load and Save, which is what makes
// the round trip testable.
package todo

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Task is one checkbox line.
type Task struct {
	// Raw is the line exactly as it appeared. It is the task's identity
	// when the file has been edited underneath us: see File.Locate.
	Raw string

	// Line is the task's index into File.Lines.
	Line int

	// Indent and Marker are kept so a rewrite gives the line back in the
	// shape the user wrote it, nested bullets and "*" lists included.
	Indent string
	Marker string

	Done bool

	// Text is the task itself, with the due token taken out.
	Text string

	// Due is local midnight on the day it is due, or the zero time.
	Due time.Time
}

// HasDue reports whether the task carries a date.
func (t Task) HasDue() bool { return !t.Due.IsZero() }

// File is a checklist file: every line, and the tasks among them.
//
// The lines that are not tasks — headings, prose, blanks — are carried through
// untouched, so ctOS can share a file with whatever else the user keeps in it.
type File struct {
	Lines []string
	Tasks []Task
}

// dueToken is what a date is written as. Obsidian's Tasks plugin writes the
// calendar emoji instead, so that form is read as well and normalised on the
// next write.
const dueToken = "due:"

const dueEmoji = "📅"

// Parse reads a checklist out of a file's contents.
func Parse(data []byte) File {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	f := File{Lines: strings.Split(text, "\n")}
	// A trailing newline splits into a final empty element; dropping it
	// here and adding it back in Bytes keeps the file's shape stable.
	if n := len(f.Lines); n > 0 && f.Lines[n-1] == "" {
		f.Lines = f.Lines[:n-1]
	}
	f.reindex()
	return f
}

// reindex re-reads the tasks out of Lines, after anything that inserts or
// removes one.
func (f *File) reindex() {
	f.Tasks = nil
	for i, line := range f.Lines {
		if t, ok := parseTask(line, i); ok {
			f.Tasks = append(f.Tasks, t)
		}
	}
}

// parseTask reads one line as "<indent><marker> [<state>] <text>".
func parseTask(line string, i int) (Task, bool) {
	rest := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(rest)]

	if len(rest) < 2 || !strings.ContainsRune("-*+", rune(rest[0])) || rest[1] != ' ' {
		return Task{}, false
	}
	marker := rest[:1]
	rest = strings.TrimLeft(rest[2:], " ")

	if len(rest) < 3 || rest[0] != '[' || rest[2] != ']' {
		return Task{}, false
	}
	var done bool
	switch rest[1] {
	case ' ':
	case 'x', 'X':
		done = true
	default:
		// Some checklists use other letters for other states. They are
		// not this widget's to interpret, so the line stays prose.
		return Task{}, false
	}

	text := strings.TrimSpace(rest[3:])
	text, due := SplitDue(text)

	return Task{
		Raw:    line,
		Line:   i,
		Indent: indent,
		Marker: marker,
		Done:   done,
		Text:   text,
		Due:    due,
	}, true
}

// SplitDue takes a due token out of a line of text, wherever it sits, and
// returns the text without it. Only an ISO date is recognised here: the
// shorthands are for what a person types, and ParseDue resolves those before
// anything reaches the file.
func SplitDue(text string) (string, time.Time) {
	fields := strings.Fields(text)
	kept := make([]string, 0, len(fields))
	var due time.Time

	for i := 0; i < len(fields); i++ {
		word := fields[i]

		// "📅 2026-09-01", the emoji and the date as separate words.
		if word == dueEmoji && i+1 < len(fields) {
			if d, err := time.ParseInLocation(DateLayout, fields[i+1], time.Local); err == nil {
				due, i = d, i+1
				continue
			}
		}
		value, ok := strings.CutPrefix(word, dueToken)
		if !ok {
			value, ok = strings.CutPrefix(word, dueEmoji)
		}
		if ok {
			if d, err := time.ParseInLocation(DateLayout, value, time.Local); err == nil {
				due = d
				continue
			}
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " "), due
}

// render writes the task back out as a markdown line.
func (t Task) render() string {
	box := "[ ]"
	if t.Done {
		box = "[x]"
	}
	line := t.Indent + t.Marker + " " + box + " " + t.Text
	if t.HasDue() {
		line += " " + dueToken + t.Due.Format(DateLayout)
	}
	return line
}

// Bytes serialises the file, ending in a newline the way a text file should.
func (f File) Bytes() []byte {
	var b bytes.Buffer
	for _, line := range f.Lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// Locate finds the task that came from line, falling back to the first task
// whose text matches raw.
//
// The fallback is the point. A key is pressed against the list as it was last
// read, but the write re-reads the file first, and between the two the user may
// have edited it in another window. Matching on the line's own text means the
// toggle still lands on the task the user was looking at, rather than on
// whatever has since moved into that position.
func (f File) Locate(line int, raw string) (int, bool) {
	for i, t := range f.Tasks {
		if t.Line == line && t.Raw == raw {
			return i, true
		}
	}
	for i, t := range f.Tasks {
		if t.Raw == raw {
			return i, true
		}
	}
	return 0, false
}

// set writes task i back to its line.
func (f *File) set(i int, t Task) {
	t.Raw = t.render()
	f.Lines[t.Line] = t.Raw
	f.Tasks[i] = t
}

// Toggle flips task i between done and not done.
func (f *File) Toggle(i int) {
	t := f.Tasks[i]
	t.Done = !t.Done
	f.set(i, t)
}

// SetText replaces task i's text, reading a due token out of it if one was
// typed, so editing a task is also how its date is changed.
func (f *File) SetText(i int, text string) {
	t := f.Tasks[i]
	clean, due := SplitDue(strings.TrimSpace(text))
	t.Text = clean
	if !due.IsZero() {
		t.Due = due
	}
	f.set(i, t)
}

// SetDue puts task i's due date, or clears it with the zero time.
func (f *File) SetDue(i int, due time.Time) {
	t := f.Tasks[i]
	t.Due = due
	f.set(i, t)
}

// Add appends a task and returns its index. The text may carry a "due:"
// token in any of ParseDue's forms, resolved against now.
func (f *File) Add(text string, now time.Time) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("a task needs some text")
	}
	text, due := resolveDue(text, now)
	if text == "" {
		return 0, fmt.Errorf("a task needs more than a date")
	}

	t := Task{Marker: "-", Text: text, Due: due}

	// New tasks go with the existing ones rather than at the end of the
	// file, so a checklist under a heading stays under it and any closing
	// prose stays closing.
	at := len(f.Lines)
	if n := len(f.Tasks); n > 0 {
		at = f.Tasks[n-1].Line + 1
		t.Indent = f.Tasks[n-1].Indent
		t.Marker = f.Tasks[n-1].Marker
	}

	t.Raw = t.render()
	f.Lines = append(f.Lines, "")
	copy(f.Lines[at+1:], f.Lines[at:])
	f.Lines[at] = t.Raw
	f.reindex()

	for i, existing := range f.Tasks {
		if existing.Line == at {
			return i, nil
		}
	}
	return 0, fmt.Errorf("could not add the task")
}

// resolveDue pulls a "due:<shorthand>" word out of typed text and resolves it.
func resolveDue(text string, now time.Time) (string, time.Time) {
	fields := strings.Fields(text)
	kept := make([]string, 0, len(fields))
	var due time.Time

	for _, word := range fields {
		value, ok := strings.CutPrefix(strings.ToLower(word), dueToken)
		if !ok {
			kept = append(kept, word)
			continue
		}
		d, valid := ParseDue(value, now)
		if !valid {
			// Not a date we understand; leave the word in the text
			// rather than silently eating it.
			kept = append(kept, word)
			continue
		}
		due = d
	}
	return strings.Join(kept, " "), due
}

// Delete removes task i's line.
func (f *File) Delete(i int) {
	at := f.Tasks[i].Line
	f.Lines = append(f.Lines[:at], f.Lines[at+1:]...)
	f.reindex()
}

// ClearDone removes every completed task and reports how many went.
func (f *File) ClearDone() int {
	kept := make([]string, 0, len(f.Lines))
	drop := make(map[int]bool, len(f.Tasks))
	for _, t := range f.Tasks {
		if t.Done {
			drop[t.Line] = true
		}
	}
	for i, line := range f.Lines {
		if !drop[i] {
			kept = append(kept, line)
		}
	}
	f.Lines = kept
	f.reindex()
	return len(drop)
}

// Load reads a checklist. A file that is not there yet is an empty list, not
// an error: the first task the user adds creates it.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return File{}, fmt.Errorf("%s is not a text file", path)
	}
	return Parse(data), nil
}

// Save writes the checklist back, creating the file and its directory if this
// is the first task.
//
// The write goes via a temporary file and a rename, so an interruption cannot
// leave a half-written task list behind.
func Save(path string, f File) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp file next to %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // a no-op once the rename lands

	if _, err := tmp.Write(f.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Bucket is the group a task falls into: the answer to "when".
type Bucket int

// The buckets, in the order a list shows them. Overdue leads because it is the
// only one that is already a problem.
const (
	Overdue Bucket = iota
	Today
	Upcoming
	Someday
	Done
)

// String names the bucket, as the list's group headings show it.
func (b Bucket) String() string {
	switch b {
	case Overdue:
		return "overdue"
	case Today:
		return "today"
	case Upcoming:
		return "upcoming"
	case Someday:
		return "someday"
	default:
		return "done"
	}
}

// BucketOf places a task relative to now.
func BucketOf(t Task, now time.Time) Bucket {
	if t.Done {
		return Done
	}
	if !t.HasDue() {
		return Someday
	}
	today := Day(now)
	switch {
	case t.Due.Before(today):
		return Overdue
	case t.Due.After(today):
		return Upcoming
	default:
		return Today
	}
}

// Sort orders tasks by bucket, then by date, keeping file order within a day.
// It sorts in place and is stable, so two tasks due the same day stay in the
// order they were written.
func Sort(tasks []Task, now time.Time) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		ba, bb := BucketOf(a, now), BucketOf(b, now)
		if ba != bb {
			return ba < bb
		}
		if a.HasDue() && b.HasDue() && !a.Due.Equal(b.Due) {
			return a.Due.Before(b.Due)
		}
		return a.Line < b.Line
	})
}

// Show is which tasks the widget is displaying.
type Show string

// The show modes. ShowToday is the "what am I doing today" view: what is due
// now or was due already, and nothing else.
const (
	ShowAll   Show = "all"
	ShowOpen  Show = "open"
	ShowToday Show = "today"
)

// AllShows is every mode, in the order the "f" key cycles them.
var AllShows = []Show{ShowAll, ShowOpen, ShowToday}

// ParseShow reads a "show:" value.
func ParseShow(s string) (Show, error) {
	if s == "" {
		return ShowAll, nil
	}
	got := Show(strings.ToLower(strings.TrimSpace(s)))
	for _, s := range AllShows {
		if got == s {
			return got, nil
		}
	}
	return "", fmt.Errorf("unknown show %q (valid: all, open, today)", s)
}

// Filter returns the tasks a show mode displays.
func Filter(tasks []Task, show Show, now time.Time) []Task {
	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		switch show {
		case ShowOpen:
			if t.Done {
				continue
			}
		case ShowToday:
			if b := BucketOf(t, now); b != Overdue && b != Today {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// Counts is the headline: how much is outstanding, and how much of it is
// already late.
type Counts struct {
	Open    int
	Done    int
	Today   int
	Overdue int
}

// Count summarises a task list.
func Count(tasks []Task, now time.Time) Counts {
	var c Counts
	for _, t := range tasks {
		switch BucketOf(t, now) {
		case Done:
			c.Done++
		case Overdue:
			c.Overdue++
			c.Open++
		case Today:
			c.Today++
			c.Open++
		default:
			c.Open++
		}
	}
	return c
}
