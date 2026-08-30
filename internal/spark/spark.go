// Package spark renders a series of numbers as a one-line sparkline.
//
// It is deliberately shared rather than living inside the widget that needed
// it first: a gauge says what a number is now, and a sparkline says whether it
// is on its way up. Every widget that polls a number wants the second — the
// system vitals today, the SSH host summary and the Grafana panels later — so
// the ring buffer and the glyph table live in one place.
package spark

import "strings"

// levels are the eight block heights, lightest to fullest. A value of zero
// renders as the lowest block rather than a blank, so that a flat line at
// zero is still visibly a line.
var levels = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Series is a fixed-capacity ring of the most recent values.
//
// The zero Series is unusable; call NewSeries. Push and Render are called from
// the UI goroutine only, so there is no locking.
type Series struct {
	vals []float64
	next int
	full bool
}

// NewSeries returns a series holding at most n values.
func NewSeries(n int) *Series {
	return &Series{vals: make([]float64, max(n, 1))}
}

// Push records the newest value, dropping the oldest once the ring is full.
func (s *Series) Push(v float64) {
	s.vals[s.next] = v
	s.next = (s.next + 1) % len(s.vals)
	if s.next == 0 {
		s.full = true
	}
}

// Len is how many values have been recorded, up to the capacity.
func (s *Series) Len() int {
	if s.full {
		return len(s.vals)
	}
	return s.next
}

// Values returns the recorded values, oldest first.
func (s *Series) Values() []float64 {
	out := make([]float64, 0, s.Len())
	if s.full {
		out = append(out, s.vals[s.next:]...)
	}
	return append(out, s.vals[:s.next]...)
}

// Delta is the change from the value n samples back to the newest one, and
// reports false when the history does not reach that far.
//
// It is what turns a readout into a ticker: a number on its own says where the
// machine is, and the change beside it says which way it is going. The window
// is counted in samples rather than seconds because the caller knows its own
// refresh interval and the series does not.
func (s *Series) Delta(n int) (float64, bool) {
	vals := s.Values()
	if n < 1 || len(vals) <= n {
		return 0, false
	}
	return vals[len(vals)-1] - vals[len(vals)-1-n], true
}

// Render draws the last width values as blocks, newest at the right.
//
// scale is the value that fills a cell; pass 100 for a percentage. A scale of
// zero or less auto-scales to the largest value in the window, which is what a
// rate wants — the busiest second in view becomes the full block.
//
// A series with fewer values than width is padded on the left with spaces, so
// the line grows rightward as history accumulates instead of stretching.
func (s *Series) Render(width int, scale float64) string {
	if width <= 0 {
		return ""
	}

	vals := s.Values()
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}
	if len(vals) == 0 {
		return strings.Repeat(" ", width)
	}

	if scale <= 0 {
		for _, v := range vals {
			scale = max(scale, v)
		}
	}

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", width-len(vals)))
	for _, v := range vals {
		b.WriteRune(level(v, scale))
	}
	return b.String()
}

// level maps a value onto one block. Everything at or below zero draws the
// lowest block, and anything at or above the scale draws the fullest.
func level(v, scale float64) rune {
	if scale <= 0 || v <= 0 {
		return levels[0]
	}
	i := int(v / scale * float64(len(levels)))
	return levels[min(max(i, 0), len(levels)-1)]
}
