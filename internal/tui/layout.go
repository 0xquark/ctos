package tui

// Box is a widget's outer rectangle, including its frame.
type Box struct {
	W, H int
}

// Layout computes the outer box for every widget on a dashboard.
//
// Rows split the available height; widgets within a row split that row's
// width. Remainders go to the earlier rows and earlier columns, so a 3-column
// layout in an 80-cell terminal gives 27/27/26 rather than losing two cells.
func Layout(rows [][]string, w, h int) [][]Box {
	out := make([][]Box, len(rows))
	if len(rows) == 0 || w <= 0 || h <= 0 {
		for i := range rows {
			out[i] = make([]Box, len(rows[i]))
		}
		return out
	}

	rowH, rowExtra := h/len(rows), h%len(rows)

	for i, row := range rows {
		boxes := make([]Box, len(row))
		height := rowH
		if i < rowExtra {
			height++
		}

		if len(row) > 0 {
			colW, colExtra := w/len(row), w%len(row)
			for j := range row {
				width := colW
				if j < colExtra {
					width++
				}
				boxes[j] = Box{W: width, H: height}
			}
		}
		out[i] = boxes
	}
	return out
}

// Inner converts an outer box to the content area a widget may draw in.
func (b Box) Inner() (w, h int) {
	return max(0, b.W-FrameOverheadX), max(0, b.H-FrameOverheadY)
}
