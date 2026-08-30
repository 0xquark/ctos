package repos

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Commit is one entry from a repository's history.
type Commit struct {
	Hash    string
	Author  string
	Subject string
	When    time.Time
}

// logFormat asks for the four fields a list of commits needs, NUL-separated so
// a subject or an author name containing anything at all still parses.
const logFormat = "%h%x00%ct%x00%an%x00%s"

// Log reads the last n commits.
//
// It is a third command per repository, so it runs only for the one the user
// is looking at rather than for every repository on every refresh.
func Log(ctx context.Context, path string, n int) ([]Commit, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := run(ctx, path, "log", "-n", strconv.Itoa(max(n, 1)), "--format="+logFormat)
	if err != nil {
		return nil, err
	}
	return ParseCommits(out), nil
}

// ParseCommits reads `git log --format=logFormat` output. A line that does not
// have all four fields is skipped: a truncated final line should not cost the
// user the commits above it.
func ParseCommits(out string) []Commit {
	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\x00", 4)
		if len(f) != 4 {
			continue
		}
		c := Commit{Hash: f[0], Author: f[2], Subject: f[3]}
		if secs, err := strconv.ParseInt(f[1], 10, 64); err == nil {
			c.When = time.Unix(secs, 0)
		}
		commits = append(commits, c)
	}
	return commits
}
