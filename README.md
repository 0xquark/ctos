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

### `clock`

| Key | Default | Meaning |
|---|---|---|
| `format` | `15:04:05` | [Go time layout](https://pkg.go.dev/time#pkg-constants) |
| `date_format` | `Mon 02 Jan 2006` | date line below the time |
| `big` | `true` | draw large digits when there is room |
| `title` | `clock` | frame label |

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
| `title` | `notes` | frame label |

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
| `title` | `hacker news` | frame label |

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
| `title` | `processes` | frame label |

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
- **`%CPU` on macOS** is an average over the process's lifetime rather than an instantaneous
  sample, so a process that was busy an hour ago still reads high. That is what `ps` reports and
  the widget does not try to correct it.

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
