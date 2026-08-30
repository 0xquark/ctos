package sysinfo

import (
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ioCounters is the total bytes read from and written to storage since boot.
type ioCounters struct {
	read, write int64
	ok          bool
}

// sectorSize is the unit /proc/diskstats counts in. It is 512 bytes
// regardless of the device's real block size — the kernel normalises it — so
// it is a constant rather than something to look up per device.
const sectorSize = 512

// diskIO reads storage throughput since the previous sample.
//
// Only Linux answers here. macOS publishes no cumulative per-device byte
// counter without root, so its figure comes from the iostat run that already
// measures CPU (see parseIostat) and is stitched in by Sample.
func (s *Sampler) diskIO() DiskIO {
	if runtime.GOOS != "linux" {
		return DiskIO{}
	}

	out, err := readFile("/proc/diskstats")
	if err != nil {
		return DiskIO{}
	}
	cur := parseDiskstats(out)

	now := time.Now()
	prev, at := s.prevIO, s.ioAt
	s.prevIO, s.ioAt = cur, now
	if at.IsZero() {
		return DiskIO{}
	}
	return ioRateFrom(prev, cur, now.Sub(at))
}

// ioRateFrom turns two counter readings into bytes per second, on the same
// terms as the network rate: a reset counter or a zero interval reports
// nothing and lets the next sample re-establish the baseline.
func ioRateFrom(prev, cur ioCounters, elapsed time.Duration) DiskIO {
	secs := elapsed.Seconds()
	if !prev.ok || !cur.ok || secs <= 0 || cur.read < prev.read || cur.write < prev.write {
		return DiskIO{}
	}
	r := float64(cur.read-prev.read) / secs
	w := float64(cur.write-prev.write) / secs
	return DiskIO{Read: r, Write: w, Total: r + w, Split: true, OK: true}
}

// parseDiskstats reads /proc/diskstats:
//
//	  8       0 sda 12043 2119 1580338 8231 ...
//	  8       1 sda1 11980 2119 1576042 8180 ...
//	259       0 nvme0n1 84512 0 9382013 4120 ...
//
// After the major, minor and device name come the read counters, of which the
// third is sectors read; six fields later is sectors written.
//
// Partitions are skipped, because their parent device counts the same I/O
// again — the rule is that a device whose name extends another listed device's
// name is a slice of it, which holds for sda1 under sda and for nvme0n1p1
// under nvme0n1. Virtual devices are skipped for the same reason: loop and ram
// devices are backed by files or memory that is counted elsewhere.
func parseDiskstats(out string) ioCounters {
	type entry struct{ read, write int64 }

	found := map[string]entry{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		name := fields[2]
		if isVirtualDisk(name) {
			continue
		}

		read, err1 := strconv.ParseInt(fields[5], 10, 64)
		write, err2 := strconv.ParseInt(fields[9], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		found[name] = entry{read: read * sectorSize, write: write * sectorSize}
	}

	var total ioCounters
	for name, e := range found {
		if hasParent(name, found) {
			continue
		}
		total.read += e.read
		total.write += e.write
		total.ok = true
	}
	return total
}

// hasParent reports whether another listed device's name is a prefix of this
// one, which is what makes this device a partition of that one.
func hasParent[T any](name string, all map[string]T) bool {
	for other := range all {
		if other != name && strings.HasPrefix(name, other) {
			return true
		}
	}
	return false
}

// isVirtualDisk reports whether a device is backed by something already
// counted somewhere else.
func isVirtualDisk(name string) bool {
	for _, prefix := range []string{"loop", "ram", "zram", "dm-", "md", "sr"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
