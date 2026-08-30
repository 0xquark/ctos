// Package system shows the machine's vitals: CPU, memory, swap, disk,
// network throughput, load average and uptime, as labelled bars with a short
// history behind each one.
package system

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/0xquark/ctos/internal/procs"
	"github.com/0xquark/ctos/internal/spark"
	"github.com/0xquark/ctos/internal/sysinfo"
	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
)

func init() {
	widget.Register(widget.Spec{
		Name:    "system",
		Summary: "CPU, memory, disk, network and load, as bars or a status strip",
		New:     New,
		Example: `type: system
style: rows               # "rows" is the panel, "bar" is the status strip
refresh: 3s               # never polls faster than 1s
metrics: [cpu, mem, swap, disk, diskio, net, load, top, uptime]
disks: ["/"]              # one entry per mount point
interface: ""             # network interface; empty sums all but loopback
history: true             # sparklines, in the "rows" style
deltas: true              # 30s change arrows, in the "bar" style
title: system`,
	})
}

// defaultRefresh matches the processes widget: vitals that update twice a
// minute are a screenshot, not a monitor.
const defaultRefresh = 3 * time.Second

// minRefresh is a second because that is what one CPU reading costs on macOS,
// where iostat measures a one-second interval for us. Polling faster would
// queue samples rather than produce more of them.
const minRefresh = time.Second

// historyLen is how many samples the sparklines keep. At the default refresh
// that is five minutes, and it is capped by the widest sparkline a widget
// will ever be given rather than by the pane in front of us, so that history
// survives the window being resized.
const historyLen = 100

// metric identifies one row. The set is closed: a dashboard naming something
// else is a config error, not a silently missing row.
type metric string

const (
	metricCPU    metric = "cpu"
	metricMem    metric = "mem"
	metricSwap   metric = "swap"
	metricDisk   metric = "disk"
	metricNet    metric = "net"
	metricLoad   metric = "load"
	metricUptime metric = "uptime"
	metricDiskIO metric = "diskio"
	metricTop    metric = "top"
)

// allMetrics is every metric, in the order a bar gets them by default: the
// ones that move fastest first, since those are what a glance is for.
var allMetrics = []metric{
	metricCPU, metricMem, metricSwap, metricDisk,
	metricDiskIO, metricNet, metricLoad, metricTop, metricUptime,
}

// rowMetrics is the default for the panel style. It leaves out the two that
// have no magnitude to draw a bar against — storage throughput and the top
// process — since in a column of bars they would be two rows of bare text.
var rowMetrics = []metric{metricCPU, metricMem, metricSwap, metricDisk, metricNet, metricLoad, metricUptime}

// style selects how the widget draws itself.
type style string

const (
	// styleRows is the panel: one labelled bar per metric.
	styleRows style = "rows"

	// styleBar is the status strip: pipe-separated chips on one or two
	// lines, dense enough to read across the top of a dashboard.
	styleBar style = "bar"
)

var allStyles = []style{styleRows, styleBar}

type config struct {
	Refresh string   `yaml:"refresh"`
	Style   string   `yaml:"style"`
	Metrics []string `yaml:"metrics"`
	Disks   []string `yaml:"disks"`
	Iface   string   `yaml:"interface"`
	History *bool    `yaml:"history"`
	Deltas  *bool    `yaml:"deltas"`
}

type sampledMsg struct {
	stats sysinfo.Stats
	top   topProcs
	err   error
}

// topProcs is the busiest process by each measure, for the "top" metric.
type topProcs struct {
	cpu, mem procs.Process
	ok       bool
}

type tickMsg struct{}

// System is a live vitals pane.
type System struct {
	widget.Base
	theme   theme.Theme
	refresh time.Duration

	style   style
	metrics []metric
	paths   []string
	history bool
	deltas  bool

	// deltaWindow is how many samples back a delta is measured against,
	// derived from the refresh interval so that the bar always compares
	// roughly the same span of wall-clock time.
	deltaWindow int

	sampler *sysinfo.Sampler
	stats   sysinfo.Stats
	top     topProcs
	hist    map[string]*spark.Series

	loading  bool
	inflight bool // a sample is running; the tick skips rather than stacking
	err      error
}

// New builds a system widget from its dashboard configuration.
func New(ctx widget.Context) (widget.Widget, error) {
	var cfg config
	if err := ctx.Decode(&cfg); err != nil {
		return nil, err
	}

	refresh, err := ctx.Refresh(cfg.Refresh, defaultRefresh, minRefresh)
	if err != nil {
		return nil, err
	}

	st, err := parseStyle(cfg.Style)
	if err != nil {
		return nil, err
	}

	metrics, err := parseMetrics(cfg.Metrics, st)
	if err != nil {
		return nil, err
	}

	paths, err := resolveDisks(cfg.Disks)
	if err != nil {
		return nil, err
	}

	history := cfg.History == nil || *cfg.History
	deltas := cfg.Deltas == nil || *cfg.Deltas

	s := &System{
		theme:       ctx.Theme,
		refresh:     refresh,
		style:       st,
		metrics:     metrics,
		paths:       paths,
		history:     history,
		deltas:      deltas,
		deltaWindow: deltaWindow(refresh),
		sampler:     sysinfo.New(paths, cfg.Iface),
		hist:        map[string]*spark.Series{},
		loading:     true,
	}
	return s, nil
}

// deltaSpan is the wall-clock distance a delta is measured over. Shorter than
// this and every reading is sampling noise; longer and the arrow stops
// reacting to what just happened.
const deltaSpan = 30 * time.Second

// deltaWindow converts that span into a number of samples, clamped so that a
// slow refresh still compares against something and a fast one does not
// outrun the history.
func deltaWindow(refresh time.Duration) int {
	n := int(deltaSpan / refresh)
	return min(max(n, 1), historyLen-1)
}

func parseStyle(name string) (style, error) {
	if name == "" {
		return styleRows, nil
	}
	st := style(strings.ToLower(strings.TrimSpace(name)))
	if !slices.Contains(allStyles, st) {
		return "", fmt.Errorf("unknown style %q (valid styles: rows, bar)", name)
	}
	return st, nil
}

// parseMetrics validates the row list. An unknown name is rejected by name
// with the alternatives listed, the same way an unknown config key is: a
// dashboard is hand-written, and a silently dropped row is a row the user
// will spend a while looking for.
func parseMetrics(names []string, st style) ([]metric, error) {
	if len(names) == 0 {
		if st == styleBar {
			return slices.Clone(allMetrics), nil
		}
		return slices.Clone(rowMetrics), nil
	}

	out := make([]metric, 0, len(names))
	for _, name := range names {
		m := metric(strings.ToLower(strings.TrimSpace(name)))
		if !slices.Contains(allMetrics, m) {
			return nil, fmt.Errorf("unknown metric %q (valid metrics: %s)", name, strings.Join(metricNames(), ", "))
		}
		if slices.Contains(out, m) {
			return nil, fmt.Errorf("metric %q listed twice", name)
		}
		out = append(out, m)
	}
	return out, nil
}

func metricNames() []string {
	out := make([]string, len(allMetrics))
	for i, m := range allMetrics {
		out[i] = string(m)
	}
	return out
}

// resolveDisks checks the mount points now rather than rendering "n/a" at
// runtime, so a typo in a path is reported while the user is still looking at
// the file they typed it into.
func resolveDisks(paths []string) ([]string, error) {
	if paths == nil {
		return []string{"/"}, nil
	}

	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("disk %q: %w", p, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// Init takes the first sample immediately.
func (s *System) Init() tea.Cmd { return s.sample() }

// Update handles sample results and the refresh tick.
func (s *System) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case sampledMsg:
		s.inflight, s.loading = false, false
		s.err = msg.err
		if msg.err == nil {
			s.stats, s.top = msg.stats, msg.top
			s.record(msg.stats)
		}
		return s.Every(s.refresh, tickMsg{})

	case tickMsg:
		// A sample can outlast the tick — the macOS CPU reading takes a
		// second on its own — so a tick that arrives mid-sample is
		// dropped rather than starting a second one.
		if s.inflight {
			return s.Every(s.refresh, tickMsg{})
		}
		return s.sample()

	case tea.KeyMsg:
		if msg.String() == "r" && !s.inflight {
			return s.sample()
		}
	}
	return nil
}

// sample reads the vitals off the UI goroutine.
func (s *System) sample() tea.Cmd {
	s.inflight = true
	wantTop := slices.Contains(s.metrics, metricTop)

	return s.Cmd(func() tea.Msg {
		stats, err := s.sampler.Sample()
		msg := sampledMsg{stats: stats, err: err}
		if wantTop {
			msg.top = sampleTop()
		}
		return msg
	})
}

// sampleTop finds the busiest process by each measure.
//
// It is a second `ps` on top of the vitals read, which is why it only runs
// when the metric is actually configured: a bar nobody asked for should not
// cost an exec every few seconds.
func sampleTop() topProcs {
	list, err := procs.Sample()
	if err != nil || len(list) == 0 {
		return topProcs{}
	}

	top := topProcs{cpu: list[0], mem: list[0], ok: true}
	for _, p := range list[1:] {
		if p.CPU > top.cpu.CPU {
			top.cpu = p
		}
		if p.RSS > top.mem.RSS {
			top.mem = p
		}
	}
	return top
}

// record appends this sample to the sparkline histories.
//
// Only the bounded metrics are kept. Uptime only ever rises and swap moves
// too slowly to draw, so neither would say anything a number does not.
func (s *System) record(st sysinfo.Stats) {
	if !s.history {
		return
	}
	if st.CPU.OK {
		s.push(string(metricCPU), st.CPU.Busy)
	}
	if st.Mem.OK {
		s.push(string(metricMem), st.Mem.Percent())
	}
	if st.Load.OK {
		s.push(string(metricLoad), st.Load.One/float64(max(st.Cores, 1))*100)
	}
	if st.Net.OK {
		s.push(netRxKey, st.Net.Rx)
		s.push(netTxKey, st.Net.Tx)
	}
	if st.DiskIO.OK {
		s.push(diskIOKey, st.DiskIO.Total)
	}
	for _, d := range st.Disks {
		if d.OK {
			s.push(diskKey(d.Path), d.Percent())
		}
	}
}

// delta is the change in a metric over the delta window, and whether there is
// enough history — and enough movement — to be worth drawing.
//
// The floor matters as much as the window: a machine at rest jitters by a
// tenth of a percent every tick, and a bar that renders an arrow for that is
// noise wearing the costume of information.
func (s *System) delta(key string, floor float64) (float64, bool) {
	if !s.deltas || !s.history {
		return 0, false
	}
	series, ok := s.hist[key]
	if !ok {
		return 0, false
	}
	d, ok := series.Delta(s.deltaWindow)
	if !ok || math.Abs(d) < floor {
		return 0, false
	}
	return d, true
}

func (s *System) push(key string, v float64) {
	series, ok := s.hist[key]
	if !ok {
		series = spark.NewSeries(historyLen)
		s.hist[key] = series
	}
	series.Push(v)
}

const (
	netRxKey  = "net.rx"
	netTxKey  = "net.tx"
	diskIOKey = "diskio"
)

// diskKey names a mount point's history, so two disks do not share one.
func diskKey(path string) string { return "disk:" + path }
