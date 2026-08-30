// Package config loads ctOS settings and dashboard definitions from YAML.
//
// Two files matter:
//
//	<dir>/config.yaml            global settings
//	<dir>/dashboards/<name>.yaml one dashboard each
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration so YAML can carry "5s" or "10m".
type Duration time.Duration

// UnmarshalYAML parses a Go duration string.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return fmt.Errorf("line %d: duration must be a string like \"30s\"", n.Line)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q: use a form like \"30s\", \"5m\", \"1h\"", n.Line, s)
	}
	if v <= 0 {
		return fmt.Errorf("line %d: duration %q must be positive", n.Line, s)
	}
	*d = Duration(v)
	return nil
}

// Duration converts back to the standard library type.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Config is the global settings file.
type Config struct {
	// Editor opens files. Falls back to $EDITOR, then "vi".
	Editor string `yaml:"editor"`

	// DefaultDashboard names the dashboard shown at startup.
	DefaultDashboard string `yaml:"default_dashboard"`

	Theme struct {
		Accent string `yaml:"accent"`
	} `yaml:"theme"`

	Refresh struct {
		Default Duration `yaml:"default"`
	} `yaml:"refresh"`

	// Dir is the directory this config was loaded from. Not a YAML field.
	Dir string `yaml:"-"`
}

// ResolveEditor returns the editor command, honouring config then $EDITOR then
// a "vi" fallback that exists on every supported platform.
func (c *Config) ResolveEditor() string {
	if c.Editor != "" {
		return c.Editor
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	return "vi"
}

// DefaultRefresh is the polling interval for widgets that set none.
func (c *Config) DefaultRefresh() time.Duration {
	if d := c.Refresh.Default.Duration(); d > 0 {
		return d
	}
	return 30 * time.Second
}

// WidgetSpec is one entry in a dashboard's widgets map.
type WidgetSpec struct {
	Type string `yaml:"type"`

	// Node is the whole mapping, so a widget factory can decode its own keys.
	Node *yaml.Node `yaml:"-"`
}

// Dashboard is one dashboard file.
type Dashboard struct {
	Name    string                `yaml:"name"`
	Widgets map[string]WidgetSpec `yaml:"widgets"`

	// Bar names widgets drawn as a frameless strip pinned to one edge of
	// the screen. A bar widget never takes focus and is not part of the
	// grid, so it is held separately rather than as a special row.
	Bar Bar `yaml:"bar"`

	// Rows lays out widget names: each inner slice is one row, and widgets
	// in a row split the available width.
	Rows [][]string `yaml:"rows"`

	// Path is the file this came from. Not a YAML field.
	Path string `yaml:"-"`
}

// Load reads the global config from dir. A missing file is not an error: ctOS
// runs on defaults so a first launch works before `ctos init`.
func Load(dir string) (*Config, error) {
	cfg := &Config{Dir: dir}
	path := SettingsFile(dir)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return cfg, nil // empty file
	}
	if err := expandTree(&doc, path); err != nil {
		return nil, err
	}
	if err := doc.Content[0].Decode(cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Dir = dir
	return cfg, nil
}

// LoadDashboard reads and validates a single dashboard file.
func LoadDashboard(path string) (*Dashboard, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dashboard %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("%s: file is empty", path)
	}
	if err := expandTree(&doc, path); err != nil {
		return nil, err
	}

	root := doc.Content[0]

	// Decode the skeleton, then attach each widget's raw node.
	// Widgets decodes into yaml.Node by value: yaml.v3 only special-cases
	// the value type, and silently yields a zero node for *yaml.Node.
	var raw struct {
		Name    string               `yaml:"name"`
		Widgets map[string]yaml.Node `yaml:"widgets"`
		Bar     Bar                  `yaml:"bar"`
		Rows    [][]string           `yaml:"rows"`
	}
	if err := root.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	d := &Dashboard{
		Name:    raw.Name,
		Bar:     raw.Bar,
		Rows:    raw.Rows,
		Path:    path,
		Widgets: make(map[string]WidgetSpec, len(raw.Widgets)),
	}
	if d.Name == "" {
		d.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	for name, node := range raw.Widgets {
		if node.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s:%d: widget %q must be a mapping with a \"type:\" key", path, node.Line, name)
		}
		var spec WidgetSpec
		if err := node.Decode(&spec); err != nil {
			return nil, fmt.Errorf("%s: widget %q: %w", path, name, err)
		}
		if spec.Type == "" {
			return nil, fmt.Errorf("%s:%d: widget %q has no \"type:\"", path, node.Line, name)
		}
		spec.Node = &node
		d.Widgets[name] = spec
	}

	if err := d.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// BarPosition is the edge of the screen a bar is pinned to.
type BarPosition string

// The four edges. Top is the default because a status strip is what the bar
// was built for, and a terminal puts its status line at the top or the bottom.
const (
	BarTop    BarPosition = "top"
	BarBottom BarPosition = "bottom"
	BarLeft   BarPosition = "left"
	BarRight  BarPosition = "right"
)

// Vertical reports whether the bar runs down a side rather than across.
func (p BarPosition) Vertical() bool { return p == BarLeft || p == BarRight }

// barPositions is every accepted value, in the order the error message lists
// them.
var barPositions = []BarPosition{BarTop, BarBottom, BarLeft, BarRight}

// DefaultBarWidth is how many columns a left- or right-hand bar takes when the
// dashboard does not say. It is wide enough for the system widget's "rows"
// panel — a label, a bar and a value — and narrow enough to be chrome.
const DefaultBarWidth = 24

// Bar is the frameless strip pinned to one edge of the dashboard.
//
// It takes either a plain list of widget names, which all sit at the strip's
// leading end, or a mapping with two groups and an optional "position:".
//
// The group keys follow the orientation, because "left:" means nothing on a
// bar that runs vertically. A top or bottom bar takes "left:" and "right:"; a
// left or right bar takes "top:" and "bottom:". Both spellings land in Start
// and End: Start is the leading end, End the trailing one.
//
// Two groups exist because a clock or a hostname belongs at the far end of a
// status line, where the eye expects it and where it will not move as the
// values before it change size.
type Bar struct {
	// Position is the edge the bar is pinned to. Empty means top.
	Position BarPosition

	// Width is how many columns a vertical bar takes. Zero means
	// DefaultBarWidth. It is meaningless on a horizontal bar, whose width
	// is the terminal's and whose height comes from its contents.
	Width int

	// Start is the leading group: the left of a horizontal bar, the top of
	// a vertical one. End is the trailing group.
	Start, End []string
}

// Horizontal reports whether the bar runs across the screen.
func (b Bar) Horizontal() bool { return !b.Position.Vertical() }

// groupKeys is the pair of group names this orientation accepts, leading first.
func (p BarPosition) groupKeys() (start, end string) {
	if p.Vertical() {
		return "top", "bottom"
	}
	return "left", "right"
}

// UnmarshalYAML accepts the list form and the mapping form.
//
// "position:" is read before the groups are validated, so the key it governs
// may be written either side of it — a file is not obliged to put position
// first just because the parser walks the mapping in order.
func (b *Bar) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		b.Position = BarTop
		return node.Decode(&b.Start)

	case yaml.MappingNode:
		if err := b.decodePosition(node); err != nil {
			return err
		}
		return b.decodeGroups(node)

	default:
		return fmt.Errorf("line %d: \"bar:\" takes a list of widget names, or a mapping with \"position:\" and two groups", node.Line)
	}
}

// decodePosition reads "position:" out of the mapping, leaving the default in
// place when it is absent.
func (b *Bar) decodePosition(node *yaml.Node) error {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value != "position" {
			continue
		}
		val := node.Content[i+1]
		var s string
		if err := val.Decode(&s); err != nil {
			return fmt.Errorf("line %d: \"position:\" must be one of %s", val.Line, joinPositions())
		}
		if !slices.Contains(barPositions, BarPosition(s)) {
			return fmt.Errorf("line %d: unknown bar position %q (valid: %s)", val.Line, s, joinPositions())
		}
		b.Position = BarPosition(s)
	}
	if b.Position == "" {
		b.Position = BarTop
	}
	return nil
}

// decodeGroups reads the two group lists and the width, rejecting any key that
// does not belong on a bar of this orientation.
func (b *Bar) decodeGroups(node *yaml.Node) error {
	startKey, endKey := b.Position.groupKeys()

	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i].Value, node.Content[i+1]

		switch key {
		case "position":
			continue

		case "width":
			if b.Horizontal() {
				return fmt.Errorf("line %d: \"width:\" applies only to a \"left\" or \"right\" bar; a %s bar is as wide as the terminal",
					val.Line, b.Position)
			}
			if err := val.Decode(&b.Width); err != nil || b.Width < 1 {
				return fmt.Errorf("line %d: \"width:\" must be a positive number of columns", val.Line)
			}

		case startKey:
			if err := val.Decode(&b.Start); err != nil {
				return err
			}

		case endKey:
			if err := val.Decode(&b.End); err != nil {
				return err
			}

		default:
			// Naming the orientation is the useful half of this
			// error: the key is almost always the other pair,
			// written from habit rather than in ignorance.
			return fmt.Errorf("line %d: unknown key %q under \"bar:\"; a %s bar takes %q and %q%s",
				node.Content[i].Line, key, b.Position, startKey, endKey, widthHint(b.Position))
		}
	}
	return nil
}

// widthHint mentions "width:" only where it applies.
func widthHint(p BarPosition) string {
	if p.Vertical() {
		return ", plus \"width:\" and \"position:\""
	}
	return ", plus \"position:\""
}

func joinPositions() string {
	out := make([]string, len(barPositions))
	for i, p := range barPositions {
		out[i] = string(p)
	}
	return strings.Join(out, ", ")
}

// Names is every widget in the bar, leading group first.
func (b Bar) Names() []string {
	return append(slices.Clone(b.Start), b.End...)
}

// Empty reports whether the dashboard has no status bar.
func (b Bar) Empty() bool { return len(b.Start)+len(b.End) == 0 }

// Columns is the width a vertical bar takes. It is zero for a horizontal bar,
// which does not take width away from the grid at all.
func (b Bar) Columns() int {
	if b.Empty() || b.Horizontal() {
		return 0
	}
	if b.Width > 0 {
		return b.Width
	}
	return DefaultBarWidth
}

// validate checks that rows and widgets agree with each other.
func (d *Dashboard) validate() error {
	if len(d.Widgets) == 0 {
		return fmt.Errorf("no widgets defined")
	}

	seen := map[string]bool{}
	for i, name := range d.Bar.Names() {
		if _, ok := d.Widgets[name]; !ok {
			return fmt.Errorf("bar[%d] references widget %q, which is not defined under \"widgets:\" (defined: %s)",
				i, name, strings.Join(d.widgetNames(), ", "))
		}
		if seen[name] {
			return fmt.Errorf("widget %q appears in \"bar:\" more than once", name)
		}
		seen[name] = true
	}

	// A dashboard with no rows stacks every widget vertically, in name
	// order, so a minimal file still renders. Anything already in the bar
	// stays there rather than being stacked as well.
	if len(d.Rows) == 0 {
		for _, name := range d.widgetNames() {
			if !seen[name] {
				d.Rows = append(d.Rows, []string{name})
			}
		}
		if len(d.Rows) == 0 {
			return fmt.Errorf("every widget is in \"bar:\"; a dashboard needs at least one widget in \"rows:\"")
		}
		return nil
	}

	for i, row := range d.Rows {
		if len(row) == 0 {
			return fmt.Errorf("rows[%d] is empty", i)
		}
		for _, name := range row {
			if _, ok := d.Widgets[name]; !ok {
				return fmt.Errorf("rows[%d] references widget %q, which is not defined under \"widgets:\" (defined: %s)",
					i, name, strings.Join(d.widgetNames(), ", "))
			}
			if seen[name] {
				return fmt.Errorf("widget %q appears in \"rows:\" or \"bar:\" more than once", name)
			}
			seen[name] = true
		}
	}
	for _, name := range d.widgetNames() {
		if !seen[name] {
			return fmt.Errorf("widget %q is defined but never placed in \"rows:\" or \"bar:\"", name)
		}
	}
	return nil
}

func (d *Dashboard) widgetNames() []string {
	names := make([]string, 0, len(d.Widgets))
	for name := range d.Widgets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ListDashboards returns every dashboard file in a config directory, sorted by
// name. A missing dashboards directory yields no results and no error.
func ListDashboards(dir string) ([]string, error) {
	glob := filepath.Join(DashboardsDir(dir), "*.y*ml")
	paths, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

// PickDashboard chooses which dashboard to open. An explicit name wins, then
// config's default_dashboard, then the alphabetically first file.
func PickDashboard(dir, want, fallback string) (string, error) {
	paths, err := ListDashboards(dir)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no dashboards found in %s\n\nRun `ctos init` to create a starter config", DashboardsDir(dir))
	}

	byName := map[string]string{}
	for _, p := range paths {
		byName[strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))] = p
	}

	for _, name := range []string{want, fallback} {
		if name == "" {
			continue
		}
		if p, ok := byName[name]; ok {
			return p, nil
		}
		if name == want {
			available := make([]string, 0, len(byName))
			for n := range byName {
				available = append(available, n)
			}
			sort.Strings(available)
			return "", fmt.Errorf("no dashboard named %q (available: %s)", want, strings.Join(available, ", "))
		}
	}
	return paths[0], nil
}
