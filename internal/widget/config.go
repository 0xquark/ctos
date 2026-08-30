package widget

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// shellKeys are the keys the dashboard loader owns. They appear in every
// widget's YAML block but belong to no widget's config struct, so Decode
// passes over them rather than reporting them as unknown.
var shellKeys = map[string]bool{"type": true}

// Decode unmarshals this widget's YAML block into cfg, a pointer to a struct
// whose fields carry `yaml:` tags. Set cfg's defaults before calling: a key
// the user did not write is left alone.
//
// Unknown keys are an error. A dashboard is hand-written, and silently
// ignoring "limt: 30" leaves the user staring at a widget that shrugs off
// their config with nothing to go on.
func (c Context) Decode(cfg any) error {
	if c.Node == nil {
		return nil
	}
	if err := checkKeys(c.Node, cfg); err != nil {
		return err
	}
	if err := c.Node.Decode(cfg); err != nil {
		return flatten(err)
	}
	return nil
}

// flatten turns yaml.v3's multi-line TypeError into one line, so a config
// problem prints as a single sentence next to the file it came from.
func flatten(err error) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	return errors.New(strings.Join(typeErr.Errors, "; "))
}

// checkKeys reports the first key in node that cfg has no field for. It runs
// before decoding, so the error carries the key's real line in the dashboard
// file rather than a position in some rewritten copy of it.
func checkKeys(node *yaml.Node, cfg any) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	known := yamlKeys(cfg)
	index := make(map[string]bool, len(known))
	for _, k := range known {
		index[k] = true
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if shellKeys[key] || index[key] {
			continue
		}
		msg := fmt.Sprintf("line %d: unknown key %q", node.Content[i].Line, key)
		if len(known) > 0 {
			msg += fmt.Sprintf(" (valid keys: %s)", strings.Join(known, ", "))
		}
		return errors.New(msg)
	}
	return nil
}

// Refresh resolves a polling interval, in order of preference: the widget's
// own "refresh:", then def, then the dashboard-wide default. The result is
// clamped so no config can poll faster than floor.
//
// Pass a def when the widget has a natural rate of its own — a process table
// wants seconds where a news feed wants minutes — and 0 to take the
// dashboard's default.
func (c Context) Refresh(spec string, def, floor time.Duration) (time.Duration, error) {
	d := def
	if d <= 0 {
		d = c.DefaultRefresh
	}
	if spec != "" {
		parsed, err := c.Duration("refresh", spec)
		if err != nil {
			return 0, err
		}
		d = parsed
	}
	return max(d, floor), nil
}

// Duration parses a duration written under key, naming the key in the error so
// the user knows which line to fix.
func (c Context) Duration(key, spec string) (time.Duration, error) {
	d, err := time.ParseDuration(spec)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: use a form like \"30s\", \"5m\" or \"1h\"", key, spec)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive, got %q", key, spec)
	}
	return d, nil
}

// yamlKeys lists the YAML names of cfg's fields, so an unknown-key error can
// show the user what they could have written.
func yamlKeys(cfg any) []string {
	t := reflect.TypeOf(cfg)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}

	keys := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if strings.Contains(opts, "inline") {
			keys = append(keys, yamlKeys(reflect.New(f.Type).Interface())...)
			continue
		}
		switch name {
		case "-":
			continue
		case "":
			name = strings.ToLower(f.Name)
		}
		keys = append(keys, name)
	}
	return keys
}
