package tui

import (
	"reflect"
	"testing"
)

func layout() [][]string {
	return [][]string{
		{"clock", "notes"},
		{"hackernews"},
	}
}

func TestMoveWithinRow(t *testing.T) {
	tests := []struct {
		name string
		move func([][]string, string) [][]string
		who  string
		want [][]string
	}{
		{"left swaps", MoveLeft, "notes", [][]string{{"notes", "clock"}, {"hackernews"}}},
		{"left at edge is a no-op", MoveLeft, "clock", layout()},
		{"right swaps", MoveRight, "clock", [][]string{{"notes", "clock"}, {"hackernews"}}},
		{"right at edge is a no-op", MoveRight, "notes", layout()},
		{"unknown widget is a no-op", MoveRight, "ghost", layout()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.move(layout(), tc.who)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMoveUp checks a widget keeps its column when it changes row: hackernews
// sits at column 0, so it lands at column 0 of the row above.
func TestMoveUp(t *testing.T) {
	got := MoveUp(layout(), "hackernews")
	want := [][]string{{"hackernews", "clock", "notes"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestMoveUpFromTopRowPromotes checks a widget can always be raised: from the
// top row it creates a new row above rather than silently doing nothing.
func TestMoveUpFromTopRowPromotes(t *testing.T) {
	got := MoveUp(layout(), "notes")
	want := [][]string{{"notes"}, {"clock"}, {"hackernews"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMoveUpAloneAtTopIsANoOp(t *testing.T) {
	start := [][]string{{"clock"}, {"notes"}}
	if got := MoveUp(start, "clock"); !reflect.DeepEqual(got, start) {
		t.Errorf("got %v, want it unchanged", got)
	}
}

func TestMoveDown(t *testing.T) {
	got := MoveDown(layout(), "clock")
	want := [][]string{{"notes"}, {"clock", "hackernews"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestColumnIsPreservedAcrossRows pins the rule that makes vertical moves
// predictable: a widget lands in the same column it left, clamped to the
// target row's width.
func TestColumnIsPreservedAcrossRows(t *testing.T) {
	start := [][]string{{"a", "b", "c"}, {"d", "e", "f"}}

	if got := MoveDown(start, "c"); !reflect.DeepEqual(got, [][]string{{"a", "b"}, {"d", "e", "c", "f"}}) {
		t.Errorf("column 2 down: got %v", got)
	}
	// Clamped: column 2 into a one-widget row lands at the end.
	narrow := [][]string{{"a", "b", "c"}, {"d"}}
	if got := MoveDown(narrow, "c"); !reflect.DeepEqual(got, [][]string{{"a", "b"}, {"d", "c"}}) {
		t.Errorf("clamped column: got %v", got)
	}
}

func TestMoveDownFromBottomCreatesARow(t *testing.T) {
	start := [][]string{{"clock"}, {"notes", "hackernews"}}
	got := MoveDown(start, "notes")
	want := [][]string{{"clock"}, {"hackernews"}, {"notes"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMoveDownAloneAtBottomIsANoOp(t *testing.T) {
	start := [][]string{{"clock"}, {"notes"}}
	if got := MoveDown(start, "notes"); !reflect.DeepEqual(got, start) {
		t.Errorf("got %v, want it unchanged", got)
	}
}

// TestEmptyRowsAreDropped is the invariant that keeps the layout renderable:
// vacating a row must remove it, not leave a zero-height gap.
func TestEmptyRowsAreDropped(t *testing.T) {
	start := [][]string{{"clock"}, {"notes"}, {"hackernews"}}
	got := MoveDown(start, "clock")
	want := [][]string{{"clock", "notes"}, {"hackernews"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSplitOut(t *testing.T) {
	got := SplitOut(layout(), "clock")
	want := [][]string{{"notes"}, {"clock"}, {"hackernews"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSplitOutAlreadyAloneIsANoOp(t *testing.T) {
	if got := SplitOut(layout(), "hackernews"); !reflect.DeepEqual(got, layout()) {
		t.Errorf("got %v, want it unchanged", got)
	}
}

// TestMovesNeverMutateInput matters because cancelling a layout edit relies on
// the pre-edit slice still being intact.
func TestMovesNeverMutateInput(t *testing.T) {
	moves := map[string]func([][]string, string) [][]string{
		"MoveLeft": MoveLeft, "MoveRight": MoveRight,
		"MoveUp": MoveUp, "MoveDown": MoveDown, "SplitOut": SplitOut,
	}

	for name, move := range moves {
		t.Run(name, func(t *testing.T) {
			original := layout()
			move(original, "clock")
			if !reflect.DeepEqual(original, layout()) {
				t.Errorf("%s mutated its input: %v", name, original)
			}
		})
	}
}

// TestNoWidgetIsEverLost guards the property that matters most: however you
// shuffle, every widget must still be placed exactly once.
func TestNoWidgetIsEverLost(t *testing.T) {
	rows := [][]string{{"a", "b", "c"}, {"d"}, {"e", "f"}}
	moves := []func([][]string, string) [][]string{
		MoveLeft, MoveRight, MoveUp, MoveDown, SplitOut,
	}
	names := []string{"a", "b", "c", "d", "e", "f"}

	// Walk a deterministic pseudo-random sequence of moves.
	for i := 0; i < 300; i++ {
		rows = moves[i%len(moves)](rows, names[(i*7)%len(names)])

		seen := map[string]int{}
		for _, row := range rows {
			if len(row) == 0 {
				t.Fatalf("step %d produced an empty row: %v", i, rows)
			}
			for _, n := range row {
				seen[n]++
			}
		}
		for _, n := range names {
			if seen[n] != 1 {
				t.Fatalf("step %d: %q appears %d times in %v", i, n, seen[n], rows)
			}
		}
	}
}
