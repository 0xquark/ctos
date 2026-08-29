package procs

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// psArgs asks for one process per line with no header. Every field is
// whitespace-free except args, which is last so the rest of the line is the
// command. "-A" (rather than "-e") is the spelling BSD and GNU ps agree on.
var psArgs = []string{"-Ao", "pid=,ppid=,user=,pcpu=,pmem=,rss=,etime=,state=,args="}

// sampleTimeout bounds a single ps run. Locally this is instant; the bound
// exists so a wedged process table cannot stall a refresh forever.
const sampleTimeout = 10 * time.Second

// Sample reads the current process table.
func Sample() ([]Process, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("the processes widget needs a POSIX `ps`, which %s does not provide", runtime.GOOS)
	}

	ctx, cancel := context.WithTimeout(context.Background(), sampleTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "ps", psArgs...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ps timed out after %s", sampleTimeout)
		}
		return nil, explainPSFailure(err)
	}
	return ParsePS(string(out))
}

// ParsePS turns `ps -Ao pid=,ppid=,user=,pcpu=,pmem=,rss=,etime=,state=,args=`
// output into processes. It is exported so tests, and later the SSH runner,
// can feed it captured output.
//
// Malformed lines are skipped rather than failing the whole sample: a header
// line from a ps that ignored the empty-header syntax, or a truncated final
// line, should not blank the widget.
func ParsePS(out string) ([]Process, error) {
	var ps []Process
	for _, line := range strings.Split(out, "\n") {
		p, ok := parseLine(line)
		if ok {
			ps = append(ps, p)
		}
	}
	if len(ps) == 0 && strings.TrimSpace(out) != "" {
		return nil, fmt.Errorf("could not parse any process from ps output")
	}
	return ps, nil
}

// parseLine reads the eight fixed columns, then takes the remainder of the
// line as the command.
func parseLine(line string) (Process, bool) {
	fields, rest := splitN(line, 8)
	if len(fields) < 8 || rest == "" {
		return Process{}, false
	}

	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return Process{}, false // header line, or garbage
	}
	ppid, _ := strconv.Atoi(fields[1])
	cpu, _ := strconv.ParseFloat(fields[3], 64)
	mem, _ := strconv.ParseFloat(fields[4], 64)
	rssKB, _ := strconv.ParseInt(fields[5], 10, 64)

	return Process{
		PID:     pid,
		PPID:    ppid,
		User:    fields[2],
		CPU:     cpu,
		Mem:     mem,
		RSS:     rssKB * 1024,
		Elapsed: parseElapsed(fields[6]),
		State:   fields[7],
		Command: rest,
	}, true
}

// splitN pulls the first n whitespace-separated fields off line and returns
// them along with the untouched remainder.
func splitN(line string, n int) (fields []string, rest string) {
	i := 0
	for len(fields) < n {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		start := i
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		if start == i {
			return fields, ""
		}
		fields = append(fields, line[start:i])
	}
	return fields, strings.TrimSpace(line[i:])
}

// parseElapsed reads ps etime, which is "[[dd-]hh:]mm:ss".
func parseElapsed(s string) time.Duration {
	days := 0
	if dash := strings.IndexByte(s, '-'); dash > 0 {
		days, _ = strconv.Atoi(s[:dash])
		s = s[dash+1:]
	}

	parts := strings.Split(s, ":")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0
		}
		nums[i] = n
	}

	var h, m, sec int
	switch len(nums) {
	case 3:
		h, m, sec = nums[0], nums[1], nums[2]
	case 2:
		m, sec = nums[0], nums[1]
	case 1:
		sec = nums[0]
	default:
		return 0
	}

	return time.Duration(days)*24*time.Hour +
		time.Duration(h)*time.Hour +
		time.Duration(m)*time.Minute +
		time.Duration(sec)*time.Second
}

// explainPSFailure turns a failed ps run into something the user can act on.
// ps writes its complaint to stderr and exits 1, so the default error would be
// the useless "exit status 1".
func explainPSFailure(err error) error {
	var stderr string
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		stderr = strings.TrimSpace(string(ee.Stderr))
	}

	// BusyBox ps accepts -o but knows no %CPU or %MEM columns, so a process
	// table is simply not available until procps is installed. This is the
	// common case on Alpine.
	if strings.Contains(stderr, "bad -o argument") {
		return fmt.Errorf("this system's ps is BusyBox, which cannot report %%CPU or %%MEM. Install procps (on Alpine: apk add procps)")
	}
	if stderr != "" {
		return fmt.Errorf("run ps: %s", firstLine(stderr))
	}
	return fmt.Errorf("run ps: %w", err)
}
