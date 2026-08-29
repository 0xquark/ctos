package procs

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Load is the 1, 5 and 15 minute run-queue averages.
type Load struct {
	One, Five, Fifteen float64
	OK                 bool // false when this platform gave us nothing
}

// LoadAverage reads the system load. It never returns an error: a missing
// load average is a cosmetic loss in the widget header, not a failure worth
// blanking the process list for.
func LoadAverage() Load {
	switch runtime.GOOS {
	case "linux":
		return loadFromProc()
	case "darwin", "freebsd", "netbsd", "openbsd":
		return loadFromSysctl()
	default:
		return Load{}
	}
}

// loadFromProc reads /proc/loadavg: "0.34 0.41 0.39 1/512 12345".
func loadFromProc() Load {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return Load{}
	}
	return parseLoadFields(strings.Fields(string(b)))
}

// loadFromSysctl reads vm.loadavg, which prints "{ 1.83 2.05 2.21 }".
func loadFromSysctl() Load {
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return Load{}
	}
	fields := strings.Fields(strings.NewReplacer("{", "", "}", "").Replace(string(out)))
	return parseLoadFields(fields)
}

func parseLoadFields(fields []string) Load {
	if len(fields) < 3 {
		return Load{}
	}
	one, err1 := strconv.ParseFloat(fields[0], 64)
	five, err2 := strconv.ParseFloat(fields[1], 64)
	fifteen, err3 := strconv.ParseFloat(fields[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return Load{}
	}
	return Load{One: one, Five: five, Fifteen: fifteen, OK: true}
}

// CPUs is the number of logical cores, used to judge whether a load average
// is healthy. It falls back to 1 so callers can always divide by it.
func CPUs() int {
	if n := runtime.NumCPU(); n > 0 {
		return n
	}
	return 1
}
