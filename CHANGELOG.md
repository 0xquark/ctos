# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/). Before 1.0, breaking changes bump the minor version.

## [Unreleased]

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

### Added

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
