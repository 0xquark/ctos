// Package repos reads the state of local git repositories.
//
// It shells out to the system `git` rather than linking a git library, for the
// reasons ADR-004 and ADR-012 give: it keeps the dependency list at four, it
// works with whatever git the user already has configured, and every parser
// here is a pure function over captured text — so the same code will read
// `ssh host git -C repo status` when v0.2 asks for repos across machines.
package repos

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Repo is one repository's state.
type Repo struct {
	// Path is the working tree; Name is what the widget labels it with.
	Path string
	Name string

	// Branch is the checked-out branch, or "" when HEAD is detached.
	// Head is then the short commit id, which is all there is to show.
	Branch   string
	Head     string
	Upstream string

	// Ahead and Behind are commits relative to the upstream branch. Both
	// are zero when there is no upstream to compare against.
	Ahead, Behind int

	// Working tree counts. Staged and Modified count tracked files;
	// Untracked counts what `git status` reports, which collapses an
	// untracked directory into one entry rather than listing it.
	Staged, Modified, Untracked, Conflicts int

	// Files is every entry behind those counts, in the order git listed
	// them. The status read already has them, so drilling into a
	// repository costs no extra command.
	Files []File

	// Last is the commit time of HEAD and Subject its first line. Both are
	// zero in a repository with no commits yet.
	Last    time.Time
	Subject string

	// Err is set when this repository could not be read. One unreadable
	// repo must not blank the rest of the list, so the error travels with
	// it instead of failing the whole refresh.
	Err error
}

// Dirty is the number of working-tree entries that differ from HEAD.
func (r Repo) Dirty() int { return r.Staged + r.Modified + r.Untracked + r.Conflicts }

// Clean reports whether the working tree matches HEAD.
func (r Repo) Clean() bool { return r.Dirty() == 0 }

// Synced reports whether the branch is level with its upstream. A repo with
// no upstream counts as synced: there is nothing it could be behind.
func (r Repo) Synced() bool { return r.Ahead == 0 && r.Behind == 0 }

// Ref is what to label the repo's position with: the branch, or the short
// commit id when HEAD is detached.
func (r Repo) Ref() string {
	if r.Branch != "" {
		return r.Branch
	}
	if r.Head != "" {
		return r.Head
	}
	return "?"
}

// timeout bounds one repository's read. Locally these are instant; the bound
// exists so a repo on a stalled network mount cannot hold up a refresh.
const timeout = 10 * time.Second

// Status reads one repository.
//
// Two commands, because git will not report both in one: the porcelain status
// carries the branch, its distance from upstream and the working-tree counts,
// and the log carries HEAD's age. Both run with --no-optional-locks so that
// polling a repo every few seconds never takes the index lock out from under
// the person working in it.
func Status(ctx context.Context, path string) Repo {
	r := Repo{Path: path, Name: filepath.Base(path)}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := run(ctx, path, "status", "--porcelain=v2", "--branch")
	if err != nil {
		r.Err = err
		return r
	}
	ParseStatus(out, &r)

	// A repository with no commits has no HEAD to describe, which is not
	// an error worth showing: the status above already said everything.
	if out, err := run(ctx, path, "log", "-1", "--format=%ct%x00%s"); err == nil {
		r.Last, r.Subject = ParseLog(out)
	}
	return r
}

// ParseStatus reads `git status --porcelain=v2 --branch` into r.
//
// Unrecognised lines are ignored rather than failing: the format is versioned
// and additive, and a header this code has not heard of is not a reason to
// blank a repository that git described perfectly well.
func ParseStatus(out string, r *Repo) {
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case '#':
			parseHeader(line, r)
		case '1', '2', 'u', '?':
			f, ok := parseEntry(line)
			if !ok {
				continue
			}
			r.Files = append(r.Files, f)
			switch {
			case f.Untracked():
				r.Untracked++
			case f.Conflicted():
				r.Conflicts++
			default:
				// A file can be both: staged changes and
				// further edits on top of them.
				if f.IsStaged() {
					r.Staged++
				}
				if f.IsModified() {
					r.Modified++
				}
			}
		}
	}
}

// parseHeader reads one "# branch.*" line.
func parseHeader(line string, r *Repo) {
	f := strings.Fields(line)
	if len(f) < 3 {
		return
	}
	switch f[1] {
	case "branch.oid":
		// "(initial)" in a repository with no commits.
		if f[2] != "(initial)" && len(f[2]) >= 7 {
			r.Head = f[2][:7]
		}
	case "branch.head":
		// "(detached)" is git's way of saying there is no branch.
		if f[2] != "(detached)" {
			r.Branch = f[2]
		}
	case "branch.upstream":
		r.Upstream = f[2]
	case "branch.ab":
		// "+2 -1". The signs are part of the format, not of the
		// numbers: behind is reported as "-1" but means one commit.
		if len(f) < 4 {
			return
		}
		r.Ahead = countField(f[2], '+')
		r.Behind = countField(f[3], '-')
	}
}

// countField reads "+2" or "-1" as a plain count.
func countField(s string, sign byte) int {
	if len(s) < 2 || s[0] != sign {
		return 0
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// ParseLog reads `git log -1 --format=%ct%x00%s`: a unix timestamp and the
// subject, separated by a NUL so a subject containing anything at all still
// parses.
func ParseLog(out string) (time.Time, string) {
	ts, subject, ok := strings.Cut(strings.TrimRight(out, "\n"), "\x00")
	if !ok {
		return time.Time{}, ""
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64)
	if err != nil {
		return time.Time{}, subject
	}
	return time.Unix(secs, 0), subject
}

// run executes one git command in a repository.
func run(ctx context.Context, path string, args ...string) (string, error) {
	full := append([]string{"--no-optional-locks", "-C", path}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git timed out after %s", timeout)
		}
		return "", explain(path, err)
	}
	return string(out), nil
}

// explain turns git's exit status into something a dashboard can show.
func explain(path string, err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if msg := firstLine(string(ee.Stderr)); msg != "" {
			return errors.New(msg)
		}
		return fmt.Errorf("git exited %d", ee.ExitCode())
	}
	var ne *exec.Error
	if errors.As(err, &ne) {
		return errors.New("git is not installed, or not on $PATH")
	}
	return fmt.Errorf("%s: %w", path, err)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	// git prefixes its diagnostics; the prefix says nothing here.
	return strings.TrimSpace(strings.TrimPrefix(line, "fatal: "))
}
