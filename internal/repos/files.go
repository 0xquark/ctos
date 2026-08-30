package repos

import "strings"

// File is one entry in a repository's working tree, as `git status` reports it.
//
// The two status letters are git's own: X is what is staged for the next
// commit and Y is what has changed on top of that, so " M" is edited but not
// staged, "M " is staged and untouched since, and "MM" is both.
type File struct {
	Path string

	// Orig is where a renamed file came from, and "" for everything else.
	Orig string

	// X and Y are the staged and working-tree status letters. Untracked
	// files carry '?' in both, unmerged ones the conflict pair.
	X, Y byte
}

// Untracked reports whether git has never seen this path.
func (f File) Untracked() bool { return f.X == '?' }

// Conflicted reports whether the file is in an unresolved merge.
func (f File) Conflicted() bool { return f.X == 'U' || f.Y == 'U' }

// IsStaged reports whether this file's change is recorded for the next commit.
//
// An untracked file is not staged: git has nothing recorded for it. Neither is
// an unresolved conflict, whose 'U' sits in the same column — `git add` on one
// means "I have resolved this", which is a different thing from staging.
func (f File) IsStaged() bool {
	return !f.Untracked() && !f.Conflicted() && f.X != '.' && f.X != 0
}

// IsModified reports whether the working tree differs from the index.
func (f File) IsModified() bool { return f.Untracked() || (f.Y != '.' && f.Y != 0) }

// Status is the two-letter code, with git's '.' rendered as a space so a
// column of them reads the way `git status --short` does.
func (f File) Status() string {
	return string(dot(f.X)) + string(dot(f.Y))
}

func dot(b byte) byte {
	if b == '.' || b == 0 {
		return ' '
	}
	return b
}

// parseEntry reads one changed, renamed, unmerged or untracked line of
// `git status --porcelain=v2`.
//
// The number of fixed fields before the path differs per entry kind, and the
// path is everything after them — paths contain spaces, so it cannot be taken
// as a field.
func parseEntry(line string) (File, bool) {
	switch line[0] {
	case '?':
		if path := rest(line, 1); path != "" {
			return File{Path: path, X: '?', Y: '?'}, true
		}

	case '1':
		// 1 XY sub mH mI mW hH hI <path>
		if x, y, ok := codes(line); ok {
			if path := rest(line, 8); path != "" {
				return File{Path: path, X: x, Y: y}, true
			}
		}

	case '2':
		// 2 XY sub mH mI mW hH hI X<score> <path>\t<origPath>
		if x, y, ok := codes(line); ok {
			path, orig, _ := strings.Cut(rest(line, 9), "\t")
			if path != "" {
				return File{Path: path, Orig: orig, X: x, Y: y}, true
			}
		}

	case 'u':
		// u XY sub m1 m2 m3 mW h1 h2 h3 <path>
		if x, y, ok := codes(line); ok {
			if path := rest(line, 10); path != "" {
				return File{Path: path, X: x, Y: y}, true
			}
		}
	}
	return File{}, false
}

// codes reads the two status letters, which are always the second field.
func codes(line string) (x, y byte, ok bool) {
	f := strings.Fields(line)
	if len(f) < 2 || len(f[1]) < 2 {
		return 0, 0, false
	}
	return f[1][0], f[1][1], true
}

// rest returns everything after the first n whitespace-separated fields, with
// the separating run of spaces removed but the path itself untouched.
func rest(line string, n int) string {
	for range n {
		line = strings.TrimLeft(line, " ")
		i := strings.IndexByte(line, ' ')
		if i < 0 {
			return ""
		}
		line = line[i:]
	}
	return strings.TrimLeft(line, " ")
}
