package sysinfo

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// cpuWarmup is how long the first Linux sample waits between its two readings
// of /proc/stat. A busy percentage is a difference between two totals, so the
// first reading has nothing to subtract from; a quarter second is long enough
// to be a real measurement and short enough that nobody sees the widget
// pause. Every later sample differences against the previous tick instead,
// and so costs nothing.
const cpuWarmup = 250 * time.Millisecond

// cpuTimes is a reading of the cumulative time the CPUs have spent working
// and idling since boot, in whatever unit the platform counts in. Only the
// ratio between two readings is used, so the unit never matters.
type cpuTimes struct {
	busy, user, system, total float64
	ok                        bool
}

// cpu reads processor utilisation.
//
// The two platforms need genuinely different treatment. Linux publishes
// cumulative counters in /proc/stat, so a delta against the previous tick is
// exact and free. macOS publishes no such counter to userspace — kern.cp_time
// is a BSD-ism Darwin does not carry, and `ps` offers only a per-process
// decaying average that sums to well under the true figure — so the reading
// comes from iostat, which measures a real interval for us at the cost of
// taking a second to answer (ADR-024).
func (s *Sampler) cpu() CPU {
	switch runtime.GOOS {
	case "linux":
		return s.cpuFromProcStat()
	case "darwin", "freebsd", "netbsd", "openbsd":
		return s.cpuFromIostat()
	default:
		return CPU{}
	}
}

func (s *Sampler) cpuFromProcStat() CPU {
	cur, err := readProcStat()
	if err != nil {
		return CPU{}
	}

	if !s.prevCPU.ok {
		s.prevCPU = cur
		time.Sleep(cpuWarmup)
		if cur, err = readProcStat(); err != nil {
			return CPU{}
		}
	}

	c := cpuDelta(s.prevCPU, cur)
	s.prevCPU = cur
	return c
}

func readProcStat() (cpuTimes, error) {
	b, err := readFile("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	return parseProcStat(b)
}

// parseProcStat reads the aggregate "cpu" line of /proc/stat:
//
//	cpu  2255 34 2290 22625563 6290 127 456 0 0 0
//
// The fields are user, nice, system, idle, iowait, irq, softirq, steal, and
// two guest counters. Guest time is already counted inside user, so summing
// every field would count it twice; the two are dropped.
func parseProcStat(out string) (cpuTimes, error) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}

		var vals []float64
		for _, f := range fields[1:min(len(fields), 9)] {
			v, err := strconv.ParseFloat(f, 64)
			if err != nil {
				return cpuTimes{}, fmt.Errorf("/proc/stat: unreadable cpu field %q", f)
			}
			vals = append(vals, v)
		}

		var total float64
		for _, v := range vals {
			total += v
		}
		// idle and iowait are both time the CPU had nothing to run.
		idle := vals[3]
		if len(vals) > 4 {
			idle += vals[4]
		}

		t := cpuTimes{user: vals[0] + vals[1], system: vals[2], total: total, ok: true}
		t.busy = total - idle
		return t, nil
	}
	return cpuTimes{}, fmt.Errorf("/proc/stat: no aggregate cpu line")
}

// cpuDelta turns two readings into the percentage busy over the gap between
// them. A zero or negative gap means the counters did not move — or were
// reset underneath us — and reports nothing rather than a spike.
func cpuDelta(prev, cur cpuTimes) CPU {
	total := cur.total - prev.total
	if !prev.ok || !cur.ok || total <= 0 {
		return CPU{}
	}
	pct := func(a, b float64) float64 { return clampPercent((a - b) / total * 100) }
	return CPU{
		Busy:   pct(cur.busy, prev.busy),
		User:   pct(cur.user, prev.user),
		System: pct(cur.system, prev.system),
		OK:     true,
	}
}

// iostatArgs asks for two samples a second apart, across up to eight disks.
// The first sample covers the time since boot and is discarded; the second is
// the one-second interval we want.
//
// The per-disk columns are kept rather than dropped with "-n 0", because they
// are the only storage throughput macOS publishes without root — so the one
// second this call already costs pays for two metrics instead of one.
var iostatArgs = []string{"-c", "2", "-w", "1", "-n", "8"}

func (s *Sampler) cpuFromIostat() CPU {
	out, err := run("iostat", iostatArgs...)
	if err != nil {
		return CPU{}
	}
	cpu, io := parseIostat(out)
	s.lastDisk = io
	return cpu
}

// parseIostat reads the last sample of iostat's columns:
//
//	           disk0       cpu    load average
//	 KB/t  tps  MB/s  us sy id   1m   5m   15m
//	23.04  165  3.72  11  6 83  2.99 3.10 3.29
//	15.61 2849 43.43   7  4 89  2.99 3.10 3.29
//
// Everything is located by its header rather than by position, because the
// disk columns repeat once per attached disk and would otherwise shift the CPU
// reading by three columns per extra disk.
//
// The "MB/s" columns are summed across disks. iostat does not separate reads
// from writes, so the result is a combined figure and says so.
func parseIostat(out string) (CPU, DiskIO) {
	var cols map[string]int
	var mbps []int
	var last []string

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if idx, rates, ok := iostatColumns(fields); ok {
			cols, mbps, last = idx, rates, nil
			continue
		}
		if cols != nil {
			last = fields
		}
	}

	if cols == nil || last == nil {
		return CPU{}, DiskIO{}
	}
	user, okU := field(last, cols["us"])
	sys, okS := field(last, cols["sy"])
	idle, okI := field(last, cols["id"])
	if !okU || !okS || !okI {
		return CPU{}, DiskIO{}
	}

	// Busy is derived from idle rather than from user plus system, so that
	// a nice or iowait column this iostat reports separately is not lost.
	cpu := CPU{
		Busy:   clampPercent(100 - idle),
		User:   clampPercent(user),
		System: clampPercent(sys),
		OK:     true,
	}

	var io DiskIO
	const mib = 1 << 20
	for _, i := range mbps {
		if v, ok := field(last, i); ok {
			io.Total += v * mib
			io.OK = true
		}
	}
	return cpu, io
}

// iostatColumns recognises the header row, indexing the three CPU columns and
// every per-disk throughput column.
func iostatColumns(fields []string) (cpu map[string]int, mbps []int, ok bool) {
	cpu = map[string]int{}
	for i, f := range fields {
		switch f {
		case "us", "sy", "id":
			cpu[f] = i
		case "MB/s":
			mbps = append(mbps, i)
		}
	}
	if len(cpu) != 3 {
		return nil, nil, false
	}
	return cpu, mbps, true
}

// field reads one whitespace-separated number by index.
func field(fields []string, i int) (float64, bool) {
	if i < 0 || i >= len(fields) {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[i], 64)
	return v, err == nil
}

// clampPercent keeps rounding error and counter jitter inside 0..100, so a
// gauge never draws past its own width.
func clampPercent(v float64) float64 { return min(max(v, 0), 100) }
