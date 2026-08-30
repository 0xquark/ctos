package sysinfo

import (
	"runtime"
	"strconv"
	"strings"
	"time"
)

// uptime is how long the machine has been up. Zero means unknown.
func uptime() time.Duration {
	switch runtime.GOOS {
	case "linux":
		out, err := readFile("/proc/uptime")
		if err != nil {
			return 0
		}
		return parseProcUptime(out)
	case "darwin", "freebsd", "netbsd", "openbsd":
		out, err := run("sysctl", "-n", "kern.boottime")
		if err != nil {
			return 0
		}
		boot := parseBoottime(out)
		if boot.IsZero() {
			return 0
		}
		return time.Since(boot)
	default:
		return 0
	}
}

// parseProcUptime reads /proc/uptime, whose first field is seconds since
// boot: "350735.47 234388.90".
func parseProcUptime(out string) time.Duration {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// parseBoottime reads kern.boottime, which prints a struct rather than a
// number: "{ sec = 1787969520, usec = 835998 } Sat Aug 29 04:12:00 2026".
func parseBoottime(out string) time.Time {
	// Read as a "sec = value" triple rather than by cutting on "sec = ",
	// which also matches inside "usec = ".
	fields := strings.Fields(out)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i] != "sec" || fields[i+1] != "=" {
			continue
		}
		secs, err := strconv.ParseInt(strings.TrimSuffix(fields[i+2], ","), 10, 64)
		if err != nil || secs <= 0 {
			return time.Time{}
		}
		return time.Unix(secs, 0)
	}
	return time.Time{}
}
