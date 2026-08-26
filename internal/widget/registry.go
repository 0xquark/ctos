package widget

import (
	"fmt"
	"sort"
	"sync"
)

// Factory builds a widget from its dashboard configuration.
type Factory func(Context) (Widget, error)

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

// Register makes a widget type available to dashboards. Widget packages call
// it from init; cmd/ctos blank-imports them to pull them in.
//
// Register panics on a duplicate type name, since that is always a programming
// error rather than bad user input.
func Register(typeName string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[typeName]; dup {
		panic(fmt.Sprintf("widget: type %q registered twice", typeName))
	}
	registry[typeName] = f
}

// New constructs a widget of the named type.
func New(typeName string, ctx Context) (Widget, error) {
	mu.RLock()
	f, ok := registry[typeName]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown widget type %q (known types: %v)", typeName, Types())
	}
	return f(ctx)
}

// Types lists every registered widget type, sorted, for error messages and
// documentation.
func Types() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
