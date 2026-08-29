package procs

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// macOS and Linux ps differ in spacing and in how they render kernel threads,
// so both shapes are pinned here.
const darwinOut = `    1     0 root               0.1  0.1  13904 06-17:52:29 Ss   /sbin/launchd
  253     1 pparker     12.5  3.7 262352 01-23:17:56 S    /Applications/Firefox.app/Contents/MacOS/firefox -foreground
  326     1 root               0.4  0.1  18928 06-17:52:02 Ss   /usr/libexec/logd
`

const linuxOut = `      1       0 root      0.0  0.1   9876    10:23:14 Ss   /sbin/init splash
      2       0 root      0.0  0.0      0    10:23:14 S    [kthreadd]
   4821    1201 pparker    99.9 12.4 812340       05:07 Rl   /usr/lib/firefox/firefox -contentproc
`

func TestParsePSDarwin(t *testing.T) {
	ps, err := ParsePS(darwinOut)
	if err != nil {
		t.Fatalf("ParsePS: %v", err)
	}
	if len(ps) != 3 {
		t.Fatalf("got %d processes, want 3", len(ps))
	}

	ff := ps[1]
	if ff.PID != 253 || ff.PPID != 1 {
		t.Errorf("pid/ppid = %d/%d, want 253/1", ff.PID, ff.PPID)
	}
	if ff.User != "pparker" {
		t.Errorf("user = %q, want pparker", ff.User)
	}
	if ff.CPU != 12.5 || ff.Mem != 3.7 {
		t.Errorf("cpu/mem = %v/%v, want 12.5/3.7", ff.CPU, ff.Mem)
	}
	if want := int64(262352 * 1024); ff.RSS != want {
		t.Errorf("rss = %d, want %d (ps reports KB)", ff.RSS, want)
	}
	if ff.State != "S" {
		t.Errorf("state = %q, want S", ff.State)
	}
	// The command must survive intact, spaces and all.
	if want := "/Applications/Firefox.app/Contents/MacOS/firefox -foreground"; ff.Command != want {
		t.Errorf("command = %q, want %q", ff.Command, want)
	}
	if ff.Name() != "firefox" {
		t.Errorf("name = %q, want firefox", ff.Name())
	}
}

func TestParsePSLinux(t *testing.T) {
	ps, err := ParsePS(linuxOut)
	if err != nil {
		t.Fatalf("ParsePS: %v", err)
	}
	if len(ps) != 3 {
		t.Fatalf("got %d processes, want 3", len(ps))
	}
	if got := ps[1].Name(); got != "[kthreadd]" {
		t.Errorf("kernel thread name = %q, want [kthreadd]", got)
	}
	if ps[2].CPU != 99.9 {
		t.Errorf("cpu = %v, want 99.9", ps[2].CPU)
	}
	if got := ps[2].Name(); got != "firefox" {
		t.Errorf("name = %q, want firefox", got)
	}
}

// A ps that ignores the empty-header syntax prints a header row. It must be
// skipped, not turned into a process with PID 0.
func TestParsePSSkipsHeader(t *testing.T) {
	withHeader := "  PID  PPID USER     %CPU %MEM   RSS     ELAPSED S    COMMAND\n" + linuxOut
	ps, err := ParsePS(withHeader)
	if err != nil {
		t.Fatalf("ParsePS: %v", err)
	}
	if len(ps) != 3 {
		t.Fatalf("got %d processes, want 3 (header should be dropped)", len(ps))
	}
	for _, p := range ps {
		if p.PID == 0 {
			t.Errorf("header leaked in as a process: %+v", p)
		}
	}
}

func TestParsePSEmptyInputIsNotAnError(t *testing.T) {
	ps, err := ParsePS("")
	if err != nil {
		t.Fatalf(`ParsePS("") = %v, want no error`, err)
	}
	if len(ps) != 0 {
		t.Fatalf("got %d processes, want 0", len(ps))
	}
}

func TestParsePSUnparseableInputIsAnError(t *testing.T) {
	if _, err := ParsePS("this is not ps output at all\n"); err == nil {
		t.Fatal("want an error when nothing parses, got nil")
	}
}

func TestParseElapsed(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"05:07", 5*time.Minute + 7*time.Second},
		{"10:23:14", 10*time.Hour + 23*time.Minute + 14*time.Second},
		{"06-17:52:29", 6*24*time.Hour + 17*time.Hour + 52*time.Minute + 29*time.Second},
		{"", 0},
		{"garbage", 0},
	}
	for _, c := range cases {
		if got := parseElapsed(c.in); got != c.want {
			t.Errorf("parseElapsed(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestName(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"/usr/bin/ssh -N host", "ssh"},
		{"[kworker/0:2-events]", "[kworker/0:2-events]"},
		{"-zsh", "zsh"},
		{"", "?"},
		{"/Applications/Some App.app/Contents/MacOS/Some", "Some"},
	}
	for _, c := range cases {
		if got := (Process{Command: c.cmd}).Name(); got != c.want {
			t.Errorf("Name(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

func sampleProcs() []Process {
	return []Process{
		{PID: 3, CPU: 5, Mem: 30, Command: "zsh"},
		{PID: 1, CPU: 90, Mem: 10, Command: "firefox"},
		{PID: 2, CPU: 90, Mem: 20, Command: "atuin"},
	}
}

func TestSortByCPUTiesBreakOnPID(t *testing.T) {
	ps := sampleProcs()
	SortBy(ps, ByCPU, false)
	// Both 90% processes come first, and the tie resolves to the lower PID
	// so the list does not jitter between samples.
	got := []int{ps[0].PID, ps[1].PID, ps[2].PID}
	want := []int{1, 2, 3}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cpu order = %v, want %v", got, want)
		}
	}
}

func TestSortOrders(t *testing.T) {
	cases := []struct {
		by   Sort
		want []int
	}{
		{ByMem, []int{3, 2, 1}},
		{ByPID, []int{1, 2, 3}},
		{ByName, []int{2, 1, 3}}, // atuin, firefox, zsh
	}
	for _, c := range cases {
		ps := sampleProcs()
		SortBy(ps, c.by, false)
		for i, want := range c.want {
			if ps[i].PID != want {
				t.Errorf("sort %v = pid %d at index %d, want %d", c.by, ps[i].PID, i, want)
			}
		}
	}
}

func TestSortCycleWraps(t *testing.T) {
	s := ByCPU
	for i := 0; i < int(sortCount); i++ {
		s = s.Next()
	}
	if s != ByCPU {
		t.Fatalf("cycling %d times landed on %v, want back at cpu", sortCount, s)
	}
}

func TestFilter(t *testing.T) {
	ps := []Process{
		{PID: 100, User: "root", Command: "/usr/sbin/sshd -D"},
		{PID: 200, User: "pparker", Command: "/usr/bin/firefox"},
	}
	cases := []struct {
		query string
		want  int
	}{
		{"", 2},
		{"   ", 2},
		{"FIREFOX", 1}, // case-insensitive
		{"root", 1},    // matches the user column
		{"100", 1},     // matches the PID
		{"sbin", 1},    // matches inside the full path
		{"nope", 0},
	}
	for _, c := range cases {
		if got := len(Filter(ps, c.query)); got != c.want {
			t.Errorf("Filter(%q) kept %d, want %d", c.query, got, c.want)
		}
	}
}

func TestParseLoadFields(t *testing.T) {
	l := parseLoadFields([]string{"1.50", "2.25", "3.00", "1/512", "9"})
	if !l.OK || l.One != 1.5 || l.Five != 2.25 || l.Fifteen != 3 {
		t.Fatalf("parseLoadFields = %+v", l)
	}
	if bad := parseLoadFields([]string{"1.0"}); bad.OK {
		t.Fatal("short input should not report OK")
	}
	if bad := parseLoadFields([]string{"a", "b", "c"}); bad.OK {
		t.Fatal("non-numeric input should not report OK")
	}
}

func TestSortDirectionReverses(t *testing.T) {
	ps := sampleProcs()
	SortBy(ps, ByCPU, true) // ascending, against the default
	if ps[0].CPU != 5 {
		t.Fatalf("reversed cpu sort starts at %v, want the quietest (5)", ps[0].CPU)
	}

	ps = sampleProcs()
	SortBy(ps, ByPID, true) // descending, against the default
	if ps[0].PID != 3 {
		t.Fatalf("reversed pid sort starts at %d, want 3", ps[0].PID)
	}
}

// The header arrow reads off this, so it has to describe what the rows
// actually do rather than which flag was set.
func TestDescendingDescribesTheEffectiveDirection(t *testing.T) {
	cases := []struct {
		by       Sort
		reversed bool
		want     bool
	}{
		{ByCPU, false, true}, // usage defaults to biggest-first
		{ByCPU, true, false},
		{ByMem, false, true},
		{ByPID, false, false}, // identifiers default to smallest-first
		{ByPID, true, true},
		{ByName, false, false},
	}
	for _, c := range cases {
		if got := c.by.Descending(c.reversed); got != c.want {
			t.Errorf("%v.Descending(%v) = %v, want %v", c.by, c.reversed, got, c.want)
		}
	}
}

// Reversing must not break the tie-break, or the list jitters between samples.
func TestReversedSortStillBreaksTiesStably(t *testing.T) {
	tied := []Process{
		{PID: 3, CPU: 7, Command: "c"},
		{PID: 1, CPU: 7, Command: "a"},
		{PID: 2, CPU: 7, Command: "b"},
	}
	for _, reversed := range []bool{false, true} {
		ps := append([]Process(nil), tied...)
		SortBy(ps, ByCPU, reversed)
		if ps[0].PID != 1 || ps[1].PID != 2 || ps[2].PID != 3 {
			t.Errorf("reversed=%v gave %v, want ties broken on ascending pid", reversed, pids(ps))
		}
	}
}

// macOS application bundles put spaces in the executable name, which is the
// one case where "first word of the command" is the wrong answer.
func TestNameHandlesMacOSBundles(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"/Applications/Arc.app/Contents/MacOS/Arc", "Arc"},
		{"/Applications/Arc.app/Contents/Frameworks/ArcCore.framework/Helpers/Browser Helper (Renderer).app/Contents/MacOS/Browser Helper (Renderer) --type=renderer --enable-x", "Browser Helper (Renderer)"},
		{"/Applications/Spotify.app/Contents/MacOS/Spotify", "Spotify"},
		// Still right for everything that is not a bundle.
		{"/usr/bin/ssh -N host", "ssh"},
		{"go test ./internal/...", "go"},
	}
	for _, c := range cases {
		if got := (Process{Command: c.cmd}).Name(); got != c.want {
			t.Errorf("Name(%q)\n = %q\nwant %q", c.cmd, got, c.want)
		}
	}
}

// ps writes its complaint to stderr and exits 1, so the raw error is the
// useless "exit status 1". Everything the user could act on is in stderr.
func TestExplainPSFailure(t *testing.T) {
	busybox := &exec.ExitError{Stderr: []byte(
		"ps: bad -o argument 'pcpu', supported arguments: user,group,comm,args,pid,ppid,pgid,etime,nice,rgroup,ruser,time,tty,vsz,sid,stat,rss")}
	got := explainPSFailure(busybox).Error()
	if !strings.Contains(got, "BusyBox") || !strings.Contains(got, "procps") {
		t.Errorf("BusyBox failure not explained: %q", got)
	}

	other := &exec.ExitError{Stderr: []byte("ps: something else went wrong\nsecond line")}
	got = explainPSFailure(other).Error()
	if !strings.Contains(got, "something else went wrong") {
		t.Errorf("stderr not surfaced: %q", got)
	}
	if strings.Contains(got, "second line") {
		t.Errorf("error should be one line, got %q", got)
	}

	got = explainPSFailure(errors.New("exec: \"ps\": executable file not found in $PATH")).Error()
	if !strings.Contains(got, "not found") {
		t.Errorf("plain error lost: %q", got)
	}
}
