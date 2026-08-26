package tui

// Rearranging a dashboard is pure list surgery on the [][]string that `rows:`
// holds. Keeping it separate from the model makes each move testable without a
// terminal, and makes cancelling a layout edit a matter of keeping the old
// slice around.
//
// The mental model the keys present:
//
//	← →   reorder within the current row
//	↑ ↓   move to the row above or below
//	↵     split out into a new row of its own
//
// Every function returns a fresh copy and never mutates its input.

// clone deep-copies a layout so callers can keep the original to cancel back to.
func clone(rows [][]string) [][]string {
	out := make([][]string, len(rows))
	for i, row := range rows {
		out[i] = append([]string(nil), row...)
	}
	return out
}

// find locates a widget by name.
func find(rows [][]string, name string) (r, c int, ok bool) {
	for i, row := range rows {
		for j, n := range row {
			if n == name {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

// compact drops rows left empty by a move.
func compact(rows [][]string) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 {
			out = append(out, row)
		}
	}
	return out
}

// remove takes a name out of its row, returning the layout and where it was.
func remove(rows [][]string, name string) (out [][]string, r, c int, ok bool) {
	r, c, ok = find(rows, name)
	if !ok {
		return rows, 0, 0, false
	}
	out = clone(rows)
	out[r] = append(out[r][:c], out[r][c+1:]...)
	return out, r, c, true
}

// insert places name into row r at column c, clamping c into range.
func insert(row []string, c int, name string) []string {
	if c < 0 {
		c = 0
	}
	if c > len(row) {
		c = len(row)
	}
	out := make([]string, 0, len(row)+1)
	out = append(out, row[:c]...)
	out = append(out, name)
	return append(out, row[c:]...)
}

// MoveLeft swaps a widget with its left neighbour in the same row.
func MoveLeft(rows [][]string, name string) [][]string {
	r, c, ok := find(rows, name)
	if !ok || c == 0 {
		return rows
	}
	out := clone(rows)
	out[r][c-1], out[r][c] = out[r][c], out[r][c-1]
	return out
}

// MoveRight swaps a widget with its right neighbour in the same row.
func MoveRight(rows [][]string, name string) [][]string {
	r, c, ok := find(rows, name)
	if !ok || c >= len(rows[r])-1 {
		return rows
	}
	out := clone(rows)
	out[r][c], out[r][c+1] = out[r][c+1], out[r][c]
	return out
}

// MoveUp moves a widget into the row above, keeping roughly its column. From
// the top row it creates a new row above, so a widget can always be promoted.
func MoveUp(rows [][]string, name string) [][]string {
	out, r, c, ok := remove(rows, name)
	if !ok {
		return rows
	}
	if r == 0 {
		// Already alone at the top: nothing to promote into.
		if len(rows[0]) == 1 {
			return rows
		}
		out = append([][]string{{name}}, out...)
		return compact(out)
	}
	out[r-1] = insert(out[r-1], c, name)
	return compact(out)
}

// MoveDown moves a widget into the row below, creating one at the bottom when
// there is none.
func MoveDown(rows [][]string, name string) [][]string {
	out, r, c, ok := remove(rows, name)
	if !ok {
		return rows
	}
	if r == len(rows)-1 {
		if len(rows[r]) == 1 {
			return rows
		}
		out = append(out, []string{name})
		return compact(out)
	}
	out[r+1] = insert(out[r+1], c, name)
	return compact(out)
}

// SplitOut gives a widget a new row of its own, directly below its current one.
// A widget already alone in its row stays put.
func SplitOut(rows [][]string, name string) [][]string {
	r, _, ok := find(rows, name)
	if !ok || len(rows[r]) == 1 {
		return rows
	}
	out, r, _, _ := remove(rows, name)

	tail := append([][]string{{name}}, out[r+1:]...)
	out = append(out[:r+1], tail...)
	return compact(out)
}
