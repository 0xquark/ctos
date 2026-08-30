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

	// Bar names widgets drawn as a frameless strip pinned above the rows.
	// A bar widget never takes focus and is not part of the grid, so it is
	// held separately rather than as a special row.
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

// Bar is the frameless strip above the rows.
//
// It takes either a plain list of widget names, which all sit on the left, or
// a mapping with "left:" and "right:" groups. The list form is the common case
// and stays the short one; the mapping exists because a clock or a hostname
// belongs at the far end of a status line, where the eye expects it and where
// it will not move as the values to its left change width.
type Bar struct {
	Left  []string `yaml:"left"`
	Right []string `yaml:"right"`
}

// UnmarshalYAML accepts both spellings.
func (b *Bar) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		return node.Decode(&b.Left)

	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			switch key := node.Content[i].Value; key {
			case "left", "right":
			default:
				return fmt.Errorf("line %d: unknown key %q under \"bar:\" (valid keys: left, right)",
					node.Content[i].Line, key)
			}
		}
		// The alias avoids recursing back into this method.
		type barFields Bar
		var f barFields
		if err := node.Decode(&f); err != nil {
			return err
		}
		*b = Bar(f)
		return nil

	default:
		return fmt.Errorf("line %d: \"bar:\" takes a list of widget names, or \"left:\" and \"right:\" lists", node.Line)
	}
}

// Names is every widget in the bar, left group first.
func (b Bar) Names() []string {
	return append(slices.Clone(b.Left), b.Right...)
}

// Empty reports whether the dashboard has no status bar.
func (b Bar) Empty() bool { return len(b.Left)+len(b.Right) == 0 }

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
