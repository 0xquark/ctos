package git

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/0xquark/ctos/internal/repos"
	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// newGit builds the widget through the registry, so the tests exercise the
// same config path a dashboard does — strict decoding included.
func newGit(t *testing.T, src string) (*Git, error) {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	w, err := widget.New("git", widget.Context{Name: "g", Node: doc.Content[0], Theme: theme.New("")})
	if err != nil {
		return nil, err
	}
	return w.(*Git), nil
}

// loaded is a widget holding the fixture repositories, sized and focused.
func loaded(t *testing.T, src string, w, h int) *Git {
	t.Helper()
	g, err := newGit(t, src)
	if err != nil {
		t.Fatal(err)
	}
	g.repos = sample()
	g.apply()
	g.Focus()
	g.SetSize(w, h)
	return g
}

func sample() []repos.Repo {
	now := time.Now()
	return []repos.Repo{
		{Name: "ctos", Path: "/r/ctos", Branch: "main", Upstream: "origin/main",
			Modified: 2, Untracked: 1, Ahead: 2, Last: now.Add(-12 * time.Minute),
			Files: []repos.File{
				{Path: "cmd/main.go", X: '.', Y: 'M'},
				{Path: "README.md", X: 'M', Y: '.'},
				{Path: "notes/", X: '?', Y: '?'},
			}},
		{Name: "dotfiles", Path: "/r/dotfiles", Branch: "main", Upstream: "origin/main",
			Last: now.Add(-3 * 24 * time.Hour)},
		{Name: "experiment", Path: "/r/experiment", Branch: "feature/x",
			Staged: 4, Behind: 1, Last: now.Add(-2 * time.Hour)},
		{Name: "detached", Path: "/r/detached", Head: "8f1a2b3", Last: now.Add(-40 * 24 * time.Hour)},
		{Name: "broken", Path: "/r/broken", Err: fmt.Errorf("not a git repository")},
	}
}

func TestConfigDefaults(t *testing.T) {
	g, err := newGit(t, "type: git\nscan: /tmp")
	if err != nil {
		t.Fatal(err)
	}
	if g.depth != 2 {
		t.Errorf("depth = %d, want 2", g.depth)
	}
	if g.order != byActivity {
		t.Errorf("sort = %q, want %q", g.order, byActivity)
	}
	if g.refresh != defaultRefresh {
		t.Errorf("refresh = %s, want %s", g.refresh, defaultRefresh)
	}
	if len(g.command) != 1 || g.command[0] != defaultCommand {
		t.Errorf("command = %v, want [%s]", g.command, defaultCommand)
	}
}

// A hand-written dashboard gets told what it did wrong, by name.
func TestConfigErrors(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"neither source", "type: git", `one of "repos:" or "scan:"`},
		{"both sources", "type: git\nscan: /tmp\nrepos: [/r]", `not both`},
		{"negative depth", "type: git\nscan: /tmp\ndepth: -1", `cannot be negative`},
		{"unknown sort", "type: git\nscan: /tmp\nsort: sideways", `unknown sort "sideways"`},
		{"unknown key", "type: git\nscan: /tmp\nnope: 1", `nope`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newGit(t, tc.yaml)
			if err == nil {
				t.Fatal("want an error, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should contain %q", err, tc.want)
			}
		})
	}
}

// Two git processes per repository is enough without a dashboard asking for
// them every second.
func TestRefreshHasAFloor(t *testing.T) {
	g, err := newGit(t, "type: git\nscan: /tmp\nrefresh: 100ms")
	if err != nil {
		t.Fatal(err)
	}
	if g.refresh != minRefresh {
		t.Errorf("refresh = %s, want the %s floor", g.refresh, minRefresh)
	}
}

// The default order answers "what was I working on?", so the most recently
// committed repository is first.
func TestSortOrders(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 70, 10)
	if g.repos[0].Name != "ctos" {
		t.Errorf("by activity, first = %q, want ctos", g.repos[0].Name)
	}

	g.order = byName
	g.apply()
	if g.repos[0].Name != "broken" {
		t.Errorf("by name, first = %q, want broken", g.repos[0].Name)
	}

	g.order = byDirty
	g.apply()
	if g.repos[0].Name != "experiment" {
		t.Errorf("by dirty, first = %q, want experiment (4 staged)", g.repos[0].Name)
	}
}

// "s" cycles the sort in place, the way the processes widget does.
func TestSortKeyCycles(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 70, 10)
	for _, want := range []order{byName, byDirty, byActivity} {
		g.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		if g.order != want {
			t.Fatalf("after s: sort = %q, want %q", g.order, want)
		}
	}
}

// The point of the filter is a list you only look at when it is not empty.
func TestOnlyInterestingHidesTheQuietOnes(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp\nonly_interesting: true", 70, 10)

	for _, r := range g.visible() {
		if r.Err == nil && r.Clean() && r.Synced() {
			t.Errorf("%q is clean and in sync but still listed", r.Name)
		}
	}
	// An unreadable repo is interesting: it is the one you may need to fix.
	var sawBroken bool
	for _, r := range g.visible() {
		sawBroken = sawBroken || r.Name == "broken"
	}
	if !sawBroken {
		t.Error("a repository that could not be read should still be listed")
	}

	// "i" turns it off again.
	g.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if len(g.visible()) != len(g.repos) {
		t.Errorf("after i: %d of %d repos visible", len(g.visible()), len(g.repos))
	}
}

// Every row has to fit the pane it was given, at every width a dashboard might
// hand over.
func TestRowsNeverExceedTheWidth(t *testing.T) {
	for w := 10; w <= 120; w++ {
		g := loaded(t, "type: git\nscan: /tmp", w, 10)
		for i, line := range strings.Split(g.View(), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Fatalf("width %d: line %d is %d cells (%q)", w, i, got, stripANSI(line))
			}
		}
	}
}

// Wide enough for everything, and the columns drop in order as it narrows.
func TestColumnsDropInOrder(t *testing.T) {
	wide := layoutFor(80)
	if wide.ref == 0 || wide.age == 0 {
		t.Errorf("a wide pane should carry every column, got %+v", wide)
	}

	// The branch is the widest column and the first to go.
	mid := layoutFor(34)
	if mid.ref != 0 {
		t.Errorf("at 34 cells the branch should be gone, got %+v", mid)
	}
	if mid.state == 0 || mid.age == 0 {
		t.Errorf("at 34 cells the state and age should survive, got %+v", mid)
	}

	narrow := layoutFor(24)
	if narrow.age != 0 || narrow.state == 0 {
		t.Errorf("at 24 cells only the state should remain, got %+v", narrow)
	}
}

// A repository that could not be read spends its row saying why. Zeroed
// columns would draw it as a clean repo, which is the wrong answer.
func TestUnreadableRepoShowsItsError(t *testing.T) {
	g := loaded(t, "type: git\nsort: name\nscan: /tmp", 70, 10)
	out := stripANSI(g.View())
	if !strings.Contains(out, "not a git repository") {
		t.Errorf("the error is missing from the list:\n%s", out)
	}
	if strings.Contains(out, "broken     "+cleanMark) {
		t.Errorf("an unreadable repo was drawn as clean:\n%s", out)
	}
}

// One line is a status strip, not a list of one repository.
func TestOneLineRendersTheStrip(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 80, 1)

	out := g.View()
	if n := lipgloss.Height(out); n != 1 {
		t.Errorf("rendered %d lines, want 1", n)
	}
	if got := lipgloss.Width(out); got > 80 {
		t.Errorf("the strip is %d cells wide", got)
	}
	// The strip is for what needs attention, so a clean repo is not on it.
	if strings.Contains(stripANSI(out), "dotfiles") {
		t.Errorf("a clean repo should not take room on the strip: %q", stripANSI(out))
	}
	if !strings.Contains(stripANSI(out), "ctos") {
		t.Errorf("the strip is missing the repo with work in it: %q", stripANSI(out))
	}

	// A widget offered one line asks for one, which is what puts it in the
	// status bar as a strip rather than as a list.
	if got := g.Lines(80); got != 1 {
		t.Errorf("Lines = %d, want 1", got)
	}
}

// When everything is quiet the strip says so rather than rendering nothing,
// which would read as a widget that failed to load.
func TestStripSaysWhenEverythingIsClean(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 80, 1)
	g.repos = []repos.Repo{
		{Name: "a", Branch: "main"},
		{Name: "b", Branch: "main"},
	}
	g.apply()

	out := stripANSI(g.View())
	if !strings.Contains(out, "2 repos clean") {
		t.Errorf("strip = %q", out)
	}
}

// An empty list explains itself: whether that is good news or a filter.
func TestEmptyListExplainsItself(t *testing.T) {
	g := loaded(t, "type: git\nscan: /code\nonly_interesting: true", 70, 10)
	g.repos = []repos.Repo{{Name: "a", Branch: "main"}}
	g.apply()
	if out := stripANSI(g.View()); !strings.Contains(out, "clean and in sync") {
		t.Errorf("view = %q", out)
	}

	g.repos = nil
	g.apply()
	if out := stripANSI(g.View()); !strings.Contains(out, "/code") {
		t.Errorf("an empty scan should name the directory it looked in, got %q", out)
	}
}

// Enter means different things at the two levels, and the footer has to say
// which: opening a repository, or staging the file under the cursor.
func TestActionFollowsTheLevel(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 70, 10)

	if a := g.Actions(); len(a) != 1 || a[0].Name != "files" {
		t.Fatalf("actions = %+v, want one named files", a)
	}

	g.enter()
	if a := g.Actions(); len(a) != 1 || a[0].Name != "stage" {
		t.Fatalf("inside a repo: actions = %+v, want one named stage", a)
	}

	// Move to the file that is already staged.
	g.files.Select(1)
	if a := g.Actions(); len(a) != 1 || a[0].Name != "unstage" {
		t.Fatalf("on a staged file: actions = %+v, want one named unstage", a)
	}

	// An empty list has nothing to open.
	g.leave()
	g.repos = nil
	g.apply()
	if len(g.Actions()) != 0 {
		t.Error("an empty list should offer no action")
	}
}

// Opening a tool that is not installed reports it instead of handing
// bubbletea a command that cannot start.
func TestOpeningAMissingToolSaysSo(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp\ncommand: definitely-not-installed-xyz", 70, 10)
	if cmd := g.launch(); cmd != nil {
		t.Error("want no command for a tool that is not on $PATH")
	}
	if !strings.Contains(g.status, "not installed") {
		t.Errorf("status = %q, want an explanation", g.status)
	}
}

// A tick that lands while a scan is still running reschedules rather than
// starting a second one.
func TestTickDoesNotStackScans(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 70, 10)
	g.inflight = true
	g.Update(tickMsg{})
	if !g.inflight {
		t.Error("the guard should still be set")
	}

	g.inflight = false
	if cmd := g.Update(tickMsg{}); cmd == nil {
		t.Error("an idle tick should start a scan")
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Enter drills into a repository and esc comes back, which is the whole shape
// of the widget: a list of repositories, and one repository's files.
func TestDrillInAndBack(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 70, 10)

	g.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if g.mode != modeRepo || g.openPath != "/r/ctos" {
		t.Fatalf("mode = %v, open = %q", g.mode, g.openPath)
	}
	if g.files.Len() != 3 {
		t.Errorf("file list holds %d entries, want 3", g.files.Len())
	}
	if out := stripANSI(g.View()); !strings.Contains(out, "cmd/main.go") {
		t.Errorf("the file list is missing from the view:\n%s", out)
	}

	g.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if g.mode != modeList {
		t.Error("esc should come back to the list")
	}
}

// The open repository is remembered by path, because a refresh re-sorts the
// list and an index would then point somewhere else.
func TestOpenRepoSurvivesARefresh(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 70, 10)
	g.Update(tea.KeyMsg{Type: tea.KeyEnter})

	g.order = byName
	g.apply()
	if g.openPath != "/r/ctos" {
		t.Errorf("a re-sort moved the open repository to %q", g.openPath)
	}
	if r, ok := g.current(); !ok || r.Name != "ctos" {
		t.Errorf("current() = %+v", r)
	}

	// A repository that goes away closes the level rather than leaving the
	// user staging files into nothing.
	kept := g.repos[:0]
	for _, r := range g.repos {
		if r.Path != "/r/ctos" {
			kept = append(kept, r)
		}
	}
	g.repos = kept
	g.apply()
	if g.mode != modeList {
		t.Error("the open repository is gone; the widget should be back at the list")
	}
}

// There is nothing inside a path git could not read, so entering it says why
// instead of opening an empty pane.
func TestEnteringAnUnreadableRepoSaysWhy(t *testing.T) {
	g := loaded(t, "type: git\nsort: name\nscan: /tmp", 70, 10)
	g.list.Select(0) // "broken", sorted by name
	g.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if g.mode != modeList {
		t.Error("an unreadable repository should not open")
	}
	if !strings.Contains(g.status, "not a git repository") {
		t.Errorf("status = %q", g.status)
	}
}

// Enter on a file stages it, and on a staged file takes it back out. The
// operation runs off the UI goroutine, so what the key returns is a command.
func TestToggleStagesAndUnstages(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 70, 10)
	g.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// cmd/main.go is modified but not staged.
	if cmd := g.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("staging should return a command")
	}
	if !g.busy {
		t.Error("the widget should be busy while the operation runs")
	}

	// While one operation is running, another key does nothing: two git
	// processes writing the same index is how a lock file gets left behind.
	if cmd := g.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}); cmd != nil {
		t.Error("a second operation should not start while one is running")
	}

	// The result clears the guard and reports what happened.
	g.Update(doneMsg{text: "staged cmd/main.go"})
	if g.busy {
		t.Error("the guard should be clear once the operation finishes")
	}
	if g.status != "staged cmd/main.go" {
		t.Errorf("status = %q", g.status)
	}
}

// A failed operation shows git's own message rather than swallowing it.
func TestFailedOperationShowsTheReason(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 70, 10)
	g.Update(tea.KeyMsg{Type: tea.KeyEnter})
	g.Update(doneMsg{err: fmt.Errorf("No stash entries found.")})

	if !strings.Contains(g.status, "No stash entries") {
		t.Errorf("status = %q, want git's message", g.status)
	}
}

// The commit box takes the whole keyboard, so typing "q" types a q.
func TestCommitBoxOwnsTheKeyboard(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 70, 10)
	g.Update(tea.KeyMsg{Type: tea.KeyEnter})

	g.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !g.GrabsKeys() {
		t.Fatal("the commit box should claim the keyboard")
	}

	for _, r := range "fix: q and s" {
		g.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if g.message != "fix: q and s" {
		t.Errorf("message = %q", g.message)
	}

	g.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if g.message != "fix: q and " {
		t.Errorf("after backspace: %q", g.message)
	}

	// Esc throws the message away and hands the keyboard back.
	g.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if g.GrabsKeys() || g.message != "" {
		t.Errorf("esc left typing=%v message=%q", g.typing, g.message)
	}
	if g.mode != modeRepo {
		t.Error("esc out of the commit box should not also leave the repository")
	}
}

// Enter commits what was typed and gives the keyboard back.
func TestCommitBoxSubmits(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 70, 10)
	g.Update(tea.KeyMsg{Type: tea.KeyEnter})
	g.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	for _, r := range "wip" {
		g.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	if cmd := g.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter should run the commit")
	}
	if g.GrabsKeys() {
		t.Error("the keyboard should come back after enter")
	}
}

// A repository with nothing to commit says so rather than drawing an empty
// pane that looks like a widget that failed.
func TestCleanRepoSaysNothingToCommit(t *testing.T) {
	g := loaded(t, "type: git\nsort: name\nscan: /tmp", 70, 10)
	g.list.Select(2) // "dotfiles", clean
	g.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if out := stripANSI(g.View()); !strings.Contains(out, "nothing to commit") {
		t.Errorf("view = %q", out)
	}
}

// Every row inside a repository fits the pane too.
func TestFileRowsNeverExceedTheWidth(t *testing.T) {
	for w := 10; w <= 120; w += 3 {
		g := loaded(t, "type: git\nscan: /tmp", w, 10)
		g.repos[0].Files = append(g.repos[0].Files, repos.File{
			Path: "a/very/deeply/nested/directory/with/a/long/name/file.go", X: '.', Y: 'M',
		})
		g.apply()
		g.Update(tea.KeyMsg{Type: tea.KeyEnter})

		for i, line := range strings.Split(g.View(), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Fatalf("width %d: line %d is %d cells (%q)", w, i, got, stripANSI(line))
			}
		}
	}
}

// A wide pane draws both panels at once, the way lazygit does: what you are
// choosing between on the left, what you have chosen on the right.
func TestWidePaneDrawsBothPanels(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 120, 12)

	lw, dw := g.split()
	if dw == 0 {
		t.Fatal("a 120-cell pane should carry both panels")
	}
	if lw < minListPanel || dw < minDetailPanel {
		t.Errorf("split = %d | %d, below the floors %d / %d", lw, dw, minListPanel, minDetailPanel)
	}

	out := stripANSI(g.View())
	if !strings.Contains(out, "ctos") {
		t.Error("the list is missing")
	}
	for _, want := range []string{"changes (3)", "cmd/main.go", "recent"} {
		if !strings.Contains(out, want) {
			t.Errorf("the detail panel is missing %q:\n%s", want, out)
		}
	}
	// The rule runs down every line, so the frames beside it stay straight.
	for i, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "│") {
			t.Errorf("line %d has no rule: %q", i, line)
		}
	}
}

// The panel follows the cursor, so you can see what is in a repository before
// deciding to go into it.
func TestDetailPanelFollowsTheCursor(t *testing.T) {
	g := loaded(t, "type: git\nsort: name\nscan: /tmp", 120, 12)

	// Sorted by name: broken, ctos, detached, dotfiles, experiment.
	g.list.Select(2) // "detached", which has no files in the fixture
	g.syncFiles()
	if out := stripANSI(g.View()); !strings.Contains(out, "nothing to commit") {
		t.Errorf("the panel did not follow the cursor:\n%s", out)
	}

	// And the file cursor is re-pointed with it: staging must never act on
	// the file at the old index of a different repository.
	g.list.Select(1) // "ctos"
	g.syncFiles()
	if g.filesPath != "/r/ctos" {
		t.Errorf("filesPath = %q, want /r/ctos", g.filesPath)
	}
	if f, ok := g.selectedFile(); !ok || f.Path != "cmd/main.go" {
		t.Errorf("selected file = %+v, want the first of ctos's", f)
	}
}

// A pane too narrow to divide shows the panel the cursor is in.
func TestNarrowPaneShowsOnePanel(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 40, 12)
	if _, dw := g.split(); dw != 0 {
		t.Fatalf("a 40-cell pane should not be split, got a %d-cell panel", dw)
	}
	if out := stripANSI(g.View()); strings.Contains(out, "changes (") {
		t.Errorf("the list should be alone at this width:\n%s", out)
	}

	g.Update(tea.KeyMsg{Type: tea.KeyEnter})
	out := stripANSI(g.View())
	if !strings.Contains(out, "changes (3)") {
		t.Errorf("entering should show the detail panel:\n%s", out)
	}
	if strings.Contains(out, "dotfiles") {
		t.Errorf("the list should be gone at this width:\n%s", out)
	}
}

// An empty list keeps the whole pane: there is nothing to detail, and half the
// width is not enough to explain why the list is empty.
func TestEmptyListIsNotSplit(t *testing.T) {
	g := loaded(t, "type: git\nscan: /code", 120, 12)
	g.repos = nil
	g.apply()

	if _, dw := g.split(); dw != 0 {
		t.Error("an empty list should not be split")
	}
	if out := stripANSI(g.View()); !strings.Contains(out, "/code") {
		t.Errorf("view = %q", out)
	}
}

// Both panels are on screen at once, and two lit cursors would each claim to
// be where the next keystroke lands.
func TestOnlyTheActivePanelDrawsItsCursor(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 120, 12)

	if !g.lit(true, modeList) || g.lit(true, modeRepo) {
		t.Error("with the list active, only the list's cursor should be lit")
	}

	g.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if g.lit(true, modeList) || !g.lit(true, modeRepo) {
		t.Error("with the files active, only the file cursor should be lit")
	}

	// An unfocused widget lights neither: the dashboard's focus is
	// somewhere else entirely.
	g.Blur()
	if g.lit(true, modeList) || g.lit(true, modeRepo) {
		t.Error("an unfocused widget should light no cursor")
	}
}

// Both panels together still have to fit the pane exactly.
func TestSplitNeverExceedsTheWidth(t *testing.T) {
	for w := 10; w <= 200; w += 7 {
		for _, focused := range []bool{false, true} {
			g := loaded(t, "type: git\nscan: /tmp", w, 12)
			if focused {
				g.Update(tea.KeyMsg{Type: tea.KeyEnter})
			}
			for i, line := range strings.Split(g.View(), "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Fatalf("width %d (focused=%v): line %d is %d cells (%q)",
						w, focused, i, got, stripANSI(line))
				}
			}
		}
	}
}

// The history is read for the repository on show and cached by path, so moving
// the cursor asks for the new one and coming back does not ask again.
func TestHistoryIsLoadedForTheSelection(t *testing.T) {
	g := loaded(t, "type: git\nsort: name\nscan: /tmp", 120, 12)

	if cmd := g.syncDetail(); cmd == nil {
		t.Fatal("the first selection should ask for its history")
	}
	g.Update(commitsMsg{path: g.shownPath(), commits: []repos.Commit{{Hash: "abc", Subject: "x"}}})
	if g.commitsPath != g.shownPath() {
		t.Fatalf("commitsPath = %q, want %q", g.commitsPath, g.shownPath())
	}
	if cmd := g.syncDetail(); cmd != nil {
		t.Error("the same selection should not be re-read")
	}

	g.list.Move(1)
	if cmd := g.syncDetail(); cmd == nil {
		t.Error("a new selection should ask for its history")
	}
	// The old history must not be captioned with the new repository's name.
	if g.commitsPath != "" || g.commits != nil {
		t.Errorf("stale history survived the move: %q %+v", g.commitsPath, g.commits)
	}
}

// A result that lands after the cursor has moved on belongs to nothing on
// screen and is dropped.
func TestStaleHistoryIsDropped(t *testing.T) {
	g := loaded(t, "type: git\nscan: /tmp", 120, 12)
	g.Update(commitsMsg{path: "/r/somewhere-else", commits: []repos.Commit{{Subject: "wrong"}}})

	if g.commits != nil {
		t.Errorf("history for another repository was kept: %+v", g.commits)
	}
}
