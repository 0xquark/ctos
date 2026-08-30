package todo

import (
	"os"
	"strings"
	"testing"
	"time"
)

// now is a fixed Wednesday, so weekday shorthands and "today" are stable.
var now = time.Date(2026, 8, 26, 14, 30, 0, 0, time.Local)

const sample = `# Today

- [ ] buy milk
- [x] ship the tasks widget
- [ ] pay rent due:2026-08-20
* [ ] call the dentist due:2026-09-04

Some prose that is not a task.
  - [ ] a nested one
- not a checkbox
- [~] some other state
`

func parseSample(t *testing.T) File {
	t.Helper()
	f := Parse([]byte(sample))
	if len(f.Tasks) != 5 {
		t.Fatalf("found %d tasks, want 5: %+v", len(f.Tasks), f.Tasks)
	}
	return f
}

func TestParseReadsCheckboxes(t *testing.T) {
	f := parseSample(t)

	want := []struct {
		text string
		done bool
		due  string
	}{
		{"buy milk", false, ""},
		{"ship the tasks widget", true, ""},
		{"pay rent", false, "2026-08-20"},
		{"call the dentist", false, "2026-09-04"},
		{"a nested one", false, ""},
	}
	for i, w := range want {
		got := f.Tasks[i]
		if got.Text != w.text || got.Done != w.done {
			t.Errorf("task %d = %q done=%v, want %q done=%v", i, got.Text, got.Done, w.text, w.done)
		}
		due := ""
		if got.HasDue() {
			due = got.Due.Format(DateLayout)
		}
		if due != w.due {
			t.Errorf("task %d due = %q, want %q", i, due, w.due)
		}
	}

	if got := f.Tasks[3].Marker; got != "*" {
		t.Errorf("marker = %q, want %q — the file's own bullet style should survive", got, "*")
	}
	if got := f.Tasks[4].Indent; got != "  " {
		t.Errorf("indent = %q, want two spaces", got)
	}
}

// A checklist usually lives inside a note. Everything that is not a task has
// to come back out of a rewrite exactly as it went in.
func TestRoundTripPreservesEverythingElse(t *testing.T) {
	f := parseSample(t)
	if got := string(f.Bytes()); got != sample {
		t.Errorf("round trip changed the file:\n--- got ---\n%s\n--- want ---\n%s", got, sample)
	}
}

func TestToggleRewritesOnlyItsOwnLine(t *testing.T) {
	f := parseSample(t)
	f.Toggle(0)

	if !f.Tasks[0].Done {
		t.Error("task 0 is still open after a toggle")
	}
	out := string(f.Bytes())
	if !strings.Contains(out, "- [x] buy milk") {
		t.Errorf("the toggled line was not rewritten:\n%s", out)
	}
	if !strings.Contains(out, "Some prose that is not a task.") {
		t.Error("a toggle disturbed the prose around it")
	}

	f.Toggle(0)
	if got := string(f.Bytes()); got != sample {
		t.Error("toggling twice did not put the file back")
	}
}

func TestSetDueWritesAnISOToken(t *testing.T) {
	f := parseSample(t)
	f.SetDue(0, Day(now))

	if !strings.Contains(string(f.Bytes()), "- [ ] buy milk due:2026-08-26") {
		t.Errorf("due token not written:\n%s", f.Bytes())
	}

	f.SetDue(0, time.Time{})
	if strings.Contains(string(f.Bytes()), "due:2026-08-26") {
		t.Error("clearing the due date left the token behind")
	}
}

// A new task belongs with the existing ones. Appending at the end of the file
// would put it under whatever prose closes the note.
func TestAddGoesAfterTheLastTaskNotTheLastLine(t *testing.T) {
	f := parseSample(t)
	i, err := f.Add("water the plants", now)
	if err != nil {
		t.Fatal(err)
	}
	if f.Tasks[i].Text != "water the plants" {
		t.Errorf("Add returned index %d, which is %q", i, f.Tasks[i].Text)
	}

	lines := strings.Split(strings.TrimRight(string(f.Bytes()), "\n"), "\n")
	last := len(lines) - 1
	if strings.Contains(lines[last], "water the plants") {
		t.Error("the new task landed at the end of the file, past the prose")
	}
	// It inherits the shape of the task above it: a nested "-" bullet.
	if got := lines[f.Tasks[i].Line]; got != "  - [ ] water the plants" {
		t.Errorf("new line = %q, want it to follow the last task's indent", got)
	}
}

func TestAddResolvesTypedShorthands(t *testing.T) {
	var f File
	i, err := f.Add("book the flights due:tomorrow", now)
	if err != nil {
		t.Fatal(err)
	}
	got := f.Tasks[i]
	if got.Text != "book the flights" {
		t.Errorf("text = %q, want the due token taken out", got.Text)
	}
	if want := Day(now).AddDate(0, 0, 1); !got.Due.Equal(want) {
		t.Errorf("due = %v, want %v", got.Due, want)
	}
}

// A shorthand nobody recognises must not be swallowed: the user gets their
// words back rather than a task that quietly lost half its text.
func TestAddKeepsAnUnparseableDueWord(t *testing.T) {
	var f File
	i, err := f.Add("email sam due:whenever", now)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Tasks[i].Text; got != "email sam due:whenever" {
		t.Errorf("text = %q, want the unrecognised word kept", got)
	}
	if f.Tasks[i].HasDue() {
		t.Error("an unparseable date should leave the task undated")
	}
}

func TestAddRejectsEmptyText(t *testing.T) {
	var f File
	if _, err := f.Add("   ", now); err == nil {
		t.Error("an empty task was accepted")
	}
	if _, err := f.Add("due:today", now); err == nil {
		t.Error("a task that is only a date was accepted")
	}
}

func TestDeleteAndClearDone(t *testing.T) {
	f := parseSample(t)
	before := len(f.Lines)

	f.Delete(0)
	if len(f.Lines) != before-1 || len(f.Tasks) != 4 {
		t.Fatalf("after delete: %d lines, %d tasks", len(f.Lines), len(f.Tasks))
	}
	if strings.Contains(string(f.Bytes()), "buy milk") {
		t.Error("the deleted task is still in the file")
	}

	f = parseSample(t)
	if n := f.ClearDone(); n != 1 {
		t.Errorf("ClearDone removed %d, want 1", n)
	}
	if strings.Contains(string(f.Bytes()), "ship the tasks widget") {
		t.Error("a completed task survived ClearDone")
	}
	if !strings.Contains(string(f.Bytes()), "buy milk") {
		t.Error("ClearDone took an open task with it")
	}
}

// The write path re-reads the file first, so the task a key was pressed
// against has to be findable again even when its line has moved.
func TestLocateFallsBackToTheLineText(t *testing.T) {
	f := parseSample(t)
	target := f.Tasks[2]

	moved := Parse([]byte("# a heading someone just added\n\n" + sample))
	i, ok := moved.Locate(target.Line, target.Raw)
	if !ok {
		t.Fatal("the task could not be found after the file shifted")
	}
	if moved.Tasks[i].Text != "pay rent" {
		t.Errorf("located %q, want %q", moved.Tasks[i].Text, "pay rent")
	}
}

func TestLocateFailsWhenTheTaskIsGone(t *testing.T) {
	f := parseSample(t)
	if _, ok := f.Locate(99, "- [ ] something nobody wrote"); ok {
		t.Error("located a task that is not in the file")
	}
}

func TestParseDue(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"today", day(2026, 8, 26), true},
		{"tomorrow", day(2026, 8, 27), true},
		{"tmr", day(2026, 8, 27), true},
		{"yesterday", day(2026, 8, 25), true},
		{"+3d", day(2026, 8, 29), true},
		{"3d", day(2026, 8, 29), true},
		{"2026-12-01", day(2026, 12, 1), true},
		// A weekday means the next one: "wed" on a Wednesday is next week.
		{"wed", day(2026, 9, 2), true},
		{"fri", day(2026, 8, 28), true},
		{"friday", day(2026, 8, 28), true},
		// A bare month-day that has already passed rolls to next year.
		{"09-04", day(2026, 9, 4), true},
		{"01-04", day(2027, 1, 4), true},
		// "none" is a valid answer that clears the date.
		{"none", time.Time{}, true},
		{"whenever", time.Time{}, false},
		{"t", time.Time{}, false}, // ambiguous between tue and thu
		{"", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := ParseDue(c.in, now)
		if ok != c.ok {
			t.Errorf("ParseDue(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("ParseDue(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDueLabel(t *testing.T) {
	cases := []struct {
		days int
		want string
	}{
		{0, "today"},
		{1, "tomorrow"},
		{-1, "yesterday"},
		{-4, "4d ago"},
		{2, "Fri"},
		{20, "15 Sep"},
	}
	for _, c := range cases {
		got := DueLabel(Day(now).AddDate(0, 0, c.days), now)
		if got != c.want {
			t.Errorf("DueLabel(%+d days) = %q, want %q", c.days, got, c.want)
		}
	}
	if got := DueLabel(time.Time{}, now); got != "" {
		t.Errorf("an undated task labelled %q, want empty", got)
	}
}

func TestBucketSortAndCount(t *testing.T) {
	f := parseSample(t)
	tasks := append([]Task(nil), f.Tasks...)
	Sort(tasks, now)

	wantOrder := []string{
		"pay rent",              // overdue
		"call the dentist",      // upcoming
		"buy milk",              // someday
		"a nested one",          // someday, later in the file
		"ship the tasks widget", // done
	}
	for i, want := range wantOrder {
		if tasks[i].Text != want {
			t.Errorf("sorted[%d] = %q, want %q", i, tasks[i].Text, want)
		}
	}

	c := Count(f.Tasks, now)
	want := Counts{Open: 4, Done: 1, Today: 0, Overdue: 1}
	if c != want {
		t.Errorf("Count = %+v, want %+v", c, want)
	}
}

func TestFilter(t *testing.T) {
	f := parseSample(t)
	f.SetDue(0, Day(now)) // buy milk is due today

	cases := []struct {
		show Show
		want int
	}{
		{ShowAll, 5},
		{ShowOpen, 4},
		{ShowToday, 2}, // one overdue, one due today
	}
	for _, c := range cases {
		if got := len(Filter(f.Tasks, c.show, now)); got != c.want {
			t.Errorf("Filter(%s) kept %d, want %d", c.show, got, c.want)
		}
	}
}

func TestParseShow(t *testing.T) {
	if got, err := ParseShow(""); err != nil || got != ShowAll {
		t.Errorf("empty show = %q, %v; want all", got, err)
	}
	if _, err := ParseShow("done"); err == nil {
		t.Error("an unknown show mode was accepted")
	}
}

func TestLoadMissingFileIsAnEmptyList(t *testing.T) {
	f, err := Load(t.TempDir() + "/not-there/tasks.md")
	if err != nil {
		t.Fatalf("a missing checklist should not be an error: %v", err)
	}
	if len(f.Tasks) != 0 {
		t.Errorf("got %d tasks from a file that does not exist", len(f.Tasks))
	}
}

func TestSaveCreatesTheFileAndItsDirectory(t *testing.T) {
	path := t.TempDir() + "/notes/tasks.md"

	var f File
	if _, err := f.Add("first task", now); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}

	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Tasks) != 1 || back.Tasks[0].Text != "first task" {
		t.Errorf("read back %+v", back.Tasks)
	}
}

func TestLoadRefusesBinary(t *testing.T) {
	path := t.TempDir() + "/tasks.md"
	if err := os.WriteFile(path, []byte("- [ ] fine\x00\x01"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("a binary file was read as a checklist")
	}
}
