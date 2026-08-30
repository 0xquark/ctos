// Package processes shows the local process table, htop-style: sortable,
// filterable, and able to signal the selected process.
package processes

import (
	"context"
	"fmt"
	"os/user"
	"strings"
	"time"

	"github.com/0xquark/ctos/internal/procs"
	"github.com/0xquark/ctos/internal/sysinfo"
	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
)

func init() {
	widget.Register(widget.Spec{
		Name:    "processes",
		Summary: "a live process table you can sort, filter and kill from",
		New:     New,
		Example: `type: processes
refresh: 3s               # never polls faster than 500ms
sort: cpu                 # cpu, mem, pid or name
user: me                  # "me" resolves to the current user
filter: ""                # initial query, editable with "/"
hide_idle: false
detail: true              # ancestry pane below the list, toggled with "d"
detail_lines: 0           # 0 splits the widget in half
log_window: 5m            # how far back "l" reads logs
title: processes`,
	})
}

// defaultRefresh is deliberately faster than the dashboard-wide default: a
// process table that updates every 30s is a screenshot, not a monitor.
const defaultRefresh = 3 * time.Second

// minRefresh keeps a typo in the config from spawning `ps` in a tight loop.
const minRefresh = 500 * time.Millisecond

// afterKillDelay is how long to wait before re-sampling once a signal is
// sent, so the table reflects the kill without a manual refresh.
const afterKillDelay = 400 * time.Millisecond

// defaultLogWindow is how far back the log pane looks. macOS scans a binary
// store to answer this, so a wider window costs real seconds.
const defaultLogWindow = 5 * time.Minute

// minDetailHeight is the shortest widget that can usefully split. Below this
// the detail pane would get two lines and the list would get three, so the
// list keeps the whole pane instead.
const minDetailHeight = 12

type config struct {
	Refresh     string `yaml:"refresh"`
	Sort        string `yaml:"sort"`
	User        string `yaml:"user"`
	Filter      string `yaml:"filter"`
	HideIdle    bool   `yaml:"hide_idle"`
	Detail      *bool  `yaml:"detail"`
	DetailLines int    `yaml:"detail_lines"`
	LogWindow   string `yaml:"log_window"`
}

type sampledMsg struct {
	procs []procs.Process
	load  sysinfo.Load
	err   error
}

type tickMsg struct{}

type signalledMsg struct {
	pid int
	sig procs.Signal
	err error
}

type logsMsg struct {
	pid   int
	lines []string
	err   error
}

// detailMode is what the lower pane shows.
type detailMode int

const (
	detailInfo detailMode = iota // ancestry tree and process facts
	detailLogs                   // recent log lines for the selected process
)

// Processes is a live process table.
type Processes struct {
	widget.Base
	cfg     config
	theme   theme.Theme
	refresh time.Duration

	all   []procs.Process // every process from the last sample
	rows  []procs.Process // after user/idle/query filtering and sorting
	load  sysinfo.Load
	index *procs.Index // ancestry lookup over the last sample

	sort     procs.Sort
	reversed bool
	query    string
	typing   bool // filter input has the keyboard

	showDetail bool
	detail     detailMode
	logWindow  time.Duration

	logs        []string
	logsPID     int // which process p.logs belongs to
	logsErr     error
	logsLoading bool

	list   widget.List
	selPID int // survives re-sorting, so a refresh cannot move the target

	confirm *procs.Process // armed kill awaiting confirmation
	status  string         // transient result line
	err     error
	loading bool
}

// New builds a processes widget from its dashboard configuration.
func New(ctx widget.Context) (widget.Widget, error) {
	var cfg config
	if err := ctx.Decode(&cfg); err != nil {
		return nil, err
	}

	refresh, err := ctx.Refresh(cfg.Refresh, defaultRefresh, minRefresh)
	if err != nil {
		return nil, err
	}

	sortBy, err := parseSort(cfg.Sort)
	if err != nil {
		return nil, err
	}

	// "me" saves the user from hardcoding their own username in a config
	// file they might share.
	if cfg.User == "me" {
		if u, err := user.Current(); err == nil {
			cfg.User = u.Username
		} else {
			cfg.User = ""
		}
	}

	logWindow := defaultLogWindow
	if cfg.LogWindow != "" {
		d, err := ctx.Duration("log_window", cfg.LogWindow)
		if err != nil {
			return nil, err
		}
		logWindow = d
	}

	// detail is a *bool so an explicit "detail: false" is distinguishable
	// from the key being absent.
	showDetail := true
	if cfg.Detail != nil {
		showDetail = *cfg.Detail
	}

	return &Processes{
		cfg:        cfg,
		theme:      ctx.Theme,
		refresh:    refresh,
		sort:       sortBy,
		query:      cfg.Filter,
		showDetail: showDetail,
		logWindow:  logWindow,
	}, nil
}

func parseSort(s string) (procs.Sort, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "cpu":
		return procs.ByCPU, nil
	case "mem", "memory":
		return procs.ByMem, nil
	case "pid":
		return procs.ByPID, nil
	case "name", "command":
		return procs.ByName, nil
	default:
		return procs.ByCPU, fmt.Errorf("invalid sort %q: use cpu, mem, pid or name", s)
	}
}

// Init takes the first sample.
func (p *Processes) Init() tea.Cmd {
	p.loading = true
	return p.sample()
}

// GrabsKeys claims the keyboard while a filter query is being typed.
func (p *Processes) GrabsKeys() bool { return p.typing }

// Update handles sampling results, the refresh tick and key input.
func (p *Processes) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case sampledMsg:
		p.loading = false
		p.err = msg.err
		if msg.err == nil {
			p.all, p.load = msg.procs, msg.load
			p.index = procs.NewIndex(msg.procs)
			p.rebuild()
		}
		return p.scheduleTick()

	case tickMsg:
		// Skip the tick when a sample is still in flight, so a slow `ps`
		// cannot pile up runs behind itself.
		if p.loading {
			return nil
		}
		p.loading = true
		return p.sample()

	case signalledMsg:
		if msg.err != nil {
			p.status = fmt.Sprintf("could not signal %d: %v", msg.pid, msg.err)
		} else {
			p.status = fmt.Sprintf("sent %s to %d", msg.sig, msg.pid)
		}
		return p.Every(afterKillDelay, tickMsg{})

	case logsMsg:
		// A result for a process the user has since moved off is stale.
		if msg.pid != p.selPID {
			return nil
		}
		p.logsLoading = false
		p.logs, p.logsErr, p.logsPID = msg.lines, msg.err, msg.pid
		return nil

	case tea.KeyMsg:
		if p.typing {
			return p.filterKey(msg)
		}
		return p.key(msg)
	}
	return nil
}

// filterKey handles input while the filter box owns the keyboard.
func (p *Processes) filterKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEnter:
		p.typing = false // keep the query, hand the keyboard back
	case tea.KeyEsc:
		p.typing = false
		p.query = ""
		p.rebuild()
	case tea.KeyBackspace:
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.rebuild()
		}
	case tea.KeyRunes, tea.KeySpace:
		p.query += string(msg.Runes)
		if msg.Type == tea.KeySpace {
			p.query += " "
		}
		p.rebuild()
	}
	return nil
}

// key handles normal navigation.
func (p *Processes) key(msg tea.KeyMsg) tea.Cmd {
	// While a kill is armed the list freezes: every key either confirms,
	// escalates, or cancels. Nothing else should be one keystroke away.
	if p.confirm != nil {
		switch msg.String() {
		case "k":
			return p.send(procs.Kill)
		case "esc", "n":
			p.confirm = nil
		}
		return nil
	}

	if p.list.HandleKey(msg, p.listHeight()) {
		// The result of the last kill is no longer what you are looking at.
		p.status = ""
		p.follow()
		return p.logsForSelection()
	}

	switch msg.String() {
	case "s":
		p.setSort(p.sort.Next())
	case "c":
		p.setSort(procs.ByCPU)
	case "m":
		p.setSort(procs.ByMem)
	case "p":
		p.setSort(procs.ByPID)
	case "n":
		p.setSort(procs.ByName)

	case "d":
		p.showDetail = !p.showDetail
	case "l":
		return p.toggleLogs()

	case "/":
		p.typing = true
		p.status = ""
	case "r":
		if !p.loading {
			p.loading = true
			return p.sample()
		}
	}

	return p.logsForSelection()
}

// logsForSelection re-queries the log pane when it is open and showing some
// other process than the selected one.
func (p *Processes) logsForSelection() tea.Cmd {
	if p.detail == detailLogs && p.showDetail && p.selPID != p.logsPID && !p.logsLoading {
		return p.fetchLogs()
	}
	return nil
}

// setSort switches the sort column, or reverses it when it is already active.
// That is the behaviour of every table anyone has used, so it needs no key of
// its own.
func (p *Processes) setSort(by procs.Sort) {
	if by == p.sort {
		p.reversed = !p.reversed
	} else {
		p.sort, p.reversed = by, false
	}
	// Jump to the head of the new order. Following the selected PID is right
	// for a refresh, which the user did not ask for, but wrong for a re-sort:
	// the reason you press "m" is to see the biggest, and landing halfway down
	// the list beside your previous selection hides exactly that.
	p.selPID = 0
	p.rebuild()
	p.list.Top()
	p.follow()
}

// toggleLogs swaps the detail pane between the ancestry view and the log
// view, opening the pane if it was closed.
func (p *Processes) toggleLogs() tea.Cmd {
	if !p.showDetail {
		p.showDetail = true
		p.detail = detailLogs
		return p.fetchLogs()
	}
	if p.detail == detailLogs {
		p.detail = detailInfo
		return nil
	}
	p.detail = detailLogs
	return p.fetchLogs()
}

// fetchLogs queries the system log for the selected process. Results are
// tagged with the PID so one that arrives after the user has moved on is
// discarded rather than shown under the wrong process.
func (p *Processes) fetchLogs() tea.Cmd {
	if p.list.Empty() {
		return nil
	}
	pid, window := p.rows[p.list.Cursor()].PID, p.logWindow
	p.logsLoading = true
	p.logs, p.logsErr, p.logsPID = nil, nil, pid

	return p.Cmd(func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), procs.LogTimeout)
		defer cancel()
		lines, err := procs.Logs(ctx, pid, window)
		return logsMsg{pid: pid, lines: lines, err: err}
	})
}

// Actions exposes the kill, whose label reflects whether it is armed.
func (p *Processes) Actions() []widget.Action {
	if len(p.rows) == 0 {
		return nil
	}
	if p.confirm != nil {
		return []widget.Action{{Name: "confirm kill", Run: func() tea.Cmd { return p.send(procs.Term) }}}
	}
	return []widget.Action{{Name: "kill", Run: p.arm}}
}

// arm stages a kill rather than performing one. Signalling a process is not
// undoable, so it always costs two keystrokes.
func (p *Processes) arm() tea.Cmd {
	if p.list.Empty() {
		return nil
	}
	target := p.rows[p.list.Cursor()]
	p.confirm = &target
	p.status = ""
	return nil
}

// send signals the armed process. It uses the PID captured when the kill was
// armed, never the row under the cursor, so a refresh landing between the two
// keystrokes cannot retarget it.
func (p *Processes) send(sig procs.Signal) tea.Cmd {
	if p.confirm == nil {
		return nil
	}
	pid := p.confirm.PID
	p.confirm = nil
	return p.Cmd(func() tea.Msg {
		err := procs.Send(pid, sig)
		return signalledMsg{pid: pid, sig: sig, err: err}
	})
}

// follow records the PID under the cursor, which is what the selection really
// is: rows are re-sorted and re-filtered under the user, so an index alone
// would drift onto a different process.
func (p *Processes) follow() {
	if p.list.Empty() {
		p.selPID = 0
		return
	}
	p.selPID = p.rows[p.list.Cursor()].PID
}

// rebuild reapplies the filters and sort, then puts the cursor back on the
// process it was on rather than the index it was at.
func (p *Processes) rebuild() {
	rows := p.all
	if p.cfg.User != "" {
		rows = keepUser(rows, p.cfg.User)
	}
	if p.cfg.HideIdle {
		rows = keepBusy(rows)
	}
	rows = procs.Filter(rows, p.query)

	sorted := make([]procs.Process, len(rows))
	copy(sorted, rows)
	procs.SortBy(sorted, p.sort, p.reversed)
	p.rows = sorted

	p.restoreCursor()
}

// restoreCursor follows the selected PID across re-sorts and refreshes. If
// that process is gone, the cursor holds its position in the list.
func (p *Processes) restoreCursor() {
	p.list.SetLen(len(p.rows))
	if p.list.Empty() {
		p.list.Top()
		p.selPID = 0
		return
	}
	if p.selPID != 0 {
		for i, r := range p.rows {
			if r.PID == p.selPID {
				p.list.Select(i)
				return
			}
		}
	}
	p.follow()
}

func keepUser(in []procs.Process, u string) []procs.Process {
	out := make([]procs.Process, 0, len(in))
	for _, p := range in {
		if strings.EqualFold(p.User, u) {
			out = append(out, p)
		}
	}
	return out
}

func keepBusy(in []procs.Process) []procs.Process {
	out := make([]procs.Process, 0, len(in))
	for _, p := range in {
		if p.CPU > 0 {
			out = append(out, p)
		}
	}
	return out
}

func (p *Processes) scheduleTick() tea.Cmd {
	return p.Every(p.refresh, tickMsg{})
}

// sample reads the process table off the UI goroutine.
func (p *Processes) sample() tea.Cmd {
	return p.Cmd(func() tea.Msg {
		list, err := procs.Sample()
		return sampledMsg{procs: list, load: sysinfo.LoadAverage(), err: err}
	})
}
