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
[`widget.Widget`](internal/widget/widget.go) and registers itself. The shell handles message
routing, cursor scrolling and config decoding, so a widget is mostly its own data and its own
drawing.

1. **Create the package.**

   ```go
   package weather

   func init() {
       widget.Register(widget.Spec{
           Name:    "weather",
           Summary: "the forecast for one location",   // shown by `ctos widgets`
           Title:   "weather",                         // default frame label
           New:     New,
           Example: `type: weather
   location: Bengaluru
   refresh: 30m
   title: weather`,
       })
   }

   type config struct {
       Location string `yaml:"location"`
       Refresh  string `yaml:"refresh"`
   }

   type Weather struct {
       widget.Base          // SetSize, Focus, Blur, Title, Actions, Cmd, Tick
       cfg     config
       theme   theme.Theme
       refresh time.Duration
   }

   func New(ctx widget.Context) (widget.Widget, error) {
       cfg := config{Refresh: "30m"}            // defaults first
       if err := ctx.Decode(&cfg); err != nil { // strict: a typo is an error
           return nil, err
       }
       if cfg.Location == "" {
           return nil, errors.New(`"location:" is required`)
       }
       refresh, err := ctx.Refresh(cfg.Refresh, 30*time.Minute, time.Minute)
       if err != nil {
           return nil, err
       }
       return &Weather{cfg: cfg, theme: ctx.Theme, refresh: refresh}, nil
   }
   ```

   Return bare errors: the registry labels them with your type and the user's widget name, so
   they come out as `weather "outside": "location:" is required`.

   Don't declare a `title` key. The shell owns it: the registry reads the dashboard's `title:`,
   falls back to your `Spec.Title`, and `Base.Title` returns it. Implement `Title()` yourself
   only if the label changes while the widget runs.

2. **Fetch with `Base.Cmd`,** which delivers the result to your widget and no other:

   ```go
   func (w *Weather) Init() tea.Cmd { return w.fetch() }

   func (w *Weather) fetch() tea.Cmd {
       location := w.cfg.Location
       return w.Cmd(func() tea.Msg {
           forecast, err := lookup(location)
           return loadedMsg{forecast: forecast, err: err}
       })
   }

   func (w *Weather) Update(msg tea.Msg) tea.Cmd {
       if msg, ok := msg.(loadedMsg); ok {
           w.forecast, w.err = msg.forecast, msg.err
           return w.Every(w.refresh, refreshMsg{})   // Tick, addressed to you
       }
       return nil
   }
   ```

   Your message types need no name field and no filter. Two `weather` widgets on one dashboard
   each see only their own results.

3. **If it is a list,** embed a [`widget.List`](internal/widget/list.go) rather than your own
   cursor and offset. It owns the selection, the clamping and the standard keys:

   ```go
   func (w *Weather) Update(msg tea.Msg) tea.Cmd {
       if msg, ok := msg.(tea.KeyMsg); ok && w.list.HandleKey(msg, w.H) {
           return nil                     // ↑/k ↓/j pgup pgdown home/g end/G
       }
       ...
   }

   func (w *Weather) View() string {
       start, end := w.list.Window(w.H)   // scrolled to keep the cursor visible
       ...
   }
   ```

4. **Wire it in** by adding a blank import to `cmd/ctos/main.go`.

5. **Document it** in the README's widget table, and in your `Spec` — the `Summary` and
   `Example` are what `ctos widgets` and `ctos widgets weather` print. A test builds every
   registered widget from its own `Example`, so an example that drifts fails CI.

6. **Test it.** At minimum, cover config validation and whatever parses or does arithmetic.

### Rules widgets must follow

- **Never block.** All I/O goes in a `tea.Cmd` that returns a message. A widget that calls
  `http.Get` in `View()` freezes the whole dashboard.
- **Send with `Base.Cmd`, `Base.Tick` or `Base.Every`.** They address the result to you, which
  is what keeps two widgets of the same type from consuming each other's messages. For a
  message produced inside a callback you do not own, such as `tea.ExecProcess`, wrap it in
  `Base.Address`.
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
