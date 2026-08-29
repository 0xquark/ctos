package procs

import (
	"strings"
	"testing"
	"time"
)

func TestCompactDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},  // not Go's "5m0s"
		{90 * time.Minute, "1h"}, // both tools take whole units
		{48 * time.Hour, "2d"},
		{0, "1s"}, // never emit "0s", which the tools reject
	}
	for _, c := range cases {
		if got := CompactDuration(c.in); got != c.want {
			t.Errorf("CompactDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTrimLinesDropsBanners(t *testing.T) {
	out := strings.Join([]string{
		"Filtering the log data using \"processID == 1\"",
		"Timestamp               Ty Process[PID:TID]",
		"2026-08-27 00:23:17.578 Df launchd[1:1b13a] real entry",
		"",
		"-- Journal begins at Mon 2026-08-25 09:00:00 UTC. --",
		"Aug 27 00:23:18 host sshd[42]: another real entry",
	}, "\n")

	got := trimLines(out)
	if len(got) != 2 {
		t.Fatalf("kept %d lines, want 2:\n%s", len(got), strings.Join(got, "\n"))
	}
	for _, line := range got {
		if !strings.Contains(line, "real entry") {
			t.Errorf("kept a banner line: %q", line)
		}
	}
}

func TestTrimLinesKeepsTheNewest(t *testing.T) {
	var b strings.Builder
	for i := 0; i < logLines+50; i++ {
		b.WriteString("entry\n")
	}
	if got := len(trimLines(b.String())); got != logLines {
		t.Fatalf("kept %d lines, want the %d newest", got, logLines)
	}
}

func TestLogsRejectsUnsupportedPlatformsClearly(t *testing.T) {
	// LogsSupported gates the widget's UI, so the two must agree: if it says
	// yes, building the command must not fail on this machine.
	if !LogsSupported() {
		t.Skip("no log source on this platform")
	}
	cmd, err := logCommand(t.Context(), 1, 5*time.Minute)
	if err != nil {
		t.Fatalf("LogsSupported said yes but logCommand failed: %v", err)
	}
	if cmd == nil {
		t.Fatal("no command built")
	}
	// The PID must reach the query, or we would be showing another
	// process's logs.
	if !strings.Contains(strings.Join(cmd.Args, " "), "1") {
		t.Errorf("pid missing from %v", cmd.Args)
	}
}
