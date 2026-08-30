package spark

import (
	"strings"
	"testing"
)

func TestRingKeepsTheNewestValues(t *testing.T) {
	s := NewSeries(3)
	for _, v := range []float64{1, 2, 3, 4, 5} {
		s.Push(v)
	}

	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3", s.Len())
	}
	got := s.Values()
	for i, want := range []float64{3, 4, 5} {
		if got[i] != want {
			t.Fatalf("Values = %v, want [3 4 5]", got)
		}
	}
}

func TestRenderScalesAgainstTheGivenMaximum(t *testing.T) {
	s := NewSeries(8)
	for _, v := range []float64{0, 12.5, 25, 37.5, 50, 62.5, 75, 100} {
		s.Push(v)
	}
	if got, want := s.Render(8, 100), "▁▂▃▄▅▆▇█"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

// A rate has no natural ceiling, so the busiest value in the window becomes
// the full block.
func TestRenderAutoScales(t *testing.T) {
	s := NewSeries(4)
	for _, v := range []float64{0, 500, 1000, 2000} {
		s.Push(v)
	}
	if got, want := s.Render(4, 0), "▁▃▅█"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

// The line has to be exactly as wide as it was asked for, or it shifts every
// column drawn to its right.
func TestRenderIsAlwaysTheRequestedWidth(t *testing.T) {
	s := NewSeries(16)
	for i := range 5 {
		s.Push(float64(i))
		for _, w := range []int{1, 8, 20} {
			if got := len([]rune(s.Render(w, 100))); got != w {
				t.Errorf("Render(%d) after %d values is %d cells", w, i+1, got)
			}
		}
	}
}

// A fresh series pads left, so the line grows rightward as history arrives
// rather than stretching to fill the width from one value.
func TestRenderPadsAnEmptyHistoryOnTheLeft(t *testing.T) {
	s := NewSeries(8)
	if got := s.Render(8, 100); got != strings.Repeat(" ", 8) {
		t.Errorf("empty Render = %q, want blanks", got)
	}

	s.Push(50)
	if got, want := s.Render(8, 100), "       ▅"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

// A window narrower than the history shows the newest end of it.
func TestRenderTruncatesToTheNewestValues(t *testing.T) {
	s := NewSeries(8)
	for _, v := range []float64{100, 100, 100, 0} {
		s.Push(v)
	}
	if got, want := s.Render(2, 100), "█▁"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestRenderHandlesDegenerateInput(t *testing.T) {
	s := NewSeries(4)
	s.Push(-5)
	s.Push(500)
	if got, want := s.Render(2, 100), "▁█"; got != want {
		t.Errorf("out-of-range values should clamp, got %q want %q", got, want)
	}
	if got := s.Render(0, 100); got != "" {
		t.Errorf("Render(0) = %q, want empty", got)
	}
	if got := NewSeries(0).Render(2, 100); got != "  " {
		t.Errorf("NewSeries(0).Render = %q, want blanks", got)
	}
}

func TestDelta(t *testing.T) {
	s := NewSeries(8)
	for _, v := range []float64{10, 12, 15, 14, 20} {
		s.Push(v)
	}

	if got, ok := s.Delta(4); !ok || got != 10 {
		t.Errorf("Delta(4) = %v %v, want 10 true", got, ok)
	}
	if got, ok := s.Delta(1); !ok || got != 6 {
		t.Errorf("Delta(1) = %v %v, want 6 true", got, ok)
	}

	// A window the history does not reach back to has no answer, rather
	// than an answer measured against a shorter span than asked for.
	if _, ok := s.Delta(5); ok {
		t.Error("Delta past the start of the history should report false")
	}
	if _, ok := s.Delta(0); ok {
		t.Error("Delta(0) should report false")
	}
}
