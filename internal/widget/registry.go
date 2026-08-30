package widget

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/0xquark/ctos/internal/theme"
	"gopkg.in/yaml.v3"
)

// Factory builds a widget from its dashboard configuration.
type Factory func(Context) (Widget, error)

// Spec is everything the program knows about a widget type: how to build one,
// and how to explain it to someone writing YAML. The documentation is part of
// the registration rather than a README that drifts, so `ctos widgets` can
// always answer what a type is and how to configure it.
type Spec struct {
	// Name is the string a dashboard's "type:" must match.
	Name string

	// Summary is one line for the widget list. Lowercase, no full stop:
	// "the current time, optionally as large block digits".
	Summary string

	// Example is a minimal YAML block showing the widget's keys, indented
	// as it would appear under a dashboard's "widgets:". Optional, but a
	// widget with any configuration at all should have one.
	Example string

	// Title is the default frame label, for a type whose name does not read
	// well as one: "hacker news" for hackernews. Falls back to Name, and a
	// dashboard's "title:" overrides both.
	Title string

	// New builds the widget.
	New Factory
}

var (
	mu       sync.RWMutex
	registry = map[string]Spec{}
)

// Register makes a widget type available to dashboards. Widget packages call
// it from init; cmd/ctos blank-imports them to pull them in.
//
// It panics on a duplicate name, or a spec missing its name, summary or
// factory: all are programming errors, caught the first time the binary runs
// rather than the first time a user asks for that widget.
func Register(spec Spec) {
	switch {
	case spec.Name == "":
		panic("widget: Register with no Name")
	case spec.New == nil:
		panic("widget: " + spec.Name + " registered with no New")
	case spec.Summary == "":
		panic("widget: " + spec.Name + " registered with no Summary; `ctos widgets` has nothing to say about it")
	}

	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[spec.Name]; dup {
		panic(fmt.Sprintf("widget: type %q registered twice", spec.Name))
	}
	registry[spec.Name] = spec
}

// binder is satisfied by any widget embedding Base. New uses it to hand the
// widget its own name, frame title and palette, so addressed messages, the
// frame label and theming work without the factory having to remember
// anything.
type binder interface {
	bind(name, title string, th theme.Theme)
}

// New constructs a widget of the named type.
func New(typeName string, ctx Context) (Widget, error) {
	spec, ok := Lookup(typeName)
	if !ok {
		return nil, fmt.Errorf("unknown widget type %q (known types: %s)", typeName, strings.Join(Types(), ", "))
	}

	ctx.Type = typeName
	w, err := spec.New(ctx)
	if err != nil {
		// Labelled here so every widget's config errors read the same way,
		// including ones whose factory returns a bare error.
		return nil, fmt.Errorf("%s %q: %w", typeName, ctx.Name, err)
	}
	if b, ok := w.(binder); ok {
		b.bind(ctx.Name, resolveTitle(spec, ctx.Node), ctx.Theme)
	}
	return w, nil
}

// resolveTitle picks the widget's frame label: the dashboard's "title:" when
// it is written, then the type's own default, then the type name. An explicit
// empty title is honoured, so a widget can be given a bare frame.
func resolveTitle(spec Spec, node *yaml.Node) string {
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "title" {
				return node.Content[i+1].Value
			}
		}
	}
	if spec.Title != "" {
		return spec.Title
	}
	return spec.Name
}

// Lookup returns the spec for a widget type.
func Lookup(typeName string) (Spec, bool) {
	mu.RLock()
	defer mu.RUnlock()
	spec, ok := registry[typeName]
	return spec, ok
}

// Specs lists every registered widget, sorted by name, for `ctos widgets` and
// for generated documentation.
func Specs() []Spec {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Spec, 0, len(registry))
	for _, spec := range registry {
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Types lists every registered widget type, sorted, for error messages.
func Types() []string {
	specs := Specs()
	out := make([]string, len(specs))
	for i, spec := range specs {
		out[i] = spec.Name
	}
	return out
}
