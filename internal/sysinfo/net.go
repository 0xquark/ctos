package sysinfo

import (
	"runtime"
	"strconv"
	"strings"
	"time"
)

// netCounters is the total bytes an interface has carried since boot.
type netCounters struct {
	rx, tx int64
	ok     bool
}

// loopback interfaces are excluded from the "every interface" sum: a machine
// talking to itself is not network throughput, and on a busy host lo dwarfs
// the real traffic.
func isLoopback(name string) bool {
	return name == "lo" || strings.HasPrefix(name, "lo") && strings.Trim(name[2:], "0123456789") == ""
}

// net reads throughput since the previous sample.
//
// The counters are cumulative, so the first sample establishes a baseline and
// reports nothing; every later one divides the difference by the elapsed time.
func (s *Sampler) net() Net {
	cur := readNetCounters(s.iface)
	now := time.Now()
	prev, at := s.prevNet, s.netAt
	s.prevNet, s.netAt = cur, now

	if at.IsZero() {
		return Net{}
	}
	return rateFrom(prev, cur, now.Sub(at))
}

// rateFrom turns two counter readings into bytes per second.
//
// A counter that went backwards means the interface was reset or renumbered,
// and a gap of no time at all would divide by zero. Both report nothing and
// let the next sample re-establish the baseline, rather than drawing a spike.
func rateFrom(prev, cur netCounters, elapsed time.Duration) Net {
	secs := elapsed.Seconds()
	if !prev.ok || !cur.ok || secs <= 0 || cur.rx < prev.rx || cur.tx < prev.tx {
		return Net{}
	}
	return Net{
		Rx: float64(cur.rx-prev.rx) / secs,
		Tx: float64(cur.tx-prev.tx) / secs,
		OK: true,
	}
}

func readNetCounters(iface string) netCounters {
	switch runtime.GOOS {
	case "linux":
		out, err := readFile("/proc/net/dev")
		if err != nil {
			return netCounters{}
		}
		return parseProcNetDev(out, iface)
	case "darwin", "freebsd", "netbsd", "openbsd":
		// "-i" is per-interface, "-b" adds the byte columns and "-n"
		// skips the name lookups that would otherwise make this the
		// slowest read in the sample.
		out, err := run("netstat", "-ibn")
		if err != nil {
			return netCounters{}
		}
		return parseNetstat(out, iface)
	default:
		return netCounters{}
	}
}

// parseProcNetDev reads /proc/net/dev:
//
//	Inter-|   Receive                    |  Transmit
//	 face |bytes    packets errs drop ... |bytes    packets errs ...
//	    lo: 1875297 241685    0    0 ...  1875297   241685    0 ...
//	  eth0: 9382013  84512    0    0 ...  1204831    41023    0 ...
//
// Received bytes are the first counter after the interface name and
// transmitted bytes the ninth, a layout fixed since Linux 2.0.
func parseProcNetDev(out string, want string) netCounters {
	var total netCounters
	for _, line := range strings.Split(out, "\n") {
		name, rest, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		if !selected(name, want) {
			continue
		}

		rx, err1 := strconv.ParseInt(fields[0], 10, 64)
		tx, err2 := strconv.ParseInt(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		total.rx += rx
		total.tx += tx
		total.ok = true
	}
	return total
}

// parseNetstat reads `netstat -ibn`:
//
//	Name  Mtu   Network     Address            Ipkts Ierrs  Ibytes Opkts Oerrs  Obytes Coll
//	lo0   16384 <Link#1>                      241685     0 18752974 241685    0 18752974    0
//	en0   1500  <Link#7>    6a:2a:63:e2:fd:e0 903812     0 93820134 415023    0 12048319    0
//	en0   1500  192.168.1   192.168.1.42      903812     - 93820134 415023    -  12048319    -
//
// Only the "<Link#n>" rows are read. netstat repeats an interface's counters
// once per address family, so summing every row would count a dual-stack
// interface two or three times over; the link row is the one that appears
// exactly once. The Address column is empty on some interfaces and present on
// others, so the seven counters are taken from the end of the line.
func parseNetstat(out string, want string) netCounters {
	const trailing = 7 // Ipkts Ierrs Ibytes Opkts Oerrs Obytes Coll

	var total netCounters
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < trailing+2 || !strings.HasPrefix(fields[2], "<Link#") {
			continue
		}
		// A "*" suffix marks an interface that is down.
		name := strings.TrimSuffix(fields[0], "*")
		if !selected(name, want) {
			continue
		}

		counters := fields[len(fields)-trailing:]
		rx, err1 := strconv.ParseInt(counters[2], 10, 64)
		tx, err2 := strconv.ParseInt(counters[5], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		total.rx += rx
		total.tx += tx
		total.ok = true
	}
	return total
}

// selected reports whether an interface belongs in the total: the named one,
// or every non-loopback interface when no name was configured.
func selected(name, want string) bool {
	if want != "" {
		return name == want
	}
	return !isLoopback(name)
}
