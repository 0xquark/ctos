// Package git shows the state of a set of local repositories: what branch each
// is on, how far it has drifted from its upstream, what is uncommitted, and
// how long ago anyone touched it.
package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/0xquark/ctos/internal/repos"
	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
)

func init() {
	widget.Register(widget.Spec{
		Name:    "git",
		Summary: "local repositories: stage, commit and stash without leaving the dashboard",
		New:     New,
		Example: `type: git
scan: ~/code              # find repositories under here
depth: 2                  # how far down to look
repos: []                 # or list working trees explicitly
refresh: 30s              # never polls faster than 5s
sort: activity            # activity, name, or dirty
only_interesting: false   # hide repos that are clean and in sync
command: lazygit          # what "g" opens, in the repo's directory
detail: true              # draw the selected repo beside the list
detail_columns: 0         # 0 gives that panel 55% of the width
commits: 8                # how much history the panel shows
limit: 50
title: git`,
	})
}

// defaultRefresh is slower than the machine widgets on purpose. A repository
// changes when a person does something to it, not on its own, and every poll
// is two git processes per repo.
const defaultRefresh = 30 * time.Second

// minRefresh keeps a dashboard from spawning git in a tight loop.
const minRefresh = 5 * time.Second

// defaultCommand is what `enter` opens. lazygit is the one ctOS's own docs
// name (ADR-001), and it takes the repository as its working directory.
const defaultCommand = "lazygit"

// mode is which panel has the cursor.
//
// On a wide pane both are drawn at once, side by side, and the mode says which
// one the arrow keys are moving in — the way lazygit's panels work. On a narrow
// one there is only room for the active panel, so the mode also decides what is
// on screen at all.
type mode int

const (
	// modeList is the set of repositories.
	modeList mode = iota

	// modeRepo is one repository's changed files, where the staging and
	// committing keys live. Two panels is as far as this goes: a rebase or
	// a conflict resolution is an interactive session that wants the whole
	// terminal, which is what "g" and lazygit are for (ADR-001).
	modeRepo
)

// order is how the list is sorted.
type order string

const (
	// byActivity puts the most recently committed repository first. It is
	// the default because "what was I doing?" is the question a list of
	// repositories usually answers.
	byActivity order = "activity"

	// byName is stable across refreshes, for a list read by position.
	byName order = "name"

	// byDirty puts the repositories with the most going on first.
	byDirty order = "dirty"
)

var allOrders = []order{byActivity, byName, byDirty}

type config struct {
	Repos           []string `yaml:"repos"`
	Scan            string   `yaml:"scan"`
	Depth           int      `yaml:"depth"`
	Refresh         string   `yaml:"refresh"`
	Sort            string   `yaml:"sort"`
	OnlyInteresting bool     `yaml:"only_interesting"`
	Command         string   `yaml:"command"`
	Limit           int      `yaml:"limit"`

	// Detail shows the selected repository beside the list.
	Detail bool `yaml:"detail"`
	// DetailColumns fixes the panel's width; 0 gives it a share.
	DetailColumns int `yaml:"detail_columns"`
	// Commits is how much history the panel shows.
	Commits int `yaml:"commits"`
}

// loadedMsg carries a scan back to the widget that asked for it.
type loadedMsg struct {
	repos []repos.Repo
	err   error
}

// openedMsg reports that the external tool exited, so the list can be re-read:
// the whole point of opening lazygit is to change something.
type openedMsg struct{ err error }

// commitsMsg carries one repository's history back for the detail panel.
type commitsMsg struct {
	path    string
	commits []repos.Commit
	err     error
}

// doneMsg is the result of one git operation.
type doneMsg struct {
	text string
	err  error
}

// opTimeout bounds a single operation. It is longer than a status read because
// "fetch" is on this path and has a network on the other end of it.
const opTimeout = 60 * time.Second

type tickMsg struct{}

// Git lists repositories and their state.
type Git struct {
	widget.Base
	theme theme.Theme

	paths   []string // explicit repos; empty when scanning
	scan    string
	depth   int
	refresh time.Duration
	order   order
	only    bool
	command []string
	limit   int

	detail     bool
	detailCols int
	commitsN   int

	repos  []repos.Repo
	list   widget.List
	err    error
	loaded bool

	// mode is the level on screen. openPath, not an index, identifies the
	// repository being looked at: a refresh re-sorts the list underneath
	// it, and an index would then point at a different repository.
	mode     mode
	openPath string

	// files is the cursor in the detail panel, and filesPath the repository
	// it belongs to.
	files     widget.List
	filesPath string

	// typing is the commit message box holding the keyboard, and message
	// what has been typed into it so far.
	typing  bool
	message string

	// status is the result of the last operation, shown until the next one.
	status string
	busy   bool

	// The detail panel's history, loaded for the selected repository only
	// and cached by path: a third git command per repository per refresh is
	// not worth paying for the ones nobody is looking at.
	commits     []repos.Commit
	commitsPath string
	commitsErr  error

	// inflight guards against a tick arriving while a scan is still
	// running: two git processes per repo is enough without stacking them.
	inflight bool
}

// New builds a git widget from its dashboard configuration.
func New(ctx widget.Context) (widget.Widget, error) {
	cfg := config{Depth: 2, Limit: 50, Command: defaultCommand, Detail: true, Commits: 8}
	if err := ctx.Decode(&cfg); err != nil {
		return nil, err
	}

	if len(cfg.Repos) == 0 && cfg.Scan == "" {
		return nil, errors.New("one of \"repos:\" or \"scan:\" is required")
	}
	if len(cfg.Repos) > 0 && cfg.Scan != "" {
		return nil, errors.New("set \"repos:\" or \"scan:\", not both")
	}
	if cfg.Depth < 0 {
		return nil, fmt.Errorf("\"depth:\" cannot be negative (got %d)", cfg.Depth)
	}
	if cfg.Limit <= 0 {
		cfg.Limit = 50
	}
	if cfg.Commits <= 0 {
		cfg.Commits = 8
	}
	if cfg.DetailColumns < 0 {
		return nil, fmt.Errorf("\"detail_columns:\" cannot be negative (got %d)", cfg.DetailColumns)
	}

	refresh, err := ctx.Refresh(cfg.Refresh, defaultRefresh, minRefresh)
	if err != nil {
		return nil, err
	}

	ord, err := parseOrder(cfg.Sort)
	if err != nil {
		return nil, err
	}

	// The command is split like the editor is, so it can carry flags.
	command := strings.Fields(cfg.Command)
	if len(command) == 0 {
		command = []string{defaultCommand}
	}

	return &Git{
		theme:      ctx.Theme,
		paths:      slices.Clone(cfg.Repos),
		scan:       cfg.Scan,
		depth:      cfg.Depth,
		refresh:    refresh,
		order:      ord,
		only:       cfg.OnlyInteresting,
		command:    command,
		limit:      cfg.Limit,
		detail:     cfg.Detail,
		detailCols: cfg.DetailColumns,
		commitsN:   cfg.Commits,
		loaded:     true,
	}, nil
}

func parseOrder(name string) (order, error) {
	if name == "" {
		return byActivity, nil
	}
	o := order(strings.ToLower(strings.TrimSpace(name)))
	if !slices.Contains(allOrders, o) {
		return "", fmt.Errorf("unknown sort %q (valid: activity, name, dirty)", name)
	}
	return o, nil
}

// Init kicks off the first scan and starts the refresh tick.
func (g *Git) Init() tea.Cmd {
	return tea.Batch(g.read(), g.Every(g.refresh, tickMsg{}))
}

// Update handles navigation, local keys and scan results.
func (g *Git) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case loadedMsg:
		g.inflight = false
		g.loaded = true
		g.err = msg.err
		if msg.err == nil {
			g.repos = msg.repos
			g.apply()
		}
		// A commit changes the history the panel is showing, and so does
		// arriving at a repository for the first time.
		g.commitsPath = ""
		return g.syncDetail()

	case openedMsg:
		return g.read()

	case tickMsg:
		next := g.Every(g.refresh, tickMsg{})
		if g.inflight {
			return next
		}
		return tea.Batch(g.read(), next)

	case doneMsg:
		g.busy = false
		g.status = msg.text
		if msg.err != nil {
			g.status = msg.err.Error()
		}
		return g.read()

	case commitsMsg:
		// A slower read may land after the cursor has moved on.
		if msg.path == g.shownPath() {
			g.commitsPath, g.commits, g.commitsErr = msg.path, msg.commits, msg.err
		}
		return nil

	case tea.KeyMsg:
		switch {
		case g.typing:
			return g.messageKey(msg)
		case g.mode == modeRepo:
			return g.repoKey(msg)
		default:
			return g.listKey(msg)
		}
	}
	return nil
}

// GrabsKeys claims the keyboard while a commit message is being typed.
func (g *Git) GrabsKeys() bool { return g.typing }

// listKey handles the repository list.
func (g *Git) listKey(msg tea.KeyMsg) tea.Cmd {
	if g.list.HandleKey(msg, g.listHeight()) {
		g.status = ""
		return g.syncDetail()
	}
	switch msg.String() {
	case "r":
		return g.read()
	case "s":
		// Cycle the sort, the way the processes widget does.
		g.order = allOrders[(slices.Index(allOrders, g.order)+1)%len(allOrders)]
		g.apply()
		g.list.Top()
		return g.syncDetail()
	case "i":
		g.only = !g.only
		g.apply()
		g.list.Top()
		return g.syncDetail()
	case "enter", "right", "l":
		g.enter()
		return g.syncDetail()
	case "g":
		return g.launch()
	}
	return nil
}

// syncFiles points the file cursor at the repository on show.
//
// Moving the cursor in the repository list resets it, or the next stage
// keystroke would act on the file at the old index of a different repository.
// It is separate from syncDetail because it is pure state with no command, and
// anything that changes the selection has to do it.
func (g *Git) syncFiles() {
	r, ok := g.target()
	if !ok {
		g.files, g.filesPath = widget.List{}, ""
		return
	}
	if r.Path != g.filesPath {
		g.files, g.filesPath = widget.List{}, r.Path
	}
	g.files.SetLen(len(r.Files))
}

// shownPath is the repository the detail panel is describing: the one being
// worked in, or the one under the cursor in the list.
func (g *Git) shownPath() string {
	if r, ok := g.target(); ok {
		return r.Path
	}
	return ""
}

// syncDetail points the detail panel at whatever is now on show: the file list
// it draws, and the history it has to fetch.
//
// It is called after anything that can change which repository that is — a
// cursor move, a re-sort, a refresh — because the panel describes the
// selection, not the drilled-into repository.
func (g *Git) syncDetail() tea.Cmd {
	g.syncFiles()

	path := g.shownPath()
	if path == "" || path == g.commitsPath {
		return nil
	}
	// Clear first: the panel must not caption one repository's history with
	// another's name while the read is in flight.
	g.commitsPath, g.commits, g.commitsErr = "", nil, nil

	n := g.commitsN
	return g.Cmd(func() tea.Msg {
		commits, err := repos.Log(context.Background(), path, n)
		return commitsMsg{path: path, commits: commits, err: err}
	})
}

// repoKey handles one repository's file list.
func (g *Git) repoKey(msg tea.KeyMsg) tea.Cmd {
	if g.files.HandleKey(msg, g.listHeight()) {
		g.status = ""
		return nil
	}
	// One operation at a time. Two git processes writing the same index is
	// how a repository ends up with a stale lock file.
	if g.busy && msg.String() != "esc" {
		return nil
	}

	switch msg.String() {
	case "esc", "left", "h":
		g.leave()
		return g.syncDetail()

	case "enter", " ":
		return g.toggleFile()

	case "a":
		return g.do("staged everything", func(ctx context.Context, path string) error {
			return repos.Stage(ctx, path)
		})

	case "u":
		return g.do("unstaged everything", func(ctx context.Context, path string) error {
			return repos.Unstage(ctx, path)
		})

	case "c":
		g.typing, g.message, g.status = true, "", ""
		return nil

	case "S":
		return g.do("stashed", repos.Stash)

	case "p":
		return g.do("popped the stash", repos.StashPop)

	case "f":
		return g.do("fetched", repos.Fetch)

	case "r":
		return g.read()

	case "g":
		return g.launch()
	}
	return nil
}

// messageKey handles the commit box while it owns the keyboard.
func (g *Git) messageKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEnter:
		message := g.message
		g.typing, g.message = false, ""
		return g.do("committed", func(ctx context.Context, path string) error {
			return repos.CommitStaged(ctx, path, message)
		})
	case tea.KeyEsc:
		g.typing, g.message = false, ""
	case tea.KeyBackspace:
		if r := []rune(g.message); len(r) > 0 {
			g.message = string(r[:len(r)-1])
		}
	case tea.KeyRunes:
		g.message += string(msg.Runes)
	case tea.KeySpace:
		g.message += " "
	}
	return nil
}

// enter drills into the selected repository.
func (g *Git) enter() {
	list := g.visible()
	if g.list.Empty() || g.list.Cursor() >= len(list) {
		return
	}
	r := list[g.list.Cursor()]
	if r.Err != nil {
		// There is nothing inside a path git could not read.
		g.status = r.Err.Error()
		return
	}
	g.mode, g.openPath, g.status = modeRepo, r.Path, ""
	g.syncFiles()
}

// leave goes back to the repository list.
func (g *Git) leave() {
	g.mode, g.openPath, g.status = modeList, "", ""
	g.typing, g.message = false, ""
}

// current is the repository being looked at, re-resolved by path because a
// refresh re-sorts the list underneath it.
func (g *Git) current() (repos.Repo, bool) {
	for _, r := range g.repos {
		if r.Path == g.openPath {
			return r, true
		}
	}
	return repos.Repo{}, false
}

// selectedFile is the entry under the cursor in the file list.
//
// It follows the repository on show rather than the one entered, because that
// is what is drawn: with both panels up, the file cursor moves as the list
// cursor does. Only the staging keys act on it, and those are reachable only
// once the cursor is in the panel, where the two are the same repository.
func (g *Git) selectedFile() (repos.File, bool) {
	r, ok := g.target()
	if !ok || g.files.Empty() || g.files.Cursor() >= len(r.Files) {
		return repos.File{}, false
	}
	return r.Files[g.files.Cursor()], true
}

// toggleFile stages the selected file, or unstages it if it is already staged.
func (g *Git) toggleFile() tea.Cmd {
	f, ok := g.selectedFile()
	if !ok {
		return nil
	}
	if f.IsStaged() {
		return g.do("unstaged "+f.Path, func(ctx context.Context, path string) error {
			return repos.Unstage(ctx, path, f.Path)
		})
	}
	return g.do("staged "+f.Path, func(ctx context.Context, path string) error {
		return repos.Stage(ctx, path, f.Path)
	})
}

// do runs one operation against the open repository off the UI goroutine and
// re-reads the list when it finishes.
func (g *Git) do(done string, op func(context.Context, string) error) tea.Cmd {
	path := g.openPath
	if path == "" {
		return nil
	}
	g.busy, g.status = true, ""

	return g.Cmd(func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		if err := op(ctx, path); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: done}
	})
}

// Actions names what enter does, which depends on the level: opening a
// repository from the list, or staging the file under the cursor inside one.
func (g *Git) Actions() []widget.Action {
	if g.mode == modeRepo {
		f, ok := g.selectedFile()
		if !ok {
			return nil
		}
		name := "stage"
		if f.IsStaged() {
			name = "unstage"
		}
		return []widget.Action{{Name: name, Run: g.toggleFile}}
	}
	if g.list.Empty() {
		return nil
	}
	return []widget.Action{{Name: "files", Run: func() tea.Cmd { g.enter(); return g.syncDetail() }}}
}

// launch hands the terminal to the external tool, in the repository's
// directory, and re-reads the list when it exits.
//
// It is where everything this widget deliberately does not do goes: a rebase,
// a conflict, an interactive add. Those want a full screen and a program built
// for them, not four keys in a dashboard pane.
func (g *Git) launch() tea.Cmd {
	repo, ok := g.target()
	if !ok {
		return nil
	}

	if _, err := exec.LookPath(g.command[0]); err != nil {
		// Refusing with a reason beats handing bubbletea a command that
		// cannot start, which leaves the screen in the tool's state.
		g.status = fmt.Sprintf("%s is not installed, or not on $PATH", g.command[0])
		return nil
	}

	c := exec.Command(g.command[0], g.command[1:]...)
	c.Dir = repo.Path
	// ExecProcess's callback is bubbletea's, not ours, so the result is
	// addressed by hand rather than through Cmd.
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return g.Address(openedMsg{err: err})
	})
}

// target is the repository a repository-level key applies to: the one being
// looked at, or the one under the cursor in the list.
func (g *Git) target() (repos.Repo, bool) {
	if g.mode == modeRepo {
		return g.current()
	}
	list := g.visible()
	if g.list.Empty() || g.list.Cursor() >= len(list) {
		return repos.Repo{}, false
	}
	return list[g.list.Cursor()], true
}

// read scans the repositories off the UI goroutine.
func (g *Git) read() tea.Cmd {
	g.inflight = true
	paths, scan, depth, limit := slices.Clone(g.paths), g.scan, g.depth, g.limit

	return g.Cmd(func() tea.Msg {
		if scan != "" {
			found, err := repos.Discover(scan, depth)
			if err != nil {
				return loadedMsg{err: err}
			}
			// Discovery comes back in path order, so cutting it here
			// would keep the alphabetically-first repositories. The
			// limit is meant to bound the work, not to decide which
			// projects the user cares about.
			paths = repos.MostRecent(found, limit)
		}
		// An explicit list is the order the user wrote, and that is a
		// statement about priority; the tail is what goes.
		if len(paths) > limit {
			paths = paths[:limit]
		}
		return loadedMsg{repos: readAll(paths)}
	})
}

// readAll reads every repository at once. They are independent, and a dozen
// repos read one after another is a dozen round trips the user waits through —
// which will matter more in v0.2, where each one is an ssh connection.
func readAll(paths []string) []repos.Repo {
	out := make([]repos.Repo, len(paths))
	done := make(chan struct{}, len(paths))
	ctx := context.Background()

	for i, path := range paths {
		go func() {
			out[i] = repos.Status(ctx, path)
			done <- struct{}{}
		}()
	}
	for range paths {
		<-done
	}
	return out
}

// apply re-sorts and re-filters after a scan or a key.
func (g *Git) apply() {
	g.sort()
	g.list.SetLen(len(g.visible()))

	// A repository that has gone away — deleted, or filtered out of a scan
	// — cannot still be open. Staging a file in it would be staging into
	// nothing.
	if g.mode == modeRepo {
		if _, ok := g.current(); !ok {
			g.leave()
		}
	}
	// The file list is re-pointed rather than re-created, so a refresh
	// mid-staging leaves the cursor where the user put it.
	g.syncFiles()
}

// visible is the repositories the list is currently showing.
func (g *Git) visible() []repos.Repo {
	if !g.only {
		return g.repos
	}
	out := make([]repos.Repo, 0, len(g.repos))
	for _, r := range g.repos {
		// An unreadable repository is interesting by definition: it is
		// the one thing on the list the user may need to act on.
		if r.Err != nil || !r.Clean() || !r.Synced() {
			out = append(out, r)
		}
	}
	return out
}

func (g *Git) sort() {
	sort.SliceStable(g.repos, func(i, j int) bool {
		a, b := g.repos[i], g.repos[j]
		switch g.order {
		case byName:
			return a.Name < b.Name
		case byDirty:
			if a.Dirty() != b.Dirty() {
				return a.Dirty() > b.Dirty()
			}
			return a.Ahead+a.Behind > b.Ahead+b.Behind
		default:
			return a.Last.After(b.Last)
		}
	})
}
