package tui

import (
	"fmt"
	"testing"
)

// TestLayoutFillsSpace is the core invariant: no cell may be lost to integer
// division, or widgets drift out of alignment with the terminal edge.
func TestLayoutFillsSpace(t *testing.T) {
	rows := [][]string{
		{"a", "b"},
		{"c"},
		{"d", "e", "f"},
	}

	for _, size := range []struct{ w, h int }{
		{80, 24}, {100, 30}, {81, 25}, {79, 23}, {120, 41}, {40, 12},
	} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			boxes := Layout(rows, size.w, size.h)

			totalH := 0
			for i, row := range boxes {
				totalH += row[0].H

				totalW := 0
				for _, b := range row {
					totalW += b.W
					if b.H != row[0].H {
						t.Errorf("row %d has ragged heights", i)
					}
				}
				if totalW != size.w {
					t.Errorf("row %d widths sum to %d, want %d", i, totalW, size.w)
				}
			}
			if totalH != size.h {
				t.Errorf("row heights sum to %d, want %d", totalH, size.h)
			}
		})
	}
}

// TestLayoutDistributesRemainder pins where the leftover cells land, so the
// split is stable rather than merely summing correctly.
func TestLayoutDistributesRemainder(t *testing.T) {
	boxes := Layout([][]string{{"a", "b", "c"}}, 80, 10)
	want := []int{27, 27, 26}
	for i, b := range boxes[0] {
		if b.W != want[i] {
			t.Errorf("column %d width = %d, want %d", i, b.W, want[i])
		}
	}
}

func TestLayoutHandlesDegenerateInput(t *testing.T) {
	if got := Layout(nil, 80, 24); len(got) != 0 {
		t.Errorf("no rows should produce no boxes, got %d", len(got))
	}
	boxes := Layout([][]string{{"a"}}, 0, 0)
	if len(boxes) != 1 || boxes[0][0].W != 0 {
		t.Errorf("zero-size terminal should produce zero-size boxes, got %+v", boxes)
	}
}

func TestBoxInnerNeverNegative(t *testing.T) {
	for _, b := range []Box{{W: 0, H: 0}, {W: 2, H: 1}, {W: 3, H: 1}} {
		w, h := b.Inner()
		if w < 0 || h < 0 {
			t.Errorf("Box%+v.Inner() = %d,%d; must never be negative", b, w, h)
		}
	}
	w, h := Box{W: 24, H: 10}.Inner()
	if w != 20 || h != 8 {
		t.Errorf("Inner() = %d,%d; want 20,8", w, h)
	}
}
