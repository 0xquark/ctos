package widget

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type stub struct{ Base }

func (s *stub) Init() tea.Cmd                    { return nil }
func (s *stub) Update(tea.Msg) (Widget, tea.Cmd) { return s, nil }
func (s *stub) View() string                     { return "" }
func (s *stub) Title() string                    { return "stub" }

func TestRegisterAndNew(t *testing.T) {
	Register("test-stub", func(Context) (Widget, error) { return &stub{}, nil })

	w, err := New("test-stub", Context{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if w.Title() != "stub" {
		t.Errorf("got %q, want stub", w.Title())
	}
}

// TestNewUnknownTypeListsOptions checks the error is actionable: a typo should
// show the user what they could have written.
func TestNewUnknownTypeListsOptions(t *testing.T) {
	Register("test-listed", func(Context) (Widget, error) { return &stub{}, nil })

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
	Register("test-broken", func(Context) (Widget, error) { return nil, sentinel })

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
	Register("test-dup", func(Context) (Widget, error) { return &stub{}, nil })
	Register("test-dup", func(Context) (Widget, error) { return &stub{}, nil })
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
