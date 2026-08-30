package sysinfo

import (
	"math"
	"testing"
	"time"
)

// Every parser here is fed captured output rather than the live machine, so
// the Linux paths are covered when the suite runs on macOS and the macOS
// paths are covered when it runs on Linux. Only a real sample can exercise
// the platform dispatch, and TestSampleOnThisMachine does that.

const procStat = `cpu  197 34 118 8452 61 0 12 0 0 0
cpu0 98 17 59 4226 30 0 6 0 0 0
cpu1 99 17 59 4226 31 0 6 0 0 0
intr 1234567 0 0
ctxt 9876543
`

func TestParseProcStat(t *testing.T) {
	got, err := parseProcStat(procStat)
	if err != nil {
		t.Fatal(err)
	}

	// user+nice+system+idle+iowait+irq+softirq+steal, with the two guest
	// counters dropped because user already includes them.
	const wantTotal = 197 + 34 + 118 + 8452 + 61 + 0 + 12 + 0
	if got.total != wantTotal {
		t.Errorf("total = %v, want %v", got.total, float64(wantTotal))
	}
	// idle and iowait are both "nothing to run".
	if want := float64(wantTotal - 8452 - 61); got.busy != want {
		t.Errorf("busy = %v, want %v", got.busy, want)
	}
	if want := float64(197 + 34); got.user != want {
		t.Errorf("user = %v, want %v", got.user, want)
	}
}

func TestParseProcStatRejectsGarbage(t *testing.T) {
	for name, in := range map[string]string{
		"empty":         "",
		"no cpu line":   "intr 12345\nctxt 678\n",
		"short":         "cpu 1 2\n",
		"not a number":  "cpu  a b c d e\n",
		"per-cpu first": "cpu0 1 2 3 4 5\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseProcStat(in); err == nil {
				t.Error("want an error, got none")
			}
		})
	}
}

func TestCPUDelta(t *testing.T) {
	prev := cpuTimes{busy: 100, user: 60, system: 40, total: 1000, ok: true}
	cur := cpuTimes{busy: 175, user: 105, system: 70, total: 1300, ok: true}

	got := cpuDelta(prev, cur)
	if !got.OK {
		t.Fatal("delta over a moving counter should be OK")
	}
	// 75 busy ticks out of 300 elapsed.
	if math.Abs(got.Busy-25) > 0.001 {
		t.Errorf("Busy = %v, want 25", got.Busy)
	}
	if math.Abs(got.User-15) > 0.001 {
		t.Errorf("User = %v, want 15", got.User)
	}
}

// A counter that has not moved, or that went backwards under a container
// boundary, must report nothing rather than a spike or a negative gauge.
func TestCPUDeltaRejectsUnusableIntervals(t *testing.T) {
	ok := cpuTimes{busy: 100, total: 1000, ok: true}
	cases := map[string]struct{ prev, cur cpuTimes }{
		"no previous reading": {cpuTimes{}, ok},
		"identical readings":  {ok, ok},
		"counter reset":       {ok, cpuTimes{busy: 10, total: 100, ok: true}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := cpuDelta(c.prev, c.cur); got.OK {
				t.Errorf("want no reading, got %+v", got)
			}
		})
	}
}

const iostatOut = `      cpu    load average
 us sy id   1m   5m   15m
 11  6 83  2.55 2.47 2.56
 22 10 68  2.55 2.47 2.56
`

// Without "-n 0" — on a BSD whose iostat does not take the flag — the disk
// columns push the CPU columns to the right, which is why they are located by
// header rather than by position.
const iostatWithDisks = `          disk0           disk2       cpu    load average
    KB/t  tps  MB/s     KB/t  tps  MB/s  us sy id   1m   5m   15m
   23.44  160  3.65    10.75  261  2.74  11  6 83  2.04 2.52 2.60
   18.02   94  1.65     9.31  102  0.93   4  5 91  2.04 2.52 2.60
`

func TestParseIostat(t *testing.T) {
	// The last sample is the one-second interval; the first covers all of
	// the time since boot and is discarded.
	got, io := parseIostat(iostatOut)
	if !got.OK || got.Busy != 32 || got.User != 22 || got.System != 10 {
		t.Errorf("parseIostat = %+v, want busy 32 user 22 system 10", got)
	}
	if io.OK {
		t.Errorf("no MB/s column means no disk reading, got %+v", io)
	}

	got, io = parseIostat(iostatWithDisks)
	if !got.OK || got.Busy != 9 || got.User != 4 {
		t.Errorf("parseIostat with disk columns = %+v, want busy 9 user 4", got)
	}
	// The per-disk MB/s columns are summed: 1.65 + 0.93.
	const mib = 1 << 20
	if want := 2.58 * mib; !io.OK || math.Abs(io.Total-want) > mib/100 {
		t.Errorf("disk throughput = %+v, want %.0f B/s", io, want)
	}
	// iostat does not separate reads from writes.
	if io.Split {
		t.Error("iostat cannot split reads from writes and should not claim to")
	}
}

func TestParseIostatRejectsGarbage(t *testing.T) {
	for name, in := range map[string]string{
		"empty":            "",
		"header only":      " us sy id\n",
		"no header":        " 11  6 83\n",
		"unreadable value": " us sy id\n  x  y  z\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got, _ := parseIostat(in); got.OK {
				t.Errorf("want no reading, got %+v", got)
			}
		})
	}
}

const meminfo = `MemTotal:       16116376 kB
MemFree:          204880 kB
MemAvailable:   11623284 kB
Buffers:          412000 kB
Cached:          9800000 kB
SReclaimable:     301000 kB
SwapTotal:       2097148 kB
SwapFree:        1572864 kB
`

func TestParseMeminfo(t *testing.T) {
	mem, swap := parseMeminfo(meminfo)

	const kib = 1024
	if !mem.OK || mem.Total != 16116376*kib {
		t.Fatalf("mem = %+v", mem)
	}
	// Used is total minus available, not total minus free: page cache is
	// counted as used by the kernel but is handed back on demand.
	if want := int64(16116376-11623284) * kib; mem.Used != want {
		t.Errorf("mem.Used = %d, want %d", mem.Used, want)
	}
	if !swap.OK || swap.Used != int64(2097148-1572864)*kib {
		t.Errorf("swap = %+v", swap)
	}
	if mem.Free != 204880*kib {
		t.Errorf("mem.Free = %d", mem.Free)
	}
	// Cache is the page cache plus the reclaimable slab plus buffers: all
	// of it is memory the kernel will hand back on demand.
	if want := int64(9800000+301000+412000) * kib; mem.Cached != want {
		t.Errorf("mem.Cached = %d, want %d", mem.Cached, want)
	}
	// Linux publishes no wired or compressed figure, and inventing one
	// would be worse than leaving the field at zero.
	if mem.Wired != 0 || mem.Compressed != 0 {
		t.Errorf("mem = %+v, want no wired or compressed figure on Linux", mem)
	}
}

// MemAvailable arrived in Linux 3.14; older kernels and some container
// runtimes omit it, and free+buffers+cached is the same estimate by hand.
func TestParseMeminfoWithoutMemAvailable(t *testing.T) {
	mem, _ := parseMeminfo(`MemTotal:       16116376 kB
MemFree:          204880 kB
Buffers:          412000 kB
Cached:          9800000 kB
`)
	const kib = 1024
	if want := int64(16116376-204880-412000-9800000) * kib; !mem.OK || mem.Used != want {
		t.Errorf("mem = %+v, want Used %d", mem, want)
	}
}

// Swap being off is a fact about the machine, not a failure to read it.
func TestParseMeminfoSwapOff(t *testing.T) {
	_, swap := parseMeminfo("MemTotal: 100 kB\nMemAvailable: 50 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")
	if !swap.OK || swap.Total != 0 || swap.Percent() != 0 {
		t.Errorf("swap = %+v, want a readable zero", swap)
	}
}

const vmStat = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                4019.
Pages active:                            388121.
Pages inactive:                          384208.
Pages speculative:                         2501.
Pages wired down:                        207169.
Pages purgeable:                           6181.
File-backed pages:                       273162.
Pages occupied by compressor:            544566.
`

func TestParseVMStat(t *testing.T) {
	const total = 25769803776
	got := parseVMStat(vmStat, total)

	// active + wired + compressed, which is Activity Monitor's "Memory
	// Used". Inactive pages are a cache macOS releases under pressure, so
	// counting them would pin the gauge near full on a healthy machine.
	if want := int64(388121+207169+544566) * 16384; !got.OK || got.Used != want {
		t.Errorf("parseVMStat = %+v, want Used %d", got, want)
	}
	if got.Total != total {
		t.Errorf("Total = %d, want %d", got.Total, total)
	}

	const page = 16384
	if want := int64(207169) * page; got.Wired != want {
		t.Errorf("Wired = %d, want %d", got.Wired, want)
	}
	if want := int64(544566) * page; got.Compressed != want {
		t.Errorf("Compressed = %d, want %d", got.Compressed, want)
	}
	// File-backed pages are the page cache: a copy of something on disk,
	// droppable at will.
	if want := int64(273162) * page; got.Cached != want {
		t.Errorf("Cached = %d, want %d", got.Cached, want)
	}
	if want := int64(4019+2501) * page; got.Free != want {
		t.Errorf("Free = %d, want %d", got.Free, want)
	}
}

// The page size is 16K on Apple silicon and 4K on Intel, so it is read from
// the header rather than assumed.
func TestParseVMStatReadsPageSize(t *testing.T) {
	got := parseVMStat("Mach Virtual Memory Statistics: (page size of 4096 bytes)\nPages active: 100.\nPages wired down: 100.\n", 1<<30)
	if want := int64(200 * 4096); got.Used != want {
		t.Errorf("Used = %d, want %d", got.Used, want)
	}
}

func TestParseSwapusage(t *testing.T) {
	got := parseSwapusage("total = 3072.00M  used = 1975.25M  free = 1096.75M  (encrypted)\n")
	if !got.OK || got.Total != 3072*(1<<20) {
		t.Fatalf("parseSwapusage = %+v", got)
	}
	if want := int64(1975.25 * (1 << 20)); got.Used != want {
		t.Errorf("Used = %d, want %d", got.Used, want)
	}
	if got := parseSwapusage("nonsense"); got.OK {
		t.Error("unparseable input should not report OK")
	}
}

const dfOut = `Filesystem     1024-blocks      Used Available Capacity  Mounted on
/dev/disk3s1s1   482797652  17631468  10144536    64%    /
`

func TestParseDF(t *testing.T) {
	got, err := parseDF(dfOut)
	if err != nil {
		t.Fatal(err)
	}
	const kib = 1024
	if got.Used != 17631468*kib || got.Avail != 10144536*kib || got.Total != 482797652*kib {
		t.Fatalf("parseDF = %+v", got)
	}
	// df's own capacity column says 64%, and dividing by Total would say 4.
	if p := got.Percent(); math.Abs(p-63.48) > 0.01 {
		t.Errorf("Percent = %.2f, want ~63.48 (df says 64%%)", p)
	}
}

// A device or mount point can contain a space, so the numbers are found by
// parsing rather than by counting fields from the left.
func TestParseDFWithSpacedFields(t *testing.T) {
	got, err := parseDF(`Filesystem 1024-blocks Used Available Capacity Mounted on
map auto_home 400 100 300 25% /System/Volumes/Data/home
`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Used != 100*1024 || got.Avail != 300*1024 {
		t.Errorf("parseDF = %+v", got)
	}
}

func TestParseDFRejectsGarbage(t *testing.T) {
	for name, in := range map[string]string{
		"empty":       "",
		"header only": "Filesystem 1024-blocks Used Available Capacity Mounted on\n",
		"no numbers":  "somefs a b c d /\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDF(in); err == nil {
				t.Error("want an error, got none")
			}
		})
	}
}

const procNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1875297  241685    0    0    0     0          0         0  1875297   241685    0    0    0     0       0          0
  eth0: 9382013   84512    0    0    0     0          0         0  1204831    41023    0    0    0     0       0          0
  eth1:  100000    1000    0    0    0     0          0         0   200000     2000    0    0    0     0       0          0
`

func TestParseProcNetDev(t *testing.T) {
	// No interface named: every non-loopback one is summed, and lo is left
	// out because a machine talking to itself is not throughput.
	got := parseProcNetDev(procNetDev, "")
	if !got.ok || got.rx != 9382013+100000 || got.tx != 1204831+200000 {
		t.Errorf("summed = %+v", got)
	}

	got = parseProcNetDev(procNetDev, "eth0")
	if !got.ok || got.rx != 9382013 || got.tx != 1204831 {
		t.Errorf("eth0 = %+v", got)
	}

	if got := parseProcNetDev(procNetDev, "nope"); got.ok {
		t.Errorf("an interface that does not exist should report nothing, got %+v", got)
	}
}

// netstat repeats an interface's counters once per address family. Only the
// <Link#n> row is read, or a dual-stack interface would be counted three
// times over.
const netstatOut = `Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0   16384 <Link#1>                        241685     0 1875297477   241685     0 1875297477     0
lo0   16384 127           127.0.0.1         241685     - 1875297477   241685     - 1875297477     -
gif0* 1280  <Link#2>                             0     0          0        0     0          0     0
en0   1500  <Link#7>    6a:2a:63:e2:fd:e0   903812     0   93820134   415023     0   12048319     0
en0   1500  192.168.1     192.168.1.42      903812     -   93820134   415023     -   12048319     -
en0   1500  fe80::1%en0 fe80:7::1           903812     -   93820134   415023     -   12048319     -
`

func TestParseNetstat(t *testing.T) {
	got := parseNetstat(netstatOut, "")
	if !got.ok || got.rx != 93820134 || got.tx != 12048319 {
		t.Errorf("summed = %+v, want one count of en0 and no lo0", got)
	}

	got = parseNetstat(netstatOut, "en0")
	if !got.ok || got.rx != 93820134 {
		t.Errorf("en0 = %+v", got)
	}
}

// The Address column is empty on some interfaces and present on others, so
// the counters are taken from the end of the line.
func TestParseNetstatWithoutAddressColumn(t *testing.T) {
	got := parseNetstat("Name Mtu Network Address Ipkts Ierrs Ibytes Opkts Oerrs Obytes Coll\nen5 1500 <Link#8> 10 0 200 20 0 400 0\n", "")
	if !got.ok || got.rx != 200 || got.tx != 400 {
		t.Errorf("parseNetstat = %+v", got)
	}
}

func TestIsLoopback(t *testing.T) {
	for _, name := range []string{"lo", "lo0", "lo1"} {
		if !isLoopback(name) {
			t.Errorf("%q should be loopback", name)
		}
	}
	for _, name := range []string{"en0", "eth0", "local0", "logical"} {
		if isLoopback(name) {
			t.Errorf("%q should not be loopback", name)
		}
	}
}

func TestNetRate(t *testing.T) {
	s := New(nil, "")

	// The counters are totals since boot, so the first reading is only a
	// baseline and has no rate to report.
	s.prevNet, s.netAt = netCounters{rx: 1000, tx: 500, ok: true}, time.Now().Add(-2*time.Second)

	got := rateFrom(s.prevNet, netCounters{rx: 3000, tx: 1500, ok: true}, 2*time.Second)
	if !got.OK || got.Rx != 1000 || got.Tx != 500 {
		t.Errorf("rate = %+v, want 1000 B/s down and 500 B/s up", got)
	}
}

func TestNetRateRejectsUnusableIntervals(t *testing.T) {
	ok := netCounters{rx: 1000, tx: 1000, ok: true}
	cases := map[string]struct {
		prev, cur netCounters
		elapsed   time.Duration
	}{
		"no baseline":     {netCounters{}, ok, time.Second},
		"no time passed":  {ok, ok, 0},
		"counter reset":   {ok, netCounters{rx: 1, tx: 1, ok: true}, time.Second},
		"unreadable read": {ok, netCounters{}, time.Second},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := rateFrom(c.prev, c.cur, c.elapsed); got.OK {
				t.Errorf("want no reading, got %+v", got)
			}
		})
	}
}

func TestParseProcUptime(t *testing.T) {
	if got := parseProcUptime("350735.47 234388.90\n"); got.Round(time.Second) != 350735*time.Second {
		t.Errorf("parseProcUptime = %s", got)
	}
	for _, in := range []string{"", "nonsense\n", "-5 1\n"} {
		if got := parseProcUptime(in); got != 0 {
			t.Errorf("parseProcUptime(%q) = %s, want 0", in, got)
		}
	}
}

func TestParseBoottime(t *testing.T) {
	got := parseBoottime("{ sec = 1787969520, usec = 835998 } Sat Aug 29 04:12:00 2026\n")
	if got.Unix() != 1787969520 {
		t.Errorf("parseBoottime = %v", got)
	}
	for _, in := range []string{"", "{ usec = 1 }", "{ sec = x }"} {
		if got := parseBoottime(in); !got.IsZero() {
			t.Errorf("parseBoottime(%q) = %v, want zero", in, got)
		}
	}
}

func TestMemoryPercent(t *testing.T) {
	m := Memory{Used: 25, Total: 100, OK: true}
	if m.Percent() != 25 {
		t.Errorf("Memory{25/100} = %.0f%%", m.Percent())
	}
	// An unread pool, and swap that is switched off, both divide by zero.
	for name, m := range map[string]Memory{
		"not ok":   {Used: 25, Total: 100},
		"no total": {Used: 0, Total: 0, OK: true},
	} {
		if m.Percent() != 0 {
			t.Errorf("%s: Percent = %v, want 0", name, m.Percent())
		}
	}
}

// The platform dispatch is the one thing a fixture cannot cover, so this
// asserts that whichever OS is running the suite answers with something.
func TestSampleOnThisMachine(t *testing.T) {
	st, err := New([]string{"/"}, "").Sample()
	if err != nil {
		t.Fatalf("Sample on %s: %v", hostname(), err)
	}
	if st.Cores < 1 {
		t.Errorf("Cores = %d", st.Cores)
	}
	if !st.Mem.OK || st.Mem.Total <= 0 {
		t.Errorf("memory unread: %+v", st.Mem)
	}
	if len(st.Disks) != 1 || !st.Disks[0].OK {
		t.Errorf("disks = %+v", st.Disks)
	}
	if st.Uptime <= 0 {
		t.Errorf("uptime = %s", st.Uptime)
	}
	if !st.CPU.OK || st.CPU.Busy < 0 || st.CPU.Busy > 100 {
		t.Errorf("cpu = %+v", st.CPU)
	}
}

// A real /proc/diskstats lists partitions alongside their parent device, and
// virtual devices alongside the real storage backing them. Summing every line
// would count the same bytes two or three times.
const diskstats = `   7       0 loop0 12 0 96 4 0 0 0 0 0 8 4 0 0 0 0 0 0
   8       0 sda 12043 2119 1580338 8231 4210 900 620000 3100 0 9000 11331 0 0 0 0 0 0
   8       1 sda1 11980 2119 1576042 8180 4180 900 618000 3080 0 8900 11260 0 0 0 0 0 0
   8       2 sda2 63 0 4296 51 30 0 2000 20 0 100 71 0 0 0 0 0 0
 259       0 nvme0n1 84512 0 9382013 4120 41023 0 1204831 2010 0 5000 6130 0 0 0 0 0 0
 259       1 nvme0n1p1 84500 0 9381000 4110 41000 0 1204000 2000 0 4990 6110 0 0 0 0 0 0
 253       0 dm-0 500 0 40000 30 200 0 16000 10 0 40 40 0 0 0 0 0 0
`

func TestParseDiskstatsCountsWholeDevicesOnce(t *testing.T) {
	got := parseDiskstats(diskstats)
	if !got.ok {
		t.Fatal("want a reading")
	}

	// sda and nvme0n1 only: their partitions are slices of the same I/O,
	// and loop0 and dm-0 are backed by storage already counted.
	if want := int64(1580338+9382013) * sectorSize; got.read != want {
		t.Errorf("read = %d, want %d", got.read, want)
	}
	if want := int64(620000+1204831) * sectorSize; got.write != want {
		t.Errorf("write = %d, want %d", got.write, want)
	}
}

func TestParseDiskstatsRejectsGarbage(t *testing.T) {
	for name, in := range map[string]string{
		"empty":        "",
		"short lines":  "   8       0 sda 12043\n",
		"virtual only": "   7       0 loop0 12 0 96 4 0 0 0 0 0 8 4 0 0 0 0 0 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := parseDiskstats(in); got.ok {
				t.Errorf("want no reading, got %+v", got)
			}
		})
	}
}

func TestIORate(t *testing.T) {
	prev := ioCounters{read: 1000, write: 500, ok: true}
	cur := ioCounters{read: 3000, write: 1500, ok: true}

	got := ioRateFrom(prev, cur, 2*time.Second)
	if !got.OK || got.Read != 1000 || got.Write != 500 || got.Total != 1500 {
		t.Errorf("rate = %+v, want 1000 read, 500 write, 1500 total", got)
	}
	// Linux counts both directions, so it may say which is which.
	if !got.Split {
		t.Error("a /proc/diskstats reading should be split")
	}
}

func TestIORateRejectsUnusableIntervals(t *testing.T) {
	ok := ioCounters{read: 1000, write: 1000, ok: true}
	cases := map[string]struct {
		prev, cur ioCounters
		elapsed   time.Duration
	}{
		"no baseline":    {ioCounters{}, ok, time.Second},
		"no time passed": {ok, ok, 0},
		"counter reset":  {ok, ioCounters{read: 1, write: 1, ok: true}, time.Second},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := ioRateFrom(c.prev, c.cur, c.elapsed); got.OK {
				t.Errorf("want no reading, got %+v", got)
			}
		})
	}
}

func TestIsVirtualDisk(t *testing.T) {
	for _, name := range []string{"loop0", "ram1", "zram0", "dm-0", "md127", "sr0"} {
		if !isVirtualDisk(name) {
			t.Errorf("%q should be virtual", name)
		}
	}
	for _, name := range []string{"sda", "nvme0n1", "vda", "hda", "xvdb"} {
		if isVirtualDisk(name) {
			t.Errorf("%q should be real", name)
		}
	}
}
