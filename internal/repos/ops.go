package repos

import (
	"context"
	"errors"
	"strings"
)

// The operations a dashboard can run on a repository.
//
// Every one of these is a single git command that either works or fails with
// a message worth showing. That is the line: staging, committing and stashing
// are one-shot and safe to put a keystroke away, where a rebase or a conflict
// resolution is an interactive session that wants the whole terminal — which
// is what `enter` and lazygit are for (ADR-001).
//
// Nothing here builds a shell string. Paths arrive from git and go back to git
// as separate argv entries after a "--", so a file called "-f" or ";rm" is a
// filename and cannot be anything else.

// Stage adds paths to the index. With no paths it stages everything, including
// untracked files.
func Stage(ctx context.Context, repo string, paths ...string) error {
	if len(paths) == 0 {
		return act(ctx, repo, "add", "--all")
	}
	return act(ctx, repo, append([]string{"add", "--"}, paths...)...)
}

// Unstage takes paths back out of the index, leaving the working tree alone.
// With no paths it unstages everything.
//
// Before the first commit there is no HEAD to restore a file to, so unstaging
// means dropping it from the index entirely — which leaves it on disk as
// untracked, the state it was in. Doing this the other way round is how a
// dashboard greets someone's brand new repository with "could not resolve
// HEAD".
func Unstage(ctx context.Context, repo string, paths ...string) error {
	args := []string{"restore", "--staged"}
	if !hasHEAD(ctx, repo) {
		args = []string{"rm", "--cached", "-r", "--quiet"}
	}
	if len(paths) == 0 {
		args = append(args, "--", ":/")
	} else {
		args = append(args, "--")
		args = append(args, paths...)
	}
	return act(ctx, repo, args...)
}

// hasHEAD reports whether the repository has any commits yet.
func hasHEAD(ctx context.Context, repo string) bool {
	_, err := run(ctx, repo, "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

// CommitStaged records what is staged. An empty message is refused here rather than
// by git, so the error names the thing the user can fix.
func CommitStaged(ctx context.Context, repo, message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("a commit needs a message")
	}
	return act(ctx, repo, "commit", "-m", message)
}

// Stash puts the working tree aside, untracked files included: leaving them
// behind is the surprise that makes people distrust stash.
func Stash(ctx context.Context, repo string) error {
	return act(ctx, repo, "stash", "push", "--include-untracked")
}

// StashPop restores the most recent stash.
func StashPop(ctx context.Context, repo string) error {
	return act(ctx, repo, "stash", "pop")
}

// Fetch updates the remote-tracking refs. It is the one operation here that
// touches the network, and it is deliberately manual: a dashboard that fetched
// on a timer would be making network calls, and possibly asking for a
// passphrase, on its own schedule rather than the user's.
func Fetch(ctx context.Context, repo string) error {
	return act(ctx, repo, "fetch", "--quiet")
}

// act runs one git command for its exit status.
func act(ctx context.Context, repo string, args ...string) error {
	_, err := run(ctx, repo, args...)
	return err
}
