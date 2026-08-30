package repos

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A repository with everything going on at once: ahead and behind, staged and
// unstaged changes, a conflict and something untracked.
const busyStatus = `# branch.oid 8f1a2b3c4d5e6f708192a3b4c5d6e7f809a1b2c3
# branch.head feature/bar
# branch.upstream origin/feature/bar
# branch.ab +3 -2
1 M. N... 100644 100644 100644 aaa bbb staged.go
1 .M N... 100644 100644 100644 ccc ddd modified.go
1 MM N... 100644 100644 100644 eee fff both.go
2 R. N... 100644 100644 100644 111 222 R100 new.go	old.go
u UU N... 100644 100644 100644 100644 aaa bbb ccc conflict.go
? notes.txt
? build/
`

func TestParseStatus(t *testing.T) {
	var r Repo
	ParseStatus(busyStatus, &r)

	if r.Branch != "feature/bar" {
		t.Errorf("Branch = %q", r.Branch)
	}
	if r.Upstream != "origin/feature/bar" {
		t.Errorf("Upstream = %q", r.Upstream)
	}
	if r.Ahead != 3 || r.Behind != 2 {
		t.Errorf("ahead/behind = %d/%d, want 3/2", r.Ahead, r.Behind)
	}
	// staged.go, both.go and the rename are staged; modified.go and
	// both.go are not.
	if r.Staged != 3 {
		t.Errorf("Staged = %d, want 3", r.Staged)
	}
	if r.Modified != 2 {
		t.Errorf("Modified = %d, want 2", r.Modified)
	}
	if r.Conflicts != 1 {
		t.Errorf("Conflicts = %d, want 1", r.Conflicts)
	}
	if r.Untracked != 2 {
		t.Errorf("Untracked = %d, want 2", r.Untracked)
	}
	if r.Clean() || r.Synced() {
		t.Error("this repo is neither clean nor synced")
	}
	if r.Head != "8f1a2b3" {
		t.Errorf("Head = %q, want the short id", r.Head)
	}
	if r.Ref() != "feature/bar" {
		t.Errorf("Ref = %q, want the branch", r.Ref())
	}
}

// The common case: nothing to report. A clean repo has to come out with every
// count at zero, or the widget cries wolf on every refresh.
func TestParseStatusClean(t *testing.T) {
	var r Repo
	ParseStatus("# branch.oid abc1234def\n# branch.head main\n# branch.upstream origin/main\n# branch.ab +0 -0\n", &r)

	if !r.Clean() {
		t.Errorf("dirty count = %d, want 0 (%+v)", r.Dirty(), r)
	}
	if !r.Synced() {
		t.Errorf("ahead/behind = %d/%d, want level", r.Ahead, r.Behind)
	}
}

// A repo with no upstream has no "branch.ab" line at all, which must read as
// level rather than as an unparsed zero that happens to look the same.
func TestParseStatusNoUpstream(t *testing.T) {
	var r Repo
	ParseStatus("# branch.oid abc1234def\n# branch.head main\n? new.go\n", &r)

	if r.Upstream != "" {
		t.Errorf("Upstream = %q, want none", r.Upstream)
	}
	if !r.Synced() {
		t.Error("a branch with no upstream is not behind anything")
	}
	if r.Untracked != 1 {
		t.Errorf("Untracked = %d, want 1", r.Untracked)
	}
}

// Detached HEAD has no branch to name, so the short commit id stands in.
func TestParseStatusDetached(t *testing.T) {
	var r Repo
	ParseStatus("# branch.oid 1234567890abcdef\n# branch.head (detached)\n", &r)

	if r.Branch != "" {
		t.Errorf("Branch = %q, want none while detached", r.Branch)
	}
	if r.Ref() != "1234567" {
		t.Errorf("Ref = %q, want the short id", r.Ref())
	}
}

// A repository with no commits reports "(initial)" where the object id goes.
func TestParseStatusInitial(t *testing.T) {
	var r Repo
	ParseStatus("# branch.oid (initial)\n# branch.head main\n? first.go\n", &r)

	if r.Head != "" {
		t.Errorf("Head = %q, want none before the first commit", r.Head)
	}
	if r.Ref() != "main" {
		t.Errorf("Ref = %q", r.Ref())
	}
}

// The format is versioned and additive, so a header from a newer git has to
// be ignored rather than throwing the parse.
func TestParseStatusIgnoresWhatItDoesNotKnow(t *testing.T) {
	var r Repo
	ParseStatus("# branch.head main\n# stash 2\n# something.new whatever\n1 .M N... 1 2 3 a b f.go\n", &r)

	if r.Branch != "main" || r.Modified != 1 {
		t.Errorf("unknown headers changed the parse: %+v", r)
	}
}

func TestParseLog(t *testing.T) {
	at, subject := ParseLog("1788103570\x00feat(git): Add the git widget\n")
	if want := time.Unix(1788103570, 0); !at.Equal(want) {
		t.Errorf("time = %s, want %s", at, want)
	}
	if subject != "feat(git): Add the git widget" {
		t.Errorf("subject = %q", subject)
	}

	// A subject may contain anything, which is why the separator is a NUL.
	_, subject = ParseLog("1788103570\x00fix: handle a\tsubject with  odd spacing\n")
	if subject != "fix: handle a\tsubject with  odd spacing" {
		t.Errorf("subject = %q", subject)
	}

	// An empty repository has no commit to describe.
	if at, _ := ParseLog(""); !at.IsZero() {
		t.Errorf("empty log gave %s", at)
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "alpha"))
	mkRepo(t, filepath.Join(root, "work", "beta"))
	mkDir(t, filepath.Join(root, "work", "notes"))
	mkRepo(t, filepath.Join(root, "deep", "deeper", "gamma"))
	// A repository inside a repository is a submodule or a vendored tree,
	// not a separate entry on a dashboard.
	mkRepo(t, filepath.Join(root, "alpha", "vendor", "inner"))
	// Dot directories are caches and configuration, never projects.
	mkRepo(t, filepath.Join(root, ".cache", "delta"))

	found, err := Discover(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "alpha"), filepath.Join(root, "work", "beta")}
	if len(found) != len(want) {
		t.Fatalf("found %v, want %v", found, want)
	}
	for i := range want {
		if found[i] != want[i] {
			t.Errorf("found[%d] = %q, want %q", i, found[i], want[i])
		}
	}

	// Depth is what decides whether the third one is in range.
	deep, err := Discover(root, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(deep) != 3 {
		t.Errorf("at depth 3 found %v, want three repos", deep)
	}
}

func TestDiscoverErrors(t *testing.T) {
	if _, err := Discover(filepath.Join(t.TempDir(), "nope"), 2); err == nil {
		t.Error("a missing root should be an error the widget can show")
	}

	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(file, 2); err == nil {
		t.Error("a file is not a scan root")
	}
}

// The one test that runs git. It reads this repository, which is the only one
// guaranteed to be here, and checks the two commands agree with each other.
func TestStatusOnThisRepository(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(wd)) // internal/repos -> the repo
	if !IsRepo(root) {
		t.Skip("not running inside a git checkout")
	}

	r := Status(context.Background(), root)
	if r.Err != nil {
		t.Fatalf("reading %s: %v", root, r.Err)
	}
	if r.Ref() == "?" {
		t.Error("neither a branch nor a commit id came back")
	}
	if r.Last.IsZero() {
		t.Error("no commit time came back")
	}
	if r.Subject == "" {
		t.Error("no commit subject came back")
	}
	if r.Name != filepath.Base(root) {
		t.Errorf("Name = %q, want %q", r.Name, filepath.Base(root))
	}
}

// A path that is not a repository has to come back as one repo in an error
// state, not as a failed refresh: the other repositories are still fine.
func TestStatusOnSomethingElse(t *testing.T) {
	r := Status(context.Background(), t.TempDir())
	if r.Err == nil {
		t.Fatal("want an error for a directory that is not a repository")
	}
	if strings.TrimSpace(r.Err.Error()) == "" {
		t.Error("the error should say something")
	}
}

func mkRepo(t *testing.T, dir string) {
	t.Helper()
	mkDir(t, filepath.Join(dir, ".git"))
}

func mkDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// The file list comes out of the same read the counts do, so drilling into a
// repository costs no extra command.
func TestParseStatusFiles(t *testing.T) {
	var r Repo
	ParseStatus(busyStatus, &r)

	if len(r.Files) != 7 {
		t.Fatalf("got %d files, want 7: %+v", len(r.Files), r.Files)
	}

	byPath := map[string]File{}
	for _, f := range r.Files {
		byPath[f.Path] = f
	}

	for _, tc := range []struct {
		path                             string
		status                           string
		staged, modified, untracked, bad bool
	}{
		{path: "staged.go", status: "M ", staged: true},
		{path: "modified.go", status: " M", modified: true},
		{path: "both.go", status: "MM", staged: true, modified: true},
		{path: "new.go", status: "R ", staged: true},
		{path: "conflict.go", status: "UU", modified: true, bad: true},
		{path: "notes.txt", status: "??", untracked: true, modified: true},
		{path: "build/", status: "??", untracked: true, modified: true},
	} {
		f, ok := byPath[tc.path]
		if !ok {
			t.Errorf("%s is missing from the file list", tc.path)
			continue
		}
		if f.Status() != tc.status {
			t.Errorf("%s: status = %q, want %q", tc.path, f.Status(), tc.status)
		}
		if f.IsStaged() != tc.staged {
			t.Errorf("%s: IsStaged = %v, want %v", tc.path, f.IsStaged(), tc.staged)
		}
		if f.IsModified() != tc.modified {
			t.Errorf("%s: IsModified = %v, want %v", tc.path, f.IsModified(), tc.modified)
		}
		if f.Untracked() != tc.untracked {
			t.Errorf("%s: Untracked = %v, want %v", tc.path, f.Untracked(), tc.untracked)
		}
		if f.Conflicted() != tc.bad {
			t.Errorf("%s: Conflicted = %v, want %v", tc.path, f.Conflicted(), tc.bad)
		}
	}

	// A rename remembers where it came from.
	if got := byPath["new.go"].Orig; got != "old.go" {
		t.Errorf("rename Orig = %q, want old.go", got)
	}
}

// Paths contain spaces, so the path is everything after the fixed fields
// rather than a field of its own.
func TestParseStatusPathsWithSpaces(t *testing.T) {
	var r Repo
	ParseStatus("1 .M N... 100644 100644 100644 aaa bbb my notes/a file.md\n? another dir/thing.txt\n", &r)

	if len(r.Files) != 2 {
		t.Fatalf("got %+v", r.Files)
	}
	if r.Files[0].Path != "my notes/a file.md" {
		t.Errorf("tracked path = %q", r.Files[0].Path)
	}
	if r.Files[1].Path != "another dir/thing.txt" {
		t.Errorf("untracked path = %q", r.Files[1].Path)
	}
}

// An empty commit message is refused before git is run, so the error names the
// thing the user can do something about.
func TestCommitStagedNeedsAMessage(t *testing.T) {
	err := CommitStaged(context.Background(), t.TempDir(), "   ")
	if err == nil || !strings.Contains(err.Error(), "message") {
		t.Errorf("err = %v, want a complaint about the message", err)
	}
}

func TestParseCommits(t *testing.T) {
	out := "8f1a2b3\x001788103570\x00Karan\x00feat(git): add the widget\n" +
		"7e0b1a2\x001788003570\x00Someone Else\x00fix: a subject with \x1b odd bytes\n" +
		"truncated line with no fields\n"

	commits := ParseCommits(out)
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2: %+v", len(commits), commits)
	}
	if commits[0].Hash != "8f1a2b3" || commits[0].Author != "Karan" {
		t.Errorf("first = %+v", commits[0])
	}
	if commits[0].Subject != "feat(git): add the widget" {
		t.Errorf("subject = %q", commits[0].Subject)
	}
	if want := time.Unix(1788103570, 0); !commits[0].When.Equal(want) {
		t.Errorf("when = %s, want %s", commits[0].When, want)
	}
	if !strings.Contains(commits[1].Subject, "odd bytes") {
		t.Errorf("second subject = %q", commits[1].Subject)
	}
}

// The log is read for the repository the user is looking at, so it has to work
// against a real one.
func TestLogOnThisRepository(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(wd))
	if !IsRepo(root) {
		t.Skip("not running inside a git checkout")
	}

	commits, err := Log(context.Background(), root, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) == 0 {
		t.Fatal("no commits came back")
	}
	for i, c := range commits {
		if c.Hash == "" || c.Subject == "" || c.When.IsZero() {
			t.Errorf("commit %d is incomplete: %+v", i, c)
		}
	}
}

// A limit has to cut somewhere, and cutting a path-sorted list keeps the
// alphabetically-first repositories rather than the ones being worked in.
func TestMostRecent(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	// Named so that path order and recency order disagree completely.
	for i, name := range []string{"alpha", "bravo", "charlie", "delta"} {
		dir := filepath.Join(root, name)
		mkRepo(t, dir)
		// alpha is oldest, delta newest.
		touch(t, filepath.Join(dir, ".git", "HEAD"), now.Add(-time.Duration(4-i)*time.Hour))
	}

	all, err := Discover(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || filepath.Base(all[0]) != "alpha" {
		t.Fatalf("discovery should come back in path order, got %v", all)
	}

	got := MostRecent(all, 2)
	if len(got) != 2 {
		t.Fatalf("got %d paths, want 2", len(got))
	}
	if filepath.Base(got[0]) != "delta" || filepath.Base(got[1]) != "charlie" {
		t.Errorf("got %v, want the two newest, newest first", names(got))
	}

	// A limit that cannot bite leaves the list exactly as it was.
	if same := MostRecent(all, 10); len(same) != 4 || same[0] != all[0] {
		t.Errorf("an unreached limit reordered the list: %v", names(same))
	}
	if none := MostRecent(all, 0); len(none) != 4 {
		t.Errorf("a zero limit should be no limit, got %v", names(none))
	}
}

// The index moves on `git add`, so staging counts as touching a repository
// even when nothing has been committed for months.
func TestTouchedFollowsTheIndex(t *testing.T) {
	dir := t.TempDir()
	mkRepo(t, dir)
	old := time.Now().Add(-90 * 24 * time.Hour)
	touch(t, filepath.Join(dir, ".git", "HEAD"), old)

	if got := Touched(dir); !got.Equal(old) {
		t.Fatalf("Touched = %s, want %s", got, old)
	}

	recent := time.Now().Add(-time.Minute)
	if err := os.WriteFile(filepath.Join(dir, ".git", "index"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(dir, ".git", "index"), recent)

	if got := Touched(dir); !got.Equal(recent) {
		t.Errorf("Touched = %s, want the index's %s", got, recent)
	}
}

// In a worktree or a submodule .git is a file, with nothing inside it to stat.
func TestTouchedOnAGitFile(t *testing.T) {
	dir := t.TempDir()
	when := time.Now().Add(-2 * time.Hour)
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(dir, ".git"), when)

	if got := Touched(dir); !got.Equal(when) {
		t.Errorf("Touched = %s, want %s", got, when)
	}
	if got := Touched(t.TempDir()); !got.IsZero() {
		t.Errorf("a directory that is not a repository gave %s", got)
	}
}

func touch(t *testing.T, path string, when time.Time) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func names(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}
