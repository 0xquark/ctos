package sysinfo

import (
	"runtime"
	"strconv"
	"strings"
)

// memory reads RAM and swap usage.
func memory() (mem, swap Memory) {
	switch runtime.GOOS {
	case "linux":
		return memFromMeminfo()
	case "darwin":
		return memFromVMStat(), swapFromSysctl()
	default:
		return Memory{}, Memory{}
	}
}

func memFromMeminfo() (mem, swap Memory) {
	out, err := readFile("/proc/meminfo")
	if err != nil {
		return Memory{}, Memory{}
	}
	return parseMeminfo(out)
}

// parseMeminfo reads /proc/meminfo, whose values are in kibibytes:
//
//	MemTotal:       16116376 kB
//	MemAvailable:   11623284 kB
//	SwapTotal:       2097148 kB
//	SwapFree:        2097148 kB
//
// Used is total minus *available* rather than minus free, because Linux
// counts page cache as used and will hand it back on demand: "free" on a
// healthy machine is near zero and would make every gauge read full.
//
// MemAvailable arrived in Linux 3.14. Older kernels, and a few container
// runtimes, omit it; there the sum of free, buffers and cached is the same
// estimate made by hand.
func parseMeminfo(out string) (mem, swap Memory) {
	kb := map[string]int64{}
	for _, line := range strings.Split(out, "\n") {
		key, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if v, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
			kb[key] = v
		}
	}

	const kib = 1024
	if total := kb["MemTotal"]; total > 0 {
		avail, ok := kb["MemAvailable"]
		if !ok {
			avail = kb["MemFree"] + kb["Buffers"] + kb["Cached"]
		}
		mem = Memory{
			Used:  (total - min(avail, total)) * kib,
			Total: total * kib,
			Free:  kb["MemFree"] * kib,
			// The reclaimable slab is cache too, and on a machine
			// with many inodes open it is a large share of it.
			Cached: (kb["Cached"] + kb["SReclaimable"] + kb["Buffers"]) * kib,
			OK:     true,
		}
	}
	// A machine with swap off reports a total of zero, which is a fact
	// about it rather than a failure to read.
	if total, ok := kb["SwapTotal"]; ok {
		free := kb["SwapFree"]
		swap = Memory{
			Used:  (total - min(free, total)) * kib,
			Total: total * kib,
			Free:  free * kib,
			OK:    true,
		}
	}
	return mem, swap
}

func memFromVMStat() Memory {
	total, err := sysctlInt("hw.memsize")
	if err != nil {
		return Memory{}
	}
	out, err := run("vm_stat")
	if err != nil {
		return Memory{}
	}
	return parseVMStat(out, total)
}

// parseVMStat reads vm_stat's page counts:
//
//	Mach Virtual Memory Statistics: (page size of 16384 bytes)
//	Pages free:                        4019.
//	Pages active:                    388121.
//	Pages wired down:                207169.
//	Pages occupied by compressor:    544566.
//
// Used is active plus wired plus compressed, which is the figure Activity
// Monitor shows as "Memory Used". Inactive pages are deliberately excluded:
// macOS keeps them as a cache it will release under pressure, so counting
// them would leave the gauge pinned near full on every healthy machine.
func parseVMStat(out string, total int64) Memory {
	// A page is 16K on Apple silicon and 4K on Intel, so the size is read
	// from the header rather than assumed.
	pageSize := int64(4096)
	if _, rest, found := strings.Cut(out, "page size of "); found {
		if fields := strings.Fields(rest); len(fields) > 0 {
			if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil && n > 0 {
				pageSize = n
			}
		}
	}

	pages := map[string]int64{}
	for _, line := range strings.Split(out, "\n") {
		key, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		v := strings.TrimSuffix(strings.TrimSpace(rest), ".")
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			pages[strings.TrimSpace(key)] = n
		}
	}

	page := func(key string) int64 { return pages[key] * pageSize }

	used := page("Pages active") + page("Pages wired down") + page("Pages occupied by compressor")
	if used <= 0 || total <= 0 {
		return Memory{}
	}
	return Memory{
		Used:  min(used, total),
		Total: total,
		Free:  page("Pages free") + page("Pages speculative"),
		// File-backed pages are macOS's page cache: memory holding a
		// copy of something on disk, which it can drop at will.
		Cached:     page("File-backed pages"),
		Wired:      page("Pages wired down"),
		Compressed: page("Pages occupied by compressor"),
		OK:         true,
	}
}

func swapFromSysctl() Memory {
	out, err := run("sysctl", "-n", "vm.swapusage")
	if err != nil {
		return Memory{}
	}
	return parseSwapusage(out)
}

// parseSwapusage reads vm.swapusage, which prints sizes with a unit suffix:
//
//	total = 3072.00M  used = 1975.25M  free = 1096.75M  (encrypted)
func parseSwapusage(out string) Memory {
	// Read as "key = value" triples rather than by splitting on runs of
	// spaces, which the trailing "(encrypted)" would otherwise break.
	vals := map[string]int64{}
	fields := strings.Fields(out)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i+1] != "=" {
			continue
		}
		if n, ok := parseSuffixed(fields[i+2]); ok {
			vals[fields[i]] = n
		}
	}

	total, ok := vals["total"]
	if !ok {
		return Memory{}
	}
	return Memory{
		Used:  min(vals["used"], total),
		Total: total,
		Free:  vals["free"],
		OK:    true,
	}
}

// parseSuffixed reads a number with an optional binary unit suffix, as the
// BSD tools write sizes: "1975.25M", "3072.00M", "0.00K".
func parseSuffixed(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	mult := float64(1)
	switch s[len(s)-1] {
	case 'K':
		mult, s = 1<<10, s[:len(s)-1]
	case 'M':
		mult, s = 1<<20, s[:len(s)-1]
	case 'G':
		mult, s = 1<<30, s[:len(s)-1]
	case 'T':
		mult, s = 1<<40, s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return int64(v * mult), true
}

// sysctlInt reads one integer sysctl.
func sysctlInt(name string) (int64, error) {
	out, err := run("sysctl", "-n", name)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(out), 10, 64)
}
