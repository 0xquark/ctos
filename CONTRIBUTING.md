# Contributing to ctOS

Thanks for looking. ctOS is early, so the most useful contributions are widgets, platform
fixes, and telling us where the config format is awkward.

## Build and test

```sh
git clone https://github.com/0xquark/ctos
cd ctos
make build      # -> ./ctos
make test       # go test ./...
make check      # fmt + vet + test, what CI runs
make lint       # golangci-lint, if installed
```

You need Go 1.26 or newer. There is nothing else to install.

Run against a throwaway config instead of your real one:

```sh
./ctos --config-dir /tmp/ctos-dev init
./ctos --config-dir /tmp/ctos-dev
```

## Adding a widget

A widget is one package under `internal/widgets/`. It implements
[`widget.Widget`](internal/widget/widget.go) and registers itself.

1. **Create the package.**

   ```go
   package weather

   func init() { widget.Register("weather", New) }

   type config struct {
       Location string `yaml:"location"`
       Title    string `yaml:"title"`
   }

   type Weather struct {
       widget.Base          // supplies SetSize, Focus, Blur, Actions
       cfg   config
       theme theme.Theme
   }

   func New(ctx widget.Context) (widget.Widget, error) {
       cfg := config{Title: "weather"}          // defaults first
       if ctx.Node != nil {
           if err := ctx.Node.Decode(&cfg); err != nil {
               return nil, fmt.Errorf("weather %q: %w", ctx.Name, err)
           }
       }
       return &Weather{cfg: cfg, theme: ctx.Theme}, nil
   }
   ```

2. **Register it** by adding a blank import to `cmd/ctos/main.go`.

3. **Document it** in the README's widget table — every key, its default, and its meaning.

4. **Test it.** At minimum, cover config validation and whatever parses or does arithmetic.

### Rules widgets must follow

- **Never block.** All I/O goes in a `tea.Cmd` that returns a message. A widget that calls
  `http.Get` in `View()` freezes the whole dashboard.
- **Tag your messages.** Every widget gets `ctx.Name`. Async result messages carry it, and
  `Update` ignores messages belonging to another widget; non-key messages are broadcast to
  everyone.
- **Respect your size.** `SetSize` gives you the inner content area. Render at most that many
  columns and rows; the frame truncates anything longer, which usually means a bug.
- **Fail visibly, not fatally.** Render the error inside your own view. Never panic, never
  `os.Exit`.
- **Don't import `internal/tui`.** Widgets depend on `internal/widget` and `internal/theme`
  only.
- **Use the theme.** Take colours from `ctx.Theme` so dashboards look like one product and
  respect the user's accent.

## Pull requests

- One logical change per PR. Say what problem it solves.
- `make check` passes, on both Linux and macOS if you touch anything OS-specific.
- New behaviour comes with a test.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/):
  `feat(widgets): add weather`, `fix(config): expand ~ in dashboard paths`.

## Reporting bugs

Include your OS, terminal, `ctos --version`, and the dashboard YAML that triggers it. For a
rendering bug, a screenshot or a copy-pasted frame helps more than a description.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
