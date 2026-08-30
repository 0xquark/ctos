package widget

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"
)

type stub struct{ Base }

func (s *stub) Init() tea.Cmd          { return nil }
func (s *stub) Update(tea.Msg) tea.Cmd { return nil }
func (s *stub) View() string           { return "" }

func TestRegisterAndNew(t *testing.T) {
	Register(Spec{Name: "test-stub", Summary: "a stub", New: func(Context) (Widget, error) { return &stub{}, nil }})

	w, err := New("test-stub", Context{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if w.Title() != "test-stub" {
		t.Errorf("title = %q, want the type name", w.Title())
	}
}

// TestNewUnknownTypeListsOptions checks the error is actionable: a typo should
// show the user what they could have written.
func TestNewUnknownTypeListsOptions(t *testing.T) {
	Register(Spec{Name: "test-listed", Summary: "a stub", New: func(Context) (Widget, error) { return &stub{}, nil }})

	_, err := New("clokc", Context{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "test-listed") {
		t.Errorf("error should list known types, got: %v", err)
	}
}

func TestFactoryErrorPropagates(t *testing.T) {
	sentinel := errors.New("bad config")
	Register(Spec{Name: "test-broken", Summary: "a stub", New: func(Context) (Widget, error) { return nil, sentinel }})

	if _, err := New("test-broken", Context{}); !errors.Is(err, sentinel) {
		t.Errorf("got %v, want the factory's error", err)
	}
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a duplicate type should panic")
		}
	}()
	Register(Spec{Name: "test-dup", Summary: "a stub", New: func(Context) (Widget, error) { return &stub{}, nil }})
	Register(Spec{Name: "test-dup", Summary: "a stub", New: func(Context) (Widget, error) { return &stub{}, nil }})
}

func TestBaseDefaults(t *testing.T) {
	var b Base
	if b.Focused() {
		t.Error("a new widget should not start focused")
	}
	b.Focus()
	if !b.Focused() {
		t.Error("Focus did not take effect")
	}
	b.Blur()
	if b.Focused() {
		t.Error("Blur did not take effect")
	}

	b.SetSize(40, 12)
	if b.W != 40 || b.H != 12 {
		t.Errorf("size = %dx%d, want 40x12", b.W, b.H)
	}
	if b.Actions() != nil {
		t.Error("Base should default to no actions")
	}
}

// A factory's error is labelled once, by the registry, so a widget need not
// spell out its own type and name to report a config problem.
func TestFactoryErrorNamesTheWidget(t *testing.T) {
	Register(Spec{Name: "test-labelled", Summary: "a stub", New: func(Context) (Widget, error) {
		return nil, errors.New("\"path:\" is required")
	}})

	_, err := New("test-labelled", Context{Name: "inbox"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got, want := err.Error(), `test-labelled "inbox": "path:" is required`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The registry binds a widget's name, so Base.Cmd can address messages without
// the factory having to remember to store it.
func TestNewBindsTheWidgetName(t *testing.T) {
	Register(Spec{Name: "test-bound", Summary: "a stub", New: func(Context) (Widget, error) { return &stub{}, nil }})

	w, err := New("test-bound", Context{Name: "left"})
	if err != nil {
		t.Fatal(err)
	}
	if got := w.(*stub).Name(); got != "left" {
		t.Errorf("bound name = %q, want left", got)
	}
}

// A widget with no summary would show up blank in `ctos widgets`, so the
// registry refuses it rather than shipping an undocumented type.
func TestRegisterRequiresDocumentation(t *testing.T) {
	cases := map[string]Spec{
		"no name":    {Summary: "a stub", New: func(Context) (Widget, error) { return &stub{}, nil }},
		"no summary": {Name: "test-undocumented", New: func(Context) (Widget, error) { return &stub{}, nil }},
		"no factory": {Name: "test-factoryless", Summary: "a stub"},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("registering a spec with %s should panic", name)
				}
			}()
			Register(spec)
		})
	}
}

// The frame title is resolved by the registry, not by each widget: a
// dashboard's "title:" wins, then the type's own default, then its name.
func TestResolveTitle(t *testing.T) {
	spec := Spec{Name: "hackernews", Title: "hacker news"}

	cases := []struct {
		name string
		yaml string
		spec Spec
		want string
	}{
		{"type default", "type: hackernews", spec, "hacker news"},
		{"dashboard overrides", "type: hackernews\ntitle: orange site", spec, "orange site"},
		{"explicit empty is honoured", "type: hackernews\ntitle: \"\"", spec, ""},
		{"type name when the type has no default", "type: clock", Spec{Name: "clock"}, "clock"},
		{"no config block", "", spec, "hacker news"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var node *yaml.Node
			if tc.yaml != "" {
				var doc yaml.Node
				if err := yaml.Unmarshal([]byte(tc.yaml), &doc); err != nil {
					t.Fatal(err)
				}
				node = doc.Content[0]
			}
			if got := resolveTitle(tc.spec, node); got != tc.want {
				t.Errorf("title = %q, want %q", got, tc.want)
			}
		})
	}
}

// Base supplies Title, so a widget only implements it when the title changes
// as the widget runs.
func TestNewBindsTheTitle(t *testing.T) {
	Register(Spec{
		Name:    "test-titled",
		Title:   "a nice label",
		Summary: "a stub",
		New:     func(Context) (Widget, error) { return &stub{}, nil },
	})

	w, err := New("test-titled", Context{Name: "left"})
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Title(); got != "a nice label" {
		t.Errorf("title = %q, want %q", got, "a nice label")
	}
}
