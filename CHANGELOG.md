# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/). Before 1.0, breaking changes bump the minor version.

## [Unreleased]

### Added

- **`git` widget.** The state of a set of local repositories — branch, distance from upstream,
  uncommitted work, and how long ago anyone touched them — with a detail panel beside the list
  showing the selected repository's changed files and recent commits. `enter` moves the cursor
  into that panel, where `enter`/`space` stage and unstage a file, `a`/`u` do all of it, `c`
  commits through a message box, `S`/`p` stash and pop, and `f` fetches. `g` hands the whole
  terminal to `lazygit` for the interactive work — a rebase or a conflict wants a full screen
  and a program built for it. Point it at a directory with `scan:` or list working trees with
  `repos:`; `s` cycles the sort, `i` hides everything clean and in sync. At one line tall it
  renders a strip of only the repositories that want attention, for the status bar.
- **`system` widget.** CPU, memory, swap, disk, network, load and uptime, as a panel of labelled
  bars with sparklines or as a one-line status strip. `style: auto` picks from the shape of the
  box, so the same widget reads correctly in a pane, across the top of the screen and down its
  side; in a narrow, tall column each metric is drawn down the page with a full-width history
  line. Read from `/proc` and the stock BSD tools rather than a metrics library.
- **`processes` widget.** A live process table you can sort (`c`/`m`/`p`/`n`), filter (`/`) and
  kill from, with a detail pane showing the process ancestry or its recent logs (`d`/`l`).
- **Status bar.** A dashboard can pin frameless widgets to an edge with `bar:` — no border, no
  focus, not part of the grid. `position:` puts it at the `top` (default), `bottom`, `left` or
  `right`, with two groups whose keys follow the orientation: `left:`/`right:` across,
  `top:`/`bottom:` down. In layout mode, `b` cycles the four edges and `s` saves it. A vertical
  bar takes a `width:`, defaulting to 24 columns.
- The starter dashboard now opens with a vitals strip and the clock in the status bar, and
  `notes`, `processes` and `hackernews` in the grid.
- Initial scaffold: a bubbletea dashboard that loads YAML and renders widgets in a row layout.
- Config: `config.yaml` for global settings, `dashboards/*.yaml` for dashboards. Resolution via
  `--config-dir`, `$CTOS_CONFIG_DIR`, `--home-config`, `$XDG_CONFIG_HOME`, `~/.config/ctos`,
  falling back to an existing `~/.ctos`.
- `${VAR}` and `${VAR:-default}` expansion, plus leading `~`, in all config strings.
- Widgets: `clock` (block digits), `notes` (list a directory, open in `$EDITOR`),
  `hackernews` (Algolia front page, open in browser).
- Focus model: `tab` / `shift+tab` between widgets, `↑` `↓` within one, `enter` for the primary
  action, `?` for full help.
- Commands: `ctos init`, `ctos dashboards`, `ctos widgets`.
- Layout mode (`ctrl+l`): move the focused widget with the arrows, `enter` to split it into a
  new row, `s` to save the arrangement back to the dashboard file, `esc` to cancel. Saving
  rewrites only the `rows:` key, preserving comments, widget settings and `${VAR}` references.
- Notes preview pane: the selected note's contents render below the list, with light markdown
  styling. Configurable via `preview` and `preview_lines`.

### Changed

- **Widget API.** Adding a widget now takes noticeably less boilerplate, and the two things a
  widget author could silently get wrong are gone.
  - A widget's async results are addressed to it: build commands with `Base.Cmd`, `Base.Tick`
    or `Base.Every` and the dashboard delivers the result to that widget alone. Message types
    no longer carry an id field, and `Update` no longer filters. Two widgets of the same type
    on one dashboard can no longer consume each other's messages.
  - `Context.Decode` replaces `ctx.Node.Decode` and is strict: a misspelled key in a dashboard
    is an error naming the line and the valid keys, instead of being ignored.
  - `widget.List` supplies the cursor, the scroll window and the standard navigation keys for
    list-shaped widgets.
  - `Update` returns `tea.Cmd` alone; widgets are pointers and mutate in place.
  - `widget.Register` takes a `widget.Spec` carrying a summary and an example config, and
    refuses a type with no summary.
  - `title:` is handled by the shell, like `type:`. It is resolved once by the registry and
    returned by `widget.Base`, so widgets no longer declare a title key or a `Title()` method.
    `title: ""` now leaves a frame unlabelled instead of falling back to a default.
- `ctos widgets` now lists each type with a one-line summary; `ctos widgets <type>` prints a
  paste-ready configuration block.
- The `clock` renders as a single unpadded line of text when it is one row tall, so it can sit
  in the status bar where a terminal expects the time, instead of taking a pane of block digits.
- Layout mode saves the status bar's position alongside the rows. `config.SaveRows` became
  `config.SaveLayout` and rewrites both keys, still preserving comments, widget settings and
  `${VAR}` references.

### Fixed

- `humanize.Truncate` counts runes and is not ANSI-aware, so cutting an already-styled string
  counted escape sequences as characters. Every cut of styled text now goes through
  `ansi.Truncate`.
- The `git` widget's `limit:` cut a path-sorted list, keeping the alphabetically-first
  repositories rather than the ones being worked in. It now cuts by how recently each was
  touched, measured before any repository is read.
