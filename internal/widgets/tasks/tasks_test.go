package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/todo"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"gopkg.in/yaml.v3"
)

// newTasks builds the widget from a YAML block, the way the registry does.
func newTasks(t *testing.T, yamlSrc string) (widget.Widget, error) {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(yamlSrc), &node); err != nil {
		t.Fatal(err)
	}
	return New(widget.Context{Name: "tasks", Node: node.Content[0], Theme: theme.New("")})
}

// seed writes a checklist and returns a widget reading it, already loaded and
// sized like a pane on a dashboard.
func seed(t *testing.T, contents string) (*Tasks, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tasks.md")
	if contents != "" {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	w, err := newTasks(t, "type: tasks\npath: "+path+"\ngroup: false\n")
	if err != nil {
		t.Fatal(err)
	}
	tw := w.(*Tasks)
	tw.SetSize(60, 12)
	tw.Focus()
	run(t, tw, tw.read())
	return tw, path
}

// run executes a command and feeds its result back, the way the dashboard
// delivers an addressed message.
func run(t *testing.T, w *Tasks, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 8 {
			t.Fatal("commands did not settle")
		}
		msg := widget.Unwrap(cmd())
		if msg == nil {
			return
		}
		cmd = w.Update(msg)
	}
}

// press sends one key, as the dashboard would.
func press(t *testing.T, w *Tasks, key string) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	run(t, w, w.Update(msg))
}

// enter is what the shell does with the key: straight through while the widget
// is grabbing the keyboard, and the primary action otherwise (see model.go).
func enter(t *testing.T, w *Tasks) {
	t.Helper()
	if w.GrabsKeys() {
		run(t, w, w.Update(tea.KeyMsg{Type: tea.KeyEnter}))
		return
	}
	actions := w.Actions()
	if len(actions) == 0 {
		t.Fatal("nothing bound to enter")
	}
	run(t, w, actions[0].Run())
}

// typeText types into the input box.
func typeText(t *testing.T, w *Tasks, text string) {
	t.Helper()
	for _, r := range text {
		press(t, w, string(r))
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func texts(tasks []todo.Task) []string {
	out := make([]string, len(tasks))
	for i, task := range tasks {
		out[i] = task.Text
	}
	return out
}

// plainView renders the widget with the styling stripped, so a test can assert
// on what is actually on screen.
func plainView(w *Tasks) string { return ansi.Strip(w.View()) }

const sample = `# Tasks

- [ ] buy milk
- [x] ship the widget
- [ ] pay rent due:2020-01-01
`

func TestNewRejectsBadConfig(t *testing.T) {
	if _, err := newTasks(t, "type: tasks\npath: \"\"\n"); err == nil {
		t.Error("an empty path was accepted")
	}
	if _, err := newTasks(t, "type: tasks\nshow: finished\n"); err == nil {
		t.Error("an unknown show mode was accepted")
	}
	if _, err := newTasks(t, "type: tasks\nrefresh: soon\n"); err == nil {
		t.Error("an unparseable refresh was accepted")
	}
}

// A dashboard that says nothing should still work: the list lands with the
// user's notes, which is where the notes widget already looks.
func TestNewDefaultsToTheNotesDirectory(t *testing.T) {
	w, err := newTasks(t, "type: tasks\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := w.(*Tasks).path; got != defaultPath {
		t.Errorf("path = %q, want %q", got, defaultPath)
	}
}

func TestAddWritesToTheFile(t *testing.T) {
	w, path := seed(t, sample)

	press(t, w, "a")
	if !w.GrabsKeys() {
		t.Fatal("the add box did not claim the keyboard")
	}
	typeText(t, w, "water the plants")
	enter(t, w)

	if w.GrabsKeys() {
		t.Error("the add box kept the keyboard after enter")
	}
	if !strings.Contains(read(t, path), "- [ ] water the plants") {
		t.Errorf("the task was not written:\n%s", read(t, path))
	}
	if got := len(w.file.Tasks); got != 4 {
		t.Errorf("the widget holds %d tasks, want 4", got)
	}
}

// The date syntax is the whole point of "tasks for today", so it has to work
// from the same box the text is typed in.
func TestAddParsesADueShorthand(t *testing.T) {
	w, path := seed(t, sample)

	press(t, w, "a")
	typeText(t, w, "call the bank due:today")
	enter(t, w)

	want := "- [ ] call the bank due:" + time.Now().Format(todo.DateLayout)
	if !strings.Contains(read(t, path), want) {
		t.Errorf("want a line %q in:\n%s", want, read(t, path))
	}
}

func TestEscapeAbandonsTheInput(t *testing.T) {
	w, path := seed(t, sample)
	before := read(t, path)

	press(t, w, "a")
	typeText(t, w, "never mind")
	press(t, w, "esc")

	if w.GrabsKeys() {
		t.Error("esc left the keyboard grabbed")
	}
	if read(t, path) != before {
		t.Error("esc wrote to the file anyway")
	}
}

func TestBackspaceEditsTheInput(t *testing.T) {
	w, _ := seed(t, sample)
	press(t, w, "a")
	typeText(t, w, "abc")
	press(t, w, "backspace")

	if w.input != "ab" {
		t.Errorf("input = %q, want %q", w.input, "ab")
	}
}

func TestEnterTicksTheSelectedTaskOff(t *testing.T) {
	w, path := seed(t, sample)

	// Sorted, the overdue task leads.
	if got := texts(w.rows); got[0] != "pay rent" {
		t.Fatalf("rows = %v, want the overdue task first", got)
	}
	enter(t, w)

	if !strings.Contains(read(t, path), "- [x] pay rent due:2020-01-01") {
		t.Errorf("the task was not ticked off:\n%s", read(t, path))
	}

	// And back again: enter on a done task reopens it.
	w.list.Select(0)
	w.remember()
	for i, r := range w.rows {
		if r.Text == "pay rent" {
			w.list.Select(i)
		}
	}
	enter(t, w)
	if !strings.Contains(read(t, path), "- [ ] pay rent due:2020-01-01") {
		t.Errorf("the task was not reopened:\n%s", read(t, path))
	}
}

// Space is the other half of the same binding, for anyone who reaches for it.
func TestSpaceAlsoToggles(t *testing.T) {
	w, path := seed(t, sample)
	press(t, w, " ")

	if !strings.Contains(read(t, path), "- [x] pay rent") {
		t.Errorf("space did not tick the task off:\n%s", read(t, path))
	}
}

func TestDueTodayIsAToggle(t *testing.T) {
	w, path := seed(t, sample)
	// "buy milk" has no date.
	for i, r := range w.rows {
		if r.Text == "buy milk" {
			w.list.Select(i)
			w.remember()
		}
	}

	press(t, w, "t")
	stamp := "due:" + time.Now().Format(todo.DateLayout)
	if !strings.Contains(read(t, path), "- [ ] buy milk "+stamp) {
		t.Errorf("t did not date the task:\n%s", read(t, path))
	}

	press(t, w, "t")
	if strings.Contains(read(t, path), "buy milk "+stamp) {
		t.Errorf("t did not take the date off again:\n%s", read(t, path))
	}
}

func TestEditRewritesTheTask(t *testing.T) {
	w, path := seed(t, sample)
	for i, r := range w.rows {
		if r.Text == "buy milk" {
			w.list.Select(i)
			w.remember()
		}
	}

	press(t, w, "e")
	if w.input != "buy milk" {
		t.Fatalf("the edit box opened with %q, want the task's text", w.input)
	}
	typeText(t, w, " and bread")
	enter(t, w)

	out := read(t, path)
	if !strings.Contains(out, "- [ ] buy milk and bread") {
		t.Errorf("the edit was not written:\n%s", out)
	}
}

// An edit box on a dated task shows the date in the form the box accepts, so
// changing a date is the same gesture as changing the text.
func TestEditPrefillsTheDueDate(t *testing.T) {
	w, _ := seed(t, sample)
	press(t, w, "e") // the overdue task is selected

	if w.input != "pay rent due:2020-01-01" {
		t.Errorf("edit box = %q, want the date prefilled", w.input)
	}
}

func TestDeleteTakesTwoPresses(t *testing.T) {
	w, path := seed(t, sample)

	press(t, w, "d")
	if w.arm != armedDelete {
		t.Fatal("d did not arm a delete")
	}
	if !strings.Contains(read(t, path), "pay rent") {
		t.Fatal("one press of d already deleted the task")
	}

	press(t, w, "esc")
	if w.arm != armedNone {
		t.Error("esc did not cancel the armed delete")
	}
	if !strings.Contains(read(t, path), "pay rent") {
		t.Error("a cancelled delete went ahead anyway")
	}

	press(t, w, "d")
	press(t, w, "d")
	if strings.Contains(read(t, path), "pay rent") {
		t.Errorf("the task survived a confirmed delete:\n%s", read(t, path))
	}
}

// While a delete is armed, enter answers the prompt rather than toggling
// whatever the cursor happens to be on.
func TestEnterConfirmsAnArmedDelete(t *testing.T) {
	w, path := seed(t, sample)
	press(t, w, "d")

	if got := w.Actions()[0].Name; got != "delete" {
		t.Errorf("enter is bound to %q while armed, want delete", got)
	}
	enter(t, w)
	if strings.Contains(read(t, path), "pay rent") {
		t.Error("enter did not carry out the armed delete")
	}
}

func TestClearCompletedTakesTwoPresses(t *testing.T) {
	w, path := seed(t, sample)

	press(t, w, "x")
	if w.arm != armedClear {
		t.Fatal("x did not arm a clear")
	}
	press(t, w, "x")

	out := read(t, path)
	if strings.Contains(out, "ship the widget") {
		t.Errorf("the completed task survived:\n%s", out)
	}
	if !strings.Contains(out, "buy milk") || !strings.Contains(out, "# Tasks") {
		t.Errorf("clearing took more than the completed tasks:\n%s", out)
	}
}

// Nothing to clear is nothing to confirm: the prompt never appears.
func TestClearDoesNotArmWithNothingDone(t *testing.T) {
	w, _ := seed(t, "- [ ] only this\n")
	press(t, w, "x")
	if w.arm != armedNone {
		t.Error("x armed a clear with no completed tasks")
	}
}

func TestShowCyclesAndFilters(t *testing.T) {
	w, _ := seed(t, sample)
	if w.show != todo.ShowAll || len(w.rows) != 3 {
		t.Fatalf("show = %q with %d rows, want all with 3", w.show, len(w.rows))
	}

	press(t, w, "f")
	if w.show != todo.ShowOpen || len(w.rows) != 2 {
		t.Errorf("show = %q with %d rows, want open with 2", w.show, len(w.rows))
	}

	press(t, w, "f")
	if w.show != todo.ShowToday || len(w.rows) != 1 {
		t.Errorf("show = %q with %d rows, want today with 1 (the overdue task)", w.show, len(w.rows))
	}

	press(t, w, "f")
	if w.show != todo.ShowAll {
		t.Errorf("show = %q, want the cycle back to all", w.show)
	}
}

// Every write re-reads the file first, so a task that has gone away in another
// editor says so rather than acting on whatever has taken its place.
func TestAWriteAgainstAVanishedTaskReportsIt(t *testing.T) {
	w, path := seed(t, sample)
	target := w.rows[0]

	if err := os.WriteFile(path, []byte("- [ ] something else entirely\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, w, w.onTask(target, "done", func(f *todo.File, i int) { f.Toggle(i) }))

	if !strings.Contains(w.status, "not in the file any more") {
		t.Errorf("status = %q, want it to say the task is gone", w.status)
	}
	if got := read(t, path); got != "- [ ] something else entirely\n" {
		t.Errorf("the file was written anyway:\n%s", got)
	}
}

// A refresh re-sorts the list. The cursor belongs to the user, so it follows
// the task rather than the position.
func TestTheCursorFollowsItsTaskAcrossARefresh(t *testing.T) {
	w, path := seed(t, "- [ ] alpha\n- [ ] beta\n")
	w.list.Select(1)
	w.remember()

	// beta becomes overdue, which sorts it to the top.
	if err := os.WriteFile(path, []byte("- [ ] alpha\n- [ ] beta due:2020-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, w, w.read())

	if got := w.rows[w.list.Cursor()].Text; got != "beta" {
		t.Errorf("cursor is on %q, want it still on beta", got)
	}
}

func TestViewRendersTheList(t *testing.T) {
	w, _ := seed(t, sample)
	out := plainView(w)

	for _, want := range []string{"pay rent", "buy milk", "ship the widget", "open"} {
		if !strings.Contains(out, want) {
			t.Errorf("the view is missing %q:\n%s", want, out)
		}
	}
	if lines := strings.Split(out, "\n"); len(lines) != w.H {
		t.Errorf("the view is %d lines, want the pane's %d", len(lines), w.H)
	}
}

func TestViewGroupsWhenAsked(t *testing.T) {
	w, _ := seed(t, sample)
	w.group = true
	out := plainView(w)

	for _, want := range []string{"OVERDUE", "SOMEDAY", "DONE"} {
		if !strings.Contains(out, want) {
			t.Errorf("the view is missing the %q heading:\n%s", want, out)
		}
	}
	if lines := strings.Split(out, "\n"); len(lines) != w.H {
		t.Errorf("headings pushed the view to %d lines, want %d", len(lines), w.H)
	}
}

// Every list-shaped widget in ctOS collapses to a strip when the bar hands it
// one line (ADR-032, ADR-034).
func TestOneLineRendersAStrip(t *testing.T) {
	w, _ := seed(t, sample)
	w.SetSize(60, 1)

	out := plainView(w)
	if strings.Contains(out, "\n") {
		t.Errorf("the strip is more than one line:\n%s", out)
	}
	if !strings.Contains(out, "open") || !strings.Contains(out, "pay rent") {
		t.Errorf("the strip should carry the counts and the next task, got %q", out)
	}
	if got := w.Lines(60); got != 1 {
		t.Errorf("Lines = %d in a one-line pane, want 1", got)
	}
}

func TestStripSaysWhenEverythingIsDone(t *testing.T) {
	w, _ := seed(t, "- [x] done it\n")
	w.SetSize(60, 1)

	if out := plainView(w); !strings.Contains(out, "all done") {
		t.Errorf("strip = %q, want it to say everything is done", out)
	}
}

func TestEmptyListInvitesTheFirstTask(t *testing.T) {
	w, _ := seed(t, "")
	if out := plainView(w); !strings.Contains(out, "press a to add a task") {
		t.Errorf("an empty list said %q", out)
	}
	// Enter has to do something useful with an empty list.
	if got := w.Actions()[0].Name; got != "add" {
		t.Errorf("enter is bound to %q on an empty list, want add", got)
	}
}

func TestNarrowPaneDropsTheDateColumnNotTheTask(t *testing.T) {
	w, _ := seed(t, sample)
	w.SetSize(20, 12)

	out := plainView(w)
	for _, line := range strings.Split(out, "\n") {
		if got := ansi.StringWidth(line); got > 20 {
			t.Errorf("line %q is %d cells wide, want at most 20", line, got)
		}
	}
	if !strings.Contains(out, "pay rent") {
		t.Errorf("the task itself was dropped:\n%s", out)
	}
}
