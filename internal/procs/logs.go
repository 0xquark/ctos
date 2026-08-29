package procs

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// LogTimeout bounds a log query. macOS `log show` scans a binary store and is
// genuinely slow, so this is far longer than the process sample's budget.
const LogTimeout = 20 * time.Second

// logLines caps what we keep. A long-running process can emit thousands of lines a
// minute and the pane shows a couple of dozen.
const logLines = 500

// LogsSupported reports whether this platform has a log source we know how to
// query, so the widget can say so up front rather than after a failed run.
func LogsSupported() bool {
	switch runtime.GOOS {
	case "darwin":
		return true
	case "linux":
		return journalctlPath() != ""
	default:
		return false
	}
}

func journalctlPath() string {
	path, err := exec.LookPath("journalctl")
	if err != nil {
		return ""
	}
	return path
}

// Logs returns recent log lines emitted by pid, newest last.
//
// The two supported systems answer very different questions: macOS unified
// logging indexes by process ID and can return entries for any running
// process, while journald indexes by the PID that submitted the entry, so a
// process that logs to stdout under a service manager may show nothing.
// Neither is a substitute for the process's own log file, and the widget says
// so when the result is empty.
func Logs(ctx context.Context, pid int, window time.Duration) ([]string, error) {
	cmd, err := logCommand(ctx, pid, window)
	if err != nil {
		return nil, err
	}

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("log query timed out after %s", LogTimeout)
		}
		// The tools write their complaint to stderr; surfacing it beats a
		// bare "exit status 1".
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr != "" {
			return nil, fmt.Errorf("%s", firstLine(stderr))
		}
		return nil, fmt.Errorf("read logs: %w", err)
	}

	return trimLines(string(out)), nil
}

func logCommand(ctx context.Context, pid int, window time.Duration) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		// --style compact drops the verbose per-line metadata that would
		// leave no room for the message itself in a dashboard pane.
		return exec.CommandContext(ctx, "log", "show",
			"--predicate", "processID == "+strconv.Itoa(pid),
			"--last", CompactDuration(window),
			"--style", "compact",
		), nil

	case "linux":
		if journalctlPath() == "" {
			return nil, fmt.Errorf("no journalctl on this system, so there is no log source to query")
		}
		return exec.CommandContext(ctx, "journalctl",
			"_PID="+strconv.Itoa(pid),
			"--since", "-"+CompactDuration(window),
			"--lines", strconv.Itoa(logLines),
			"--no-pager",
			"--output", "short-precise",
		), nil

	default:
		return nil, fmt.Errorf("reading process logs is not supported on %s", runtime.GOOS)
	}
}

// CompactDuration renders a window the way both `log show --last` and
// `journalctl --since` want it: "30m", "2h", "1d". Go's own Duration.String
// would say "5m0s", which is neither what the tools accept nor what anyone
// wants to read in a pane header.
func CompactDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	case d >= time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d >= time.Minute:
		return strconv.Itoa(int(d.Minutes())) + "m"
	default:
		return strconv.Itoa(max(1, int(d.Seconds()))) + "s"
	}
}

// trimLines drops the tools' banner lines and keeps the tail.
func trimLines(out string) []string {
	var keep []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Both tools print a header before the entries; neither is an entry.
		if strings.HasPrefix(line, "Filtering the log data using") ||
			strings.HasPrefix(line, "Timestamp ") ||
			strings.HasPrefix(line, "-- Journal begins at") ||
			strings.HasPrefix(line, "-- No entries --") {
			continue
		}
		keep = append(keep, line)
	}
	if len(keep) > logLines {
		keep = keep[len(keep)-logLines:]
	}
	return keep
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
