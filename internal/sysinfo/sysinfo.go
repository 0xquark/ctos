// Package sysinfo samples a machine's vitals: CPU, memory, swap, disk,
// network throughput, load average and uptime.
//
// Like internal/procs it reads what the system already publishes — /proc on
// Linux, sysctl and the stock BSD tools on macOS — rather than linking a
// metrics library, for the same three reasons: the sources are present
// everywhere that matters, the dependency list stays at four modules, and the
// same text is what `ssh host cat /proc/stat` will return once the Runner
// interface lands (ADR-023).
//
// Every parser here is a pure function over that text, so all of them are
// tested from fixtures on whichever platform CI happens to be running.
package sysinfo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// runTimeout bounds any one of the tools sysinfo shells out to. Only iostat
// takes measurable time, and it takes a second by design; the bound exists so
// that a wedged tool cannot stall a refresh forever.
const runTimeout = 10 * time.Second

// Stats is one sample of a machine's vitals.
//
// Every metric carries its own OK flag rather than the sample carrying one
// error. A machine that cannot report its swap is not a machine whose CPU
// gauge should go blank, and the platforms genuinely differ in what they will
// answer — so an unavailable metric is a quiet gap in the widget, and Sample
// fails only when nothing at all could be read.
type Stats struct {
	CPU    CPU
	Mem    Memory
	Swap   Memory
	Disks  []Disk
	Net    Net
	DiskIO DiskIO
	Load   Load
	Uptime time.Duration
	Cores  int
	Host   string

	// At is when the sample was taken.
	At time.Time
}

// CPU is the share of the machine's total capacity in use, where 100 means
// every core is busy.
type CPU struct {
	Busy   float64
	User   float64
	System float64
	OK     bool
}

// Memory is a used-of-total pair, in bytes. It describes RAM and swap alike.
//
// Free and the three breakdown fields are the platform's own categories, and
// each is zero where the platform does not publish one: Linux has no wired or
// compressed figure, and swap has none of them anywhere. A renderer shows the
// fields that are non-zero rather than inventing an equivalent.
type Memory struct {
	Used, Total int64
	Free        int64

	// Cached is reclaimable page cache: "Cached" plus the reclaimable
	// slab on Linux, file-backed pages on macOS.
	Cached int64

	// Wired is memory the kernel cannot page out. macOS only.
	Wired int64

	// Compressed is what the macOS memory compressor holds.
	Compressed int64

	OK bool
}

// Percent is how full the pool is, 0 when it is unmeasured or empty. A
// machine with swap disabled reports a total of zero rather than an error.
func (m Memory) Percent() float64 {
	if !m.OK || m.Total <= 0 {
		return 0
	}
	return float64(m.Used) / float64(m.Total) * 100
}

// Disk is the usage of one mounted filesystem.
type Disk struct {
	Path string

	// Used and Avail are df's own two figures. Avail is smaller than Total
	// minus Used, because a filesystem holds blocks back for root.
	Used, Avail int64

	// Total is the size df reports for the whole filesystem. It is kept
	// for reference but is not what Percent divides by; see below.
	Total int64

	OK bool
}

// Percent is how full the filesystem is: used over used-plus-available, which
// is how df computes its own "capacity" column.
//
// Total is deliberately not the denominator. A filesystem reserves blocks
// nothing can allocate, and macOS makes the gap dramatic: a sealed APFS
// system volume reports the size of the whole container, so dividing by it
// showed a disk that is 64% full as 4%.
func (d Disk) Percent() float64 {
	room := d.Used + d.Avail
	if !d.OK || room <= 0 {
		return 0
	}
	return float64(d.Used) / float64(room) * 100
}

// Net is network throughput in bytes per second, measured across the gap
// between two samples.
type Net struct {
	Rx, Tx float64
	OK     bool
}

// DiskIO is storage throughput in bytes per second.
//
// Split says whether Read and Write mean anything separately. Linux counts
// sectors in and out per device and can answer both; macOS publishes only a
// combined figure through iostat, so there Total is the only number and the
// renderer says "12M/s" rather than claiming a direction it does not know.
type DiskIO struct {
	Read, Write, Total float64
	Split              bool
	OK                 bool
}

// Sampler holds the counters that only mean something as a difference: CPU
// time and network bytes are both totals since boot, so a rate needs the
// previous reading as well as this one.
//
// It is not safe for concurrent use. The widget guarantees that by never
// starting a sample while one is in flight, which it wants anyway so that a
// slow sample cannot stack up behind the refresh tick.
type Sampler struct {
	paths []string
	iface string

	prevCPU  cpuTimes
	prevNet  netCounters
	netAt    time.Time
	prevIO   ioCounters
	ioAt     time.Time
	lastDisk DiskIO // macOS reads CPU and disk from one iostat call
}

// New returns a sampler that watches the given mount points, and the given
// network interface. An empty iface sums every interface except loopback;
// empty paths means no disk rows.
func New(paths []string, iface string) *Sampler {
	return &Sampler{paths: paths, iface: iface}
}

// Sample reads every vital once.
//
// It returns an error only when the platform offers nothing at all, which
// today means Windows: anything short of that is reported through the
// per-metric OK flags, so a widget keeps rendering the metrics that do work.
func (s *Sampler) Sample() (Stats, error) {
	if runtime.GOOS == "windows" {
		return Stats{}, fmt.Errorf("the system widget needs /proc or the BSD sysctl tools, which %s does not provide", runtime.GOOS)
	}

	st := Stats{
		Cores: CPUs(),
		Load:  LoadAverage(),
		Host:  hostname(),
		At:    time.Now(),
	}

	// Network is read before CPU because the darwin CPU reading blocks for
	// a second, and the throughput rate is only as good as the interval it
	// is divided by.
	st.Net = s.net()
	st.DiskIO = s.diskIO()
	st.CPU = s.cpu()
	// macOS gets its disk throughput from the same iostat run as its CPU
	// reading, so the answer only exists once that has run.
	if !st.DiskIO.OK && s.lastDisk.OK {
		st.DiskIO = s.lastDisk
	}
	st.Mem, st.Swap = memory()
	st.Disks = s.disks()
	st.Uptime = uptime()

	if !st.CPU.OK && !st.Mem.OK && !st.Load.OK && st.Uptime == 0 {
		return st, fmt.Errorf("could not read any system statistics on %s", runtime.GOOS)
	}
	return st, nil
}

// Paths are the mount points this sampler reports on.
func (s *Sampler) Paths() []string { return s.paths }

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// run executes one of the system tools and returns its stdout.
func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s timed out after %s", name, runTimeout)
		}
		return "", err
	}
	return string(out), nil
}

// readFile reads one of the /proc files. It exists so that every parser in
// this package can stay a pure function over text, tested from a fixture on
// whichever platform happens to be running the suite.
func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
