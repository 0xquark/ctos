# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/). Before 1.0, breaking changes bump the minor version.

## [Unreleased]

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

### Changed

- The clock now draws rounded line digits matching the widget frames, in three rows rather than
  five, leaving more room for the date.
