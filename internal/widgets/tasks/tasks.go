// Package tasks is a checklist you can work from the dashboard: add a task,
// give it a day, tick it off.
//
// The store is a markdown file of "- [ ]" lines (internal/todo), not a
// database, so the same list opens in any editor and syncs with whatever
// already syncs the user's notes.
package tasks

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/0xquark/ctos/internal/todo"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
)

func init() {
	widget.Register(widget.Spec{
		Name:    "tasks",
		Summary: "a markdown checklist you can add to, date and tick off",
		New:     New,
		Example: `type: tasks
path: ~/notes/tasks.md    # the markdown file the tasks live in
show: all                 # all, open, or today
group: true               # section headings by when things are due
refresh: 60s              # re-read the file, so edits elsewhere show up
limit: 200
title: tasks`,
	})
}

// defaultPath puts the list with the user's notes, so the notes widget lists
// it and whatever syncs that directory syncs this too.
const defaultPath = "~/notes/tasks.md"

// defaultRefresh only exists to notice edits made somewhere else — nothing
// changes a checklist on its own — so it is slow on purpose.
const defaultRefresh = 60 * time.Second

// minRefresh keeps a dashboard from re-reading the file in a tight loop.
const minRefresh = 5 * time.Second

type config struct {
	Path    string `yaml:"path"`
	Show    string `yaml:"show"`
	Group   bool   `yaml:"group"`
	Refresh string `yaml:"refresh"`
	Limit   int    `yaml:"limit"`
}

// loadedMsg carries a read of the file back to the widget that asked for it.
type loadedMsg struct {
	file todo.File
	err  error
}

// doneMsg is the result of one write.
type doneMsg struct {
	text string
	file todo.File
	err  error
}

// editedMsg reports that the editor exited, so the file can be re-read.
type editedMsg struct{ err error }

type tickMsg struct{}

// armed is a destructive key waiting for its second press. Both keys here
// throw work away, so neither happens on one keystroke.
type armed int

const (
	armedNone armed = iota
	armedDelete
	armedClear
)

// Tasks is a checklist widget over one markdown file.
type Tasks struct {
	widget.Base
	editor string

	path    string
	show    todo.Show
	group   bool
	refresh time.Duration
	limit   int

	file   todo.File
	rows   []todo.Task // after filtering and sorting: what is on screen
	list   widget.List
	err    error
	loaded bool

	// typing is the input box holding the keyboard. It is editing an
	// existing task when editing is set, and adding a new one otherwise.
	typing  bool
	editing bool
	input   string

	// target is the task a pending operation applies to, captured when the
	// key was pressed. It is a value, not an index: the file is re-read
	// before every write, and an index would point at a different task.
	target todo.Task

	arm    armed
	status string
	busy   bool

	// selText is the task the cursor was on, so a refresh that re-sorts the
	// list underneath the user can put it back.
	selText string
}

// New builds a tasks widget from its dashboard configuration.
func New(ctx widget.Context) (widget.Widget, error) {
	cfg := config{Path: defaultPath, Group: true, Limit: 200}
	if err := ctx.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Path == "" {
		return nil, errors.New("\"path:\" is required")
	}
	if cfg.Limit <= 0 {
		cfg.Limit = 200
	}

	show, err := todo.ParseShow(cfg.Show)
	if err != nil {
		return nil, err
	}
	refresh, err := ctx.Refresh(cfg.Refresh, defaultRefresh, minRefresh)
	if err != nil {
		return nil, err
	}

	return &Tasks{
		editor:  ctx.Editor,
		path:    cfg.Path,
		show:    show,
		group:   cfg.Group,
		refresh: refresh,
		limit:   cfg.Limit,
	}, nil
}

// Init reads the file and starts the refresh tick.
func (t *Tasks) Init() tea.Cmd {
	return tea.Batch(t.read(), t.Every(t.refresh, tickMsg{}))
}

// Update handles keys, reads and the results of writes.
func (t *Tasks) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case loadedMsg:
		t.loaded = true
		t.err = msg.err
		if msg.err == nil {
			t.file = msg.file
			t.rebuild()
		}
		return nil

	case doneMsg:
		t.busy = false
		if msg.err != nil {
			t.status = msg.err.Error()
			return nil
		}
		t.status = msg.text
		t.file = msg.file
		t.rebuild()
		return nil

	case editedMsg:
		return t.read()

	case tickMsg:
		next := t.Every(t.refresh, tickMsg{})
		// A read while a write is in flight would show the file as it was
		// before the write and then be overwritten by it anyway.
		if t.busy {
			return next
		}
		return tea.Batch(t.read(), next)

	case tea.KeyMsg:
		switch {
		case t.typing:
			return t.inputKey(msg)
		case t.arm != armedNone:
			return t.armedKey(msg)
		default:
			return t.key(msg)
		}
	}
	return nil
}

// GrabsKeys claims the keyboard while a task is being typed.
func (t *Tasks) GrabsKeys() bool { return t.typing }

// key handles normal navigation.
func (t *Tasks) key(msg tea.KeyMsg) tea.Cmd {
	if t.list.HandleKey(msg, t.listHeight()) {
		t.status = ""
		t.remember()
		return nil
	}

	switch msg.String() {
	case " ":
		return t.toggle()

	case "a", "n":
		t.startAdd()

	case "e":
		t.startEdit()

	case "t":
		return t.dueToday()

	case "d":
		if task, ok := t.selected(); ok {
			t.arm, t.target, t.status = armedDelete, task, ""
		}

	case "x":
		if t.counts().Done > 0 {
			t.arm, t.status = armedClear, ""
		}

	case "f":
		t.cycleShow()

	case "o":
		return t.open()

	case "r":
		t.status = ""
		return t.read()
	}
	return nil
}

// armedKey handles the second keystroke of a destructive operation. While one
// is armed the list freezes: every key either confirms or cancels, so nothing
// else is one press away from a task the user did not mean to lose.
func (t *Tasks) armedKey(msg tea.KeyMsg) tea.Cmd {
	kind := t.arm
	switch msg.String() {
	case "esc", "n":
		t.arm = armedNone
		return nil
	case "d":
		if kind == armedDelete {
			return t.confirm()
		}
	case "x":
		if kind == armedClear {
			return t.confirm()
		}
	}
	return nil
}

// confirm carries out the armed operation. It is also what enter runs, through
// Actions, so the armed prompt answers to the same key everything else does.
func (t *Tasks) confirm() tea.Cmd {
	kind := t.arm
	t.arm = armedNone

	switch kind {
	case armedDelete:
		target := t.target
		return t.onTask(target, "deleted "+quote(target.Text), func(f *todo.File, i int) {
			f.Delete(i)
		})
	case armedClear:
		var n int
		return t.mutate(func() string { return fmt.Sprintf("cleared %s", plural(n, "completed task")) },
			func(f *todo.File) error {
				n = f.ClearDone()
				return nil
			})
	}
	return nil
}

// inputKey handles the add/edit box while it owns the keyboard.
func (t *Tasks) inputKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEnter:
		text := strings.TrimSpace(t.input)
		editing, target := t.editing, t.target
		t.typing, t.editing, t.input = false, false, ""
		if text == "" {
			return nil
		}
		if editing {
			t.selText = "" // the text is what the cursor follows, and it just changed
			return t.onTask(target, "edited", func(f *todo.File, i int) {
				f.SetText(i, text)
			})
		}
		now := time.Now()
		return t.mutate(func() string { return "added" }, func(f *todo.File) error {
			_, err := f.Add(text, now)
			return err
		})

	case tea.KeyEsc:
		t.typing, t.editing, t.input = false, false, ""

	case tea.KeyBackspace:
		if r := []rune(t.input); len(r) > 0 {
			t.input = string(r[:len(r)-1])
		}

	case tea.KeyRunes:
		t.input += string(msg.Runes)

	case tea.KeySpace:
		t.input += " "
	}
	return nil
}

// startAdd opens an empty input box.
func (t *Tasks) startAdd() {
	t.typing, t.editing, t.input, t.status = true, false, "", ""
}

// startEdit opens the input box on the selected task, prefilled with its text
// and its date in the form the box accepts, so a date is edited the same way
// it is set.
func (t *Tasks) startEdit() {
	task, ok := t.selected()
	if !ok {
		return
	}
	t.input = task.Text
	if task.HasDue() {
		t.input += " due:" + task.Due.Format(todo.DateLayout)
	}
	t.typing, t.editing, t.target, t.status = true, true, task, ""
}

// toggle ticks the selected task off, or puts it back.
func (t *Tasks) toggle() tea.Cmd {
	task, ok := t.selected()
	if !ok {
		return nil
	}
	done := "done"
	if task.Done {
		done = "reopened"
	}
	return t.onTask(task, done+" "+quote(task.Text), func(f *todo.File, i int) {
		f.Toggle(i)
	})
}

// dueToday puts today's date on the selected task, or takes it off if it is
// already there. It is the one key the "today" view is built out of.
func (t *Tasks) dueToday() tea.Cmd {
	task, ok := t.selected()
	if !ok {
		return nil
	}
	today := todo.Day(time.Now())
	if task.HasDue() && task.Due.Equal(today) {
		return t.onTask(task, "no date", func(f *todo.File, i int) { f.SetDue(i, time.Time{}) })
	}
	return t.onTask(task, "due today", func(f *todo.File, i int) { f.SetDue(i, today) })
}

// cycleShow moves to the next view: all, open, today.
func (t *Tasks) cycleShow() {
	for i, s := range todo.AllShows {
		if s == t.show {
			t.show = todo.AllShows[(i+1)%len(todo.AllShows)]
			break
		}
	}
	t.status = ""
	t.rebuild()
	t.list.Top()
	t.remember()
}

// Actions names what enter does. While an operation is armed it answers the
// prompt, so the confirming key is the same one everything else uses.
func (t *Tasks) Actions() []widget.Action {
	switch t.arm {
	case armedDelete:
		return []widget.Action{{Name: "delete", Run: t.confirm}}
	case armedClear:
		return []widget.Action{{Name: "clear completed", Run: t.confirm}}
	}

	task, ok := t.selected()
	if !ok {
		// An empty list has one useful thing to do with it.
		return []widget.Action{{Name: "add", Run: func() tea.Cmd { t.startAdd(); return nil }}}
	}
	name := "done"
	if task.Done {
		name = "reopen"
	}
	return []widget.Action{{Name: name, Run: t.toggle}}
}

// open hands the terminal to the editor, for the things a four-key widget
// should not try to be: reordering, sections, notes under a task.
func (t *Tasks) open() tea.Cmd {
	fields := strings.Fields(t.editor)
	if len(fields) == 0 {
		fields = []string{"vi"}
	}
	if _, err := exec.LookPath(fields[0]); err != nil {
		t.status = fields[0] + " is not installed, or not on $PATH"
		return nil
	}

	c := exec.Command(fields[0], append(fields[1:], t.path)...)
	// ExecProcess's callback is bubbletea's, not ours, so the result is
	// addressed by hand rather than through Cmd.
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return t.Address(editedMsg{err: err})
	})
}

// read loads the file off the UI goroutine.
func (t *Tasks) read() tea.Cmd {
	path := t.path
	return t.Cmd(func() tea.Msg {
		f, err := todo.Load(path)
		return loadedMsg{file: f, err: err}
	})
}

// mutate re-reads the file, applies op and writes it back, off the UI
// goroutine.
//
// Re-reading rather than writing back the copy in memory is what makes it safe
// to keep the same file open in an editor: ctOS never writes a version of the
// file it has not just read, so an edit made elsewhere in the last minute is
// not silently reverted by a keystroke here.
//
// done is a function because a message like "cleared 3 tasks" is only knowable
// once op has run.
func (t *Tasks) mutate(done func() string, op func(*todo.File) error) tea.Cmd {
	if t.busy {
		return nil
	}
	path := t.path
	t.busy, t.status = true, ""

	return t.Cmd(func() tea.Msg {
		f, err := todo.Load(path)
		if err != nil {
			return doneMsg{err: err}
		}
		if err := op(&f); err != nil {
			return doneMsg{err: err}
		}
		if err := todo.Save(path, f); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: done(), file: f}
	})
}

// onTask applies op to one task, found again in the file as it is on disk.
//
// A task that is no longer there says so rather than acting on whatever has
// moved into its place — the one outcome worse than doing nothing.
func (t *Tasks) onTask(target todo.Task, done string, op func(*todo.File, int)) tea.Cmd {
	return t.mutate(func() string { return done }, func(f *todo.File) error {
		i, ok := f.Locate(target.Line, target.Raw)
		if !ok {
			return fmt.Errorf("%s is not in the file any more; press r to reload", quote(target.Text))
		}
		op(f, i)
		return nil
	})
}

// selected is the task under the cursor.
func (t *Tasks) selected() (todo.Task, bool) {
	if t.list.Empty() || t.list.Cursor() >= len(t.rows) {
		return todo.Task{}, false
	}
	return t.rows[t.list.Cursor()], true
}

// remember records what the cursor is on, for follow to put it back.
func (t *Tasks) remember() {
	if task, ok := t.selected(); ok {
		t.selText = task.Text
	}
}

// rebuild re-filters and re-sorts after a read or a key, then puts the cursor
// back where it was.
func (t *Tasks) rebuild() {
	now := time.Now()
	t.rows = todo.Filter(t.file.Tasks, t.show, now)
	todo.Sort(t.rows, now)
	if len(t.rows) > t.limit {
		t.rows = t.rows[:t.limit]
	}
	t.list.SetLen(len(t.rows))
	t.follow()
}

// follow puts the cursor back on the task it was on, which a re-sort or a
// refresh may have moved. A task that has gone — ticked off in a view that
// hides completed ones — leaves the cursor where it is, on whatever has taken
// that position.
func (t *Tasks) follow() {
	if t.selText == "" {
		return
	}
	for i, r := range t.rows {
		if r.Text == t.selText {
			t.list.Select(i)
			return
		}
	}
	t.remember()
}

// counts summarises the whole file, not the filtered view: the headline has to
// say what is outstanding even while the list is showing only today.
func (t *Tasks) counts() todo.Counts {
	return todo.Count(t.file.Tasks, time.Now())
}

// quote wraps a task's text for a status line, cutting it so one long task
// cannot push the rest of the message off the end.
func quote(text string) string {
	const maxWords = 40
	r := []rune(text)
	if len(r) > maxWords {
		text = strings.TrimSpace(string(r[:maxWords])) + "…"
	}
	return "\"" + text + "\""
}

// plural writes a count with its noun: "1 task", "3 tasks".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
