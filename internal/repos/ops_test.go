package repos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These operations write, so they are exercised against a repository created
// for the test and thrown away with it. This is the only test in the package
// that changes anything.
func TestOperationsOnARealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := newRepo(t)
	ctx := context.Background()

	write(t, dir, "a.txt", "one\n")
	write(t, dir, "b.txt", "two\n")

	// Untracked to begin with, and neither file staged.
	r := Status(ctx, dir)
	if r.Untracked != 2 || r.Staged != 0 {
		t.Fatalf("fresh repo: untracked=%d staged=%d (%+v)", r.Untracked, r.Staged, r.Files)
	}

	// Stage one file by name.
	if err := Stage(ctx, dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	r = Status(ctx, dir)
	if r.Staged != 1 || r.Untracked != 1 {
		t.Fatalf("after staging a.txt: staged=%d untracked=%d", r.Staged, r.Untracked)
	}
	if f, ok := find(r, "a.txt"); !ok || !f.IsStaged() {
		t.Errorf("a.txt should be staged, got %+v", f)
	}

	// And take it back out.
	if err := Unstage(ctx, dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if r := Status(ctx, dir); r.Staged != 0 || r.Untracked != 2 {
		t.Fatalf("after unstaging: staged=%d untracked=%d", r.Staged, r.Untracked)
	}

	// Stage everything, including what git has never seen.
	if err := Stage(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if r := Status(ctx, dir); r.Staged != 2 || r.Untracked != 0 {
		t.Fatalf("after staging all: staged=%d untracked=%d", r.Staged, r.Untracked)
	}

	// Commit clears the working tree and gives the repo a HEAD.
	if err := CommitStaged(ctx, dir, "first commit"); err != nil {
		t.Fatal(err)
	}
	r = Status(ctx, dir)
	if !r.Clean() {
		t.Fatalf("after commit the tree should be clean, got %+v", r.Files)
	}
	if r.Subject != "first commit" {
		t.Errorf("Subject = %q", r.Subject)
	}
	if r.Last.IsZero() {
		t.Error("no commit time after committing")
	}

	// Committing nothing is an error the user can act on, not a silent
	// empty commit.
	if err := CommitStaged(ctx, dir, "nothing staged"); err == nil {
		t.Error("committing an empty index should fail")
	}

	// Stash takes untracked files with it, which is the behaviour people
	// expect and git does not give by default.
	write(t, dir, "a.txt", "one changed\n")
	write(t, dir, "c.txt", "three\n")
	if err := Stash(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if r := Status(ctx, dir); !r.Clean() {
		t.Errorf("stash should have cleared the tree, got %+v", r.Files)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.txt")); err == nil {
		t.Error("an untracked file survived the stash")
	}

	// And pop puts both back.
	if err := StashPop(ctx, dir); err != nil {
		t.Fatal(err)
	}
	r = Status(ctx, dir)
	if r.Modified != 1 || r.Untracked != 1 {
		t.Errorf("after pop: modified=%d untracked=%d (%+v)", r.Modified, r.Untracked, r.Files)
	}
}

// A path handed back to git is an argv entry after "--", so a file named like
// a flag is a file.
func TestStagingAFileNamedLikeAFlag(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := newRepo(t)
	write(t, dir, "--force", "not a flag\n")

	if err := Stage(context.Background(), dir, "--force"); err != nil {
		t.Fatal(err)
	}
	r := Status(context.Background(), dir)
	if f, ok := find(r, "--force"); !ok || !f.IsStaged() {
		t.Errorf("the file should be staged, got %+v", r.Files)
	}
}

// A failing operation carries git's own first line, which is the sentence that
// tells the user what to do.
func TestOperationErrorsCarryGitsMessage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	err := StashPop(context.Background(), newRepo(t))
	if err == nil {
		t.Fatal("popping an empty stash should fail")
	}
	if strings.Contains(err.Error(), "exited") {
		t.Errorf("err = %q, want git's own message", err)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "user.name", "ctOS test"},
		// Signing is a machine setting, and a test must not depend on
		// the developer's key being available.
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(r Repo, path string) (File, bool) {
	for _, f := range r.Files {
		if f.Path == path {
			return f, true
		}
	}
	return File{}, false
}
