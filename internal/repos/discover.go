package repos

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Discover finds git repositories under root, descending at most depth levels.
//
// A directory holding .git is a repository and is not descended into: the
// submodules and vendored trees inside one are not separate entries on a
// dashboard. Anything unreadable is skipped rather than failing the scan, so
// one directory without permission does not cost the user the rest.
func Discover(root string, depth int) ([]string, error) {
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s does not exist", root)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	var found []string
	walk(root, max(depth, 0), &found)
	sort.Strings(found)
	return found, nil
}

func walk(dir string, depth int, found *[]string) {
	if IsRepo(dir) {
		*found = append(*found, dir)
		return
	}
	if depth == 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		// A dot directory under a scan root is configuration, a cache
		// or a trash can. None of them is a project the user is
		// working on, and .cache alone can hold hundreds of clones.
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		walk(filepath.Join(dir, e.Name()), depth-1, found)
	}
}

// IsRepo reports whether dir is the root of a working tree. .git is a
// directory in a normal clone and a file in a worktree or a submodule, so the
// check is for its presence rather than its kind.
func IsRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Touched is roughly when someone last did something in a repository.
//
// It is the newest mtime among the files git rewrites as you work: HEAD, which
// moves on a commit, a checkout or a branch switch, and the index, which moves
// on `git add`. Neither is moved by reading — every command in this package
// passes --no-optional-locks precisely so that watching a repository does not
// look like working in one.
//
// It is an approximation, and a cheap one: two stat calls against a commit
// timestamp's two processes. That is the trade it exists to make.
func Touched(dir string) time.Time {
	git := filepath.Join(dir, ".git")

	var newest time.Time
	for _, name := range []string{"HEAD", "index"} {
		if fi, err := os.Stat(filepath.Join(git, name)); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	if !newest.IsZero() {
		return newest
	}
	// In a worktree or a submodule .git is a file, not a directory, and
	// there is nothing inside it to stat.
	if fi, err := os.Stat(git); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

// MostRecent returns the n repositories touched most recently, newest first.
//
// It exists because a limit has to cut somewhere, and cutting a path-sorted
// list keeps the alphabetically-first repositories rather than the ones the
// user has been working in — which is the opposite of what a limit on a
// dashboard is for. Ties break on path, so the result does not shuffle between
// refreshes.
//
// The cut happens before any repository is read, so the limit still bounds the
// expensive work: this is two stat calls per candidate, not two processes.
func MostRecent(paths []string, n int) []string {
	if n <= 0 || len(paths) <= n {
		return paths
	}

	type entry struct {
		path    string
		touched time.Time
	}
	entries := make([]entry, len(paths))
	for i, p := range paths {
		entries[i] = entry{path: p, touched: Touched(p)}
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].touched.Equal(entries[j].touched) {
			return entries[i].touched.After(entries[j].touched)
		}
		return entries[i].path < entries[j].path
	})

	out := make([]string, n)
	for i := range out {
		out[i] = entries[i].path
	}
	return out
}
