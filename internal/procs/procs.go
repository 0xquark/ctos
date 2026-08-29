// Package procs samples the local process table and the system load.
//
// It shells out to the system `ps` rather than reading kernel structures, for
// the same reason ctOS shells out to `ssh`: `ps` is present everywhere that
// matters, its POSIX output is stable, and it keeps the dependency list at
// four modules (ADR-009). The same parser will serve remote hosts once the
// Runner interface lands, since `ssh host ps ...` returns the identical text.
package procs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Process is one entry in the process table.
type Process struct {
	PID     int
	PPID    int
	User    string
	CPU     float64       // percent of one core, as ps reports it
	Mem     float64       // percent of physical memory
	RSS     int64         // resident set size in bytes
	Elapsed time.Duration // wall-clock time since the process started
	State   string        // ps state code, e.g. "S", "Ss", "R+"
	Command string        // full command line
}

// bundleExec is where a macOS application bundle keeps its executable. The
// name after it routinely contains spaces ("Browser Helper (Renderer)"), which
// is the one case where splitting a command on its first space is wrong.
const bundleExec = "/Contents/MacOS/"

// Name is the short program name: what the user recognises in a narrow column.
//
// The general rule is "basename of the first word", which is right for
// /usr/bin/ssh and for `go test ./...`. macOS bundles break it, so they are
// handled first: everything after the last slash, up to the first argument.
func (p Process) Name() string {
	cmd := strings.TrimSpace(p.Command)
	if cmd == "" {
		return "?"
	}

	// Bracketed kernel threads on Linux have no path to trim.
	if strings.HasPrefix(cmd, "[") {
		if end := strings.Index(cmd, "]"); end > 0 {
			return cmd[:end+1]
		}
	}

	if i := strings.LastIndex(cmd, bundleExec); i >= 0 {
		name := cmd[i+len(bundleExec):]
		// Arguments conventionally start with a dash, which is the only
		// separator available once spaces are part of the name.
		if j := strings.Index(name, " -"); j > 0 {
			name = name[:j]
		}
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}

	first := cmd
	if i := strings.IndexByte(cmd, ' '); i > 0 {
		first = cmd[:i]
	}
	return strings.TrimPrefix(filepath.Base(first), "-")
}

// Sort orders processes in place.
type Sort int

// The available sort orders, in the order the "s" key cycles through them.
const (
	ByCPU Sort = iota
	ByMem
	ByPID
	ByName
	sortCount
)

// String names the sort order for the widget header.
func (s Sort) String() string {
	switch s {
	case ByMem:
		return "mem"
	case ByPID:
		return "pid"
	case ByName:
		return "name"
	default:
		return "cpu"
	}
}

// Next returns the following sort order, wrapping around.
func (s Sort) Next() Sort { return (s + 1) % sortCount }

// DefaultDescending reports which way this order runs before the user
// reverses it. Usage sorts biggest-first, because the interesting processes
// are the greedy ones; identifiers sort smallest-first, the way you would read
// them in a list.
func (s Sort) DefaultDescending() bool { return s == ByCPU || s == ByMem }

// Descending reports the effective direction, which is what the column header
// arrow shows.
func (s Sort) Descending(reversed bool) bool { return s.DefaultDescending() != reversed }

// SortBy orders ps in place. Ties always break on ascending PID, so the list
// never jitters between two samples that agree on the sort key.
func SortBy(ps []Process, by Sort, reversed bool) {
	desc := by.Descending(reversed)

	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		switch by {
		case ByMem:
			if a.Mem != b.Mem {
				return (a.Mem > b.Mem) == desc
			}
		case ByPID:
			if a.PID != b.PID {
				return (a.PID < b.PID) != desc
			}
		case ByName:
			an, bn := strings.ToLower(a.Name()), strings.ToLower(b.Name())
			if an != bn {
				return (an < bn) != desc
			}
		default:
			if a.CPU != b.CPU {
				return (a.CPU > b.CPU) == desc
			}
		}
		return a.PID < b.PID
	})
}

// Filter keeps processes whose name, full command line, or PID contains the
// query. Matching is case-insensitive. An empty query keeps everything.
func Filter(ps []Process, query string) []Process {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return ps
	}
	out := make([]Process, 0, len(ps))
	for _, p := range ps {
		if strings.Contains(strings.ToLower(p.Command), q) ||
			strings.Contains(strings.ToLower(p.User), q) ||
			strings.Contains(itoa(p.PID), q) {
			out = append(out, p)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Signal is how firmly we ask a process to stop.
type Signal int

// Term asks politely; Kill does not ask.
const (
	Term Signal = iota
	Kill
)

// String names the signal for the confirmation prompt.
func (s Signal) String() string {
	if s == Kill {
		return "SIGKILL"
	}
	return "SIGTERM"
}

// Send delivers sig to pid. A process that has already exited is not an
// error: the caller wanted it gone, and it is gone.
func Send(pid int, sig Signal) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	s := syscall.SIGTERM
	if sig == Kill {
		s = syscall.SIGKILL
	}
	if err := p.Signal(s); err != nil {
		if err == os.ErrProcessDone {
			return nil
		}
		return err
	}
	return nil
}
