# ctOS

**Your terminal's central operating system.**

ctOS is a terminal control plane. You compose dashboards from widgets in YAML, switch between
them, and act on what you see; edit a note, open a story, kill a process. When you need a real
tool, an action hands the whole terminal to it and gives it back when you quit.

> Status: **v0.1** early. The scaffold works; remote monitoring over SSH is next.

```
╭─ clock ────────────────────────────────────────╮╭─ notes ────────────────────────────────────────╮
│                                                ││ ▸ today.md                                  2m │
│                                                ││   standup.md                                1h │
│                                                ││   ctos-ideas.md                             2h │
│                                                ││ ────────────────────────────────────────────── │
│          ╷ ╭─╴     ╭─╮ ╭─╮     ╷ ╷ ╭─╴         ││ # Wednesday                                    │
│          │ ├─╮  ▪   ─┤ │ │  ▪  ╰─┤ ╰─╮         ││                                                │
│          ╵ ╰─╯     ╰─╯ ╰─╯       ╵ ╰─╯         ││ • shipped the notes preview                    │
│                                                ││ • fixed the marker width bug                   │
│                Wed 26 Aug 2026                 ││                                                │
│                                                ││ > layout mode next                             │
╰────────────────────────────────────────────────╯╰────────────────────────────────────────────────╯
╭─ hacker news ────────────────────────────────────────────────────────────────────────────────────╮
│  1 ▸ Show HN: ctOS, a terminal control plane                                                     │
│      412 pts · 137 comments · github.com · 3h                                                    │
│  2   Ask HN: What is your terminal setup?                                                        │
│      88 pts · 210 comments · 5h                                                                  │
╰──────────────────────────────────────────────────────────────────────────────────────────────────╯
 tab focus  ·  ↑↓ move  ·  enter edit  ·  ? help  ·  q quit
```

## Install

```sh
go install github.com/0xquark/ctos/cmd/ctos@latest
```

Or from source:

```sh
git clone https://github.com/0xquark/ctos
cd ctos
make build && ./ctos
```

## Quick start

```sh
ctos init    # write a starter config
ctos         # open your dashboard
```

`ctos init` creates `~/.config/ctos/config.yaml` and `~/.config/ctos/dashboards/home.yaml`.
Edit them and restart.

## Controls

| Key | Does |
|---|---|
| `tab` / `shift+tab` | move focus between widgets |
| `↑` `↓` (or `k` `j`) | navigate inside the focused widget |
| `enter` | the focused widget's primary action |
| `r` | refresh the focused widget |
| `ctrl+l` | rearrange the layout |
| `?` | toggle full help |
| `q` | quit |

The same key does the same thing in every widget.

Widgets may add their own keys on top — the `processes` widget uses `c`/`m`/`p`/`n` to sort,
`/` to filter and `d`/`l` for its detail pane. While a widget is taking text input, it owns the whole keyboard, so `q` types
a `q` instead of quitting; `esc` gets you out.

### Status bar

A dashboard can pin widgets above the grid as a frameless strip:

```yaml
widgets:
  vitals:
    type: system
    style: bar
  clock:
    type: clock

bar:
  left: [vitals]
  right: [clock]

rows:
  - [notes, processes]
```

`bar: [vitals]` is the short form when nothing needs to sit on the right. The right-hand group is
measured first and the left gets what is left over, because the right is the fixed part: a clock
is as wide as a clock, while a vitals strip fills whatever it is given. It is capped at a third
of the width, since it is a trailing detail rather than the point of the bar.

```
 CPU 13.0% │ MEM █████▒▒░ 66% 15.9G/24.0G │ SWP 45% 1.3G/3.0G │ / 67% 8.4G free │ LOAD 3.12 2.92 2.61        Sun 30 Aug  15:57:18
```

The bar is chrome, not a pane. It has no border and no title, `tab` never lands on it, and
`ctrl+l` cannot move it — a widget with no cursor and no actions is not somewhere focus should
be able to get stuck. It is as tall as its contents need, up to three lines, and the rows below
give back exactly that much: on a terminal too short for both, the dashboard keeps a row and the
bar is the thing that shrinks. `system` in `style: bar` always asks for exactly one.

Any widget type can go there. `system` with `style: bar` is what the left-hand side is for, and
the `clock` collapses to a single line of text when it is one line tall — which is where a
terminal expects the time to be, and a great deal less of the screen than a pane of block digits.

### Rearranging the layout

Press `ctrl+l` to pick up the focused widget and move it around:

| Key | Does |
|---|---|
| `←` `→` | reorder within the current row |
| `↑` `↓` | move to the row above or below, keeping the column |
| `enter` | split out into a new row |
| `tab` | pick a different widget to move |
| `s` | save the arrangement back to the dashboard file |
| `esc` | cancel and restore the previous layout |

Saving rewrites only the `rows:` key. Your comments, widget settings and `${VAR}` references
are left exactly as you wrote them.

## Configuration

Two files, both optional — ctOS runs on defaults without them.

**`~/.config/ctos/config.yaml`** — global settings:

```yaml
editor: ${EDITOR:-vi}       # opens files; falls back to $EDITOR, then vi
default_dashboard: home
theme:
  accent: "#ff6b35"
refresh:
  default: 30s              # for widgets that set no interval
```

**`~/.config/ctos/dashboards/home.yaml`** — one dashboard:

```yaml
name: home

widgets:
  clock:
    type: clock
    format: "15:04:05"
  notes:
    type: notes
    path: ~/notes
  hackernews:
    type: hackernews
    limit: 20
    refresh: 5m

rows:
  - [clock, notes]
  - [hackernews]
```

Widgets are named under `widgets:`, then arranged by `rows:`. Widgets sharing a row split the
width evenly. Omit `rows:` and each widget gets its own full-width row.

Any string value may reference the environment as `${VAR}` or `${VAR:-default}`, so dashboards
stay shareable and secrets stay out of the file.

### Config location

Resolved in this order:

1. `--config-dir <path>`
2. `$CTOS_CONFIG_DIR`
3. `~/.ctos` — with `--home-config`
4. `$XDG_CONFIG_HOME/ctos`
5. `~/.config/ctos`
6. `~/.ctos` — if it already exists

## Widgets

Run `ctos widgets` to list what your build supports, and `ctos widgets <type>` for one
widget's keys as a block you can paste into a dashboard.

Every widget takes `title`, the label drawn on its frame. It defaults to the type's own name
(`hacker news` for `hackernews`), and `title: ""` leaves the frame bare. The keys below are
the ones each type adds.

### `clock`

| Key | Default | Meaning |
|---|---|---|
| `format` | `15:04:05` | [Go time layout](https://pkg.go.dev/time#pkg-constants) |
| `date_format` | `Mon 02 Jan 2006` | date, below the time in a pane and before it on one line |
| `big` | `true` | draw large digits when there is room |

Given one line — in the [status bar](#status-bar), or a pane one row tall — it renders as
`Mon 02 Jan  15:04:05` and nothing else, with no padding, so that whatever placed it can align
it. The date is dropped rather than truncated when there is no room, since half a date is worse
than none and the time is the point.

### `system`

The machine at a glance, in one of two styles. `style: rows` is a panel — one labelled bar per
vital, with a sparkline behind the ones that move. `style: bar` is a status strip: pipe-separated
values across the width of the terminal, meant for the dashboard's [status bar](#status-bar).

| Key | Default | Meaning |
|---|---|---|
| `style` | `rows` | `rows` for the panel, `bar` for the status strip |
| `refresh` | `3s` | poll interval, minimum `1s` |
| `metrics` | see below | what to show, in order: `cpu`, `mem`, `swap`, `disk`, `diskio`, `net`, `load`, `top`, `uptime` |
| `disks` | `["/"]` | one entry per mount point; `[]` for none |
| `interface` | *(all but loopback)* | network interface to measure |
| `history` | `true` | draw the sparklines |
| `deltas` | `true` | show each value's change over the last 30s (`bar` style) |

`metrics` defaults to everything in the `bar` style. In the `rows` style it leaves out `diskio`
and `top`, which have no magnitude to draw a bar against and would be two rows of bare text in a
column of bars.

```
 cpu  ▁▂▃▄▄▃▂▂ ████░░░░░░░░░░░░   22% 17 us · 5 sy
 mem  ▅▆▆▆▆▆▆▆ ███████████░░░░░   67% 16G/24G
 swap          ███████████░░░░░   67% 2G/3G
 /             ██████████░░░░░░   64% 10G free
 net  ▂▃▆█▇▅▄▄ ↓ 26K/s  ↑ 48K/s
 load ▂▃▃▃▃▃▃▃ ██████░░░░░░░░░░  4.62 3.76 3.33 · 12 cores
 up            23h 57m · delorean
```

Columns drop as the pane narrows, in the order a glance can spare them: the detail text on the
right, then the sparklines, then the bars, leaving the labels and the numbers. A pane too short
for a row each packs the same numbers onto fewer lines rather than hiding the metrics that did
not fit.

**`style: bar`** is the ticker form, and the reason the `bar:` slot exists. It is always exactly
one line — a strip that wraps stops being a strip, because it pushes the dashboard down as the
machine gets busier and the second line gets read as a continuation rather than at a glance.

```
 CPU 10.0% │ MEM ██████▒░ 69% 16.6G/24.0G │ SWP 45% 1.4G/3.0G │ / 66% 8.5G free │ DISK 51K/s │ NET ↓15K/s ↑31K/s │ LOAD 3.82 3.43 2.78 │ TOP CPU WindowServer 9.3% │ TOP MEM Arc 769.5M
```

Fitting a machine's vitals into one line of unknown width is the whole problem, and it is solved
the way the process table solves its columns: **every value knows how to render itself at several
levels of detail and carries a priority**, and the bar gives up detail on its least important
values before it gives up any value at all. Narrowing the terminal walks down this list, taking a
whole tier apart — shortening it, then removing it — before touching anything above it:

| | |
|---|---|
| never dropped | `cpu`, `mem`, `load`, disk usage |
| given up first ↑ | swap, top CPU process, network, disk throughput, top memory process |
| given up first ↓ | the memory breakdown, then uptime |

```
 200  CPU 10.0% │ MEM ██████▒░ 69% 16.6G/24.0G │ SWP 45% 1.4G/3.0G │ / 66% 8.5G free │ DISK 51K/s │ NET ↓15K/s ↑31K/s │ LOAD 3.82 3.43 2.78 │ TOP CPU WindowServer 9.3%
 120  CPU 10.0% │ MEM ██████▒░ 69% 16.6G/24.0G │ SWP 45% 1.4G/3.0G │ / 66% 8.5G free │ LOAD 3.82 3.43 2.78 │ TOP WindowServer 9%
  80  CPU 10.0% │ MEM ██████▒░ 69% 16.6G/24.0G │ / 66% 8.5G │ LOAD 3.82 3.43 2.78
  40  CPU 10.0% │ MEM ██████▒░ 69%
```

**The memory bar** is the breakdown drawn rather than spelled out, ordered from what the kernel
can never hand back through to what is already free: wired in red, compressed in amber, the rest
of what is in use in the accent colour, then cache and free in two dimmer glyphs. The glyphs
differ as well as the colours, so it still reads on a terminal that is not showing colour. Eight
cells carry what the numbers need forty for, which is why the bar survives a narrow terminal that
`wired 3.0G  comp 8.0G` does not.

The numbers themselves are whatever the platform publishes, and no more. Linux has no wired or
compressed figure and macOS has no buffers figure, so a category appears only where there is a
real number behind it rather than a zero standing in for one. Squeezed, the breakdown keeps its
two largest parts — the two doing the most to explain the headline percentage.

**Value hierarchy.** In each group only the number that matters is bright and colour-coded; the
supporting figures are a shade back. `LOAD 3.82 3.43 2.78` is one reading and two pieces of
context, not three equal numbers, and rendering it that way is what lets the eye cross the whole
bar in one pass and stop only where it should.

**Deltas** are each value's change over the last 30 seconds, drawn only when the movement clears
a floor — a machine at rest jitters by a tenth of a percent every tick, and an arrow for that is
noise dressed as information. The colours are the opposite of a financial ticker's on purpose:
rising is amber and falling is green, because on this bar a climbing number means the machine is
working harder.

The bars are coloured against thresholds — green, amber, red. Swap's are lower than memory's,
because paging is a symptom before it is a problem, and load is judged per core, so the bar
reads full when there is one runnable task for every core.

**What the numbers mean.** Memory excludes the caches the kernel will hand back on demand:
`MemAvailable` on Linux, and active-plus-wired-plus-compressed on macOS, which is the figure
Activity Monitor calls "Memory Used". Counting the caches would pin the gauge near full on
every healthy machine. Disk usage is used over used-plus-available — `df`'s own `capacity`
column — because a filesystem's reported total includes blocks nothing can allocate; on a
sealed macOS system volume that gap is the difference between 64% and 4% (see
[ADR-025](docs/DECISIONS.md)).

**Platforms.** Vitals are read from what the system already publishes rather than from a
metrics library, so the same parsers will serve remote hosts over SSH in v0.2
([ADR-023](docs/DECISIONS.md)).

| | Linux | macOS |
|---|---|---|
| CPU | `/proc/stat`, free | `iostat`, and it takes a second — see below |
| Memory, swap | `/proc/meminfo` | `vm_stat`, `sysctl vm.swapusage` |
| Memory breakdown | free, cached, buffers | free, cached, wired, compressed |
| Disk usage | `df` | `df` |
| Disk throughput | `/proc/diskstats`, read/write split | `iostat`, combined only |
| Network | `/proc/net/dev` | `netstat -ibn` |
| Load, uptime | `/proc/loadavg`, `/proc/uptime` | `sysctl` |

macOS has no cumulative CPU counter a program can read, so the reading comes from `iostat`,
which measures a one-second interval on our behalf. That is why the refresh floor is `1s` and
why a tick arriving while a sample is still running is dropped rather than queued. Linux
differences `/proc/stat` against the previous tick and pays nothing, beyond 250ms on the very
first sample, where there is no previous reading to difference against.

A metric this platform cannot answer for is a row that is not drawn, rather than a row of
dashes: swap that is switched off says `off`, because that is a fact worth knowing about the
machine.

### `notes`

Lists files newest-first. `enter` opens the selected file in your editor.

| Key | Default | Meaning |
|---|---|---|
| `path` | *(required)* | directory to list |
| `recursive` | `false` | descend into sub-directories |
| `extensions` | `[".md", ".txt"]` | which files to list |
| `limit` | `200` | maximum entries |
| `preview` | `true` | show the selected note's contents below the list |
| `preview_lines` | `0` | rows given to the preview; `0` splits the widget in half |

The preview applies light markdown styling such as headings, bullets, quotes and code fences  and
refuses to print binary files. It disappears automatically when the widget is under 8 rows
tall.

### `hackernews`

Front page via the [Algolia API](https://hn.algolia.com/api). `enter` opens the story in your
browser.

| Key | Default | Meaning |
|---|---|---|
| `limit` | `20` | stories to fetch (max 100) |
| `refresh` | global default | poll interval, minimum `1m` |

### `processes`

The local process table, htop-style, with a detail pane below it.

| Key | Default | Meaning |
|---|---|---|
| `sort` | `cpu` | initial order: `cpu`, `mem`, `pid` or `name` |
| `refresh` | `3s` | poll interval, minimum `500ms` |
| `user` | *(all users)* | only this user's processes; `me` means the current user |
| `filter` | *(none)* | initial filter query |
| `hide_idle` | `false` | drop processes using no CPU |
| `detail` | `true` | show the detail pane under the list |
| `detail_lines` | `0` | rows given to the detail pane; `0` splits the space in half |
| `log_window` | `5m` | how far back the log view looks |

Keys, while the widget is focused:

| Key | Does |
|---|---|
| `c` `m` `p` `n` | sort by CPU, memory, PID or name — press again to reverse |
| `s` | cycle through those four |
| `/` | filter by command, user or PID |
| `d` | show or hide the detail pane |
| `l` | switch the detail pane between ancestry and logs |
| `enter` | kill the selected process (asks first) |
| `r` | refresh now |

The active column is marked with an arrow showing which way the rows actually run, and columns
drop as the pane narrows — least useful first — so CPU% and the command are always visible.

**The detail pane** shows the selected process's full command, expanded state, memory and start
time, then its place in the process tree: the chain of parents up to `init`/`launchd`, and its
own children. Press `l` and the pane shows that process's recent log lines instead, from `log
show` on macOS or `journalctl` on Linux. Logging is queried on demand rather than on the refresh
tick, because macOS scans a binary store to answer and takes about a second. A process that
writes to stdout under a service manager often has nothing there — the pane says so rather than
looking broken.

**Killing** takes two keystrokes: `enter` arms it and names what will be signalled, then `enter`
sends `SIGTERM` or `k` sends `SIGKILL`. `esc` cancels. The armed process is remembered by PID, so
a refresh that re-sorts the table between the two keystrokes cannot retarget the kill.

Changing the sort jumps to the top of the new order, since that is the reason to change it. A
plain refresh keeps the cursor on whatever process you had selected, even if it moved rows.

**Platforms.** The widget reads the process table with the system `ps`, and is verified on macOS
and on Linux with procps (Debian 13 / procps-ng 4.0.4). The BSDs should work; same `ps` flags;
but are untested.

| | Process table | Load average | Logs |
|---|---|---|---|
| macOS | yes | yes | `log show` |
| Linux, procps | yes | `/proc/loadavg` | `journalctl`, when present |
| Linux, BusyBox only | **no** — see below | yes | no |
| Windows | no | no | no |

Two caveats worth knowing:

- **BusyBox `ps`** (Alpine's default) has no `%CPU` or `%MEM` columns, so there is no process
  table to build. The widget says so and names the fix: `apk add procps`.
- **`%CPU` on macOS** is a decaying average over up to a minute of real time, not an
  instantaneous sample, so a process that was busy thirty seconds ago still reads high. That is
  what `ps` reports and the widget does not try to correct it. On Linux the same column is an
  average over the process's whole lifetime, which is blunter still. Neither sums to the
  machine's actual CPU usage, which is why the `system` widget measures that separately
  ([ADR-024](docs/DECISIONS.md)).

On a system without systemd, `journalctl` is absent and the log view says so rather than
failing. Everything else keeps working.

## Why not just use tmux?

ctOS owns its widgets rather than tiling other people's TUIs, because a pane of `htop` is an
opaque rectangle. ctOS can't know what's selected in it, attach actions to it, or combine its
data with anything else.

The `processes` widget is the first half of that argument: because ctOS parses the process table
itself, the table can be filtered, sorted, sized to a quarter of the screen, and acted on with
the same keys as every other widget. The second half arrives in v0.2 — one merged, sorted
process table across every machine you watch, instead of four panes each running `ssh vm-N htop`.

## License

MIT — see [LICENSE](LICENSE).
