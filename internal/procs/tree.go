package procs

import "sort"

// Index is a process table keyed for lookup, so ancestry questions do not
// rescan the slice for every answer.
type Index struct {
	byPID    map[int]Process
	children map[int][]int
}

// NewIndex builds a lookup over a sample.
func NewIndex(ps []Process) *Index {
	ix := &Index{
		byPID:    make(map[int]Process, len(ps)),
		children: make(map[int][]int),
	}
	for _, p := range ps {
		ix.byPID[p.PID] = p
	}
	for _, p := range ps {
		// Only record a parent link the sample can actually resolve, so a
		// process whose parent has exited does not create a phantom root.
		if p.PPID != p.PID {
			if _, ok := ix.byPID[p.PPID]; ok {
				ix.children[p.PPID] = append(ix.children[p.PPID], p.PID)
			}
		}
	}
	for ppid := range ix.children {
		sort.Ints(ix.children[ppid])
	}
	return ix
}

// Get returns a process by PID.
func (ix *Index) Get(pid int) (Process, bool) {
	p, ok := ix.byPID[pid]
	return p, ok
}

// maxAncestry bounds the walk toward init. Nothing legitimate is this deep;
// the bound exists so a corrupt PPID cycle cannot hang the render.
const maxAncestry = 64

// Ancestors returns the chain from the outermost ancestor down to pid's
// parent. The process itself is not included. A process whose parent is not in
// the sample returns an empty chain.
func (ix *Index) Ancestors(pid int) []Process {
	var chain []Process
	seen := map[int]bool{pid: true}

	cur, ok := ix.byPID[pid]
	for i := 0; ok && i < maxAncestry; i++ {
		parent, found := ix.byPID[cur.PPID]
		if !found || seen[parent.PID] {
			break
		}
		seen[parent.PID] = true
		chain = append(chain, parent)
		cur = parent
	}

	// Walked upward; the caller wants to read downward.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// Children returns pid's direct children, ordered by PID.
func (ix *Index) Children(pid int) []Process {
	ids := ix.children[pid]
	out := make([]Process, 0, len(ids))
	for _, id := range ids {
		if p, ok := ix.byPID[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

// Descendants counts everything below pid, so the detail pane can say how much
// is hiding under a collapsed subtree.
func (ix *Index) Descendants(pid int) int {
	n := 0
	stack := []int{pid}
	seen := map[int]bool{pid: true}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, kid := range ix.children[cur] {
			if seen[kid] {
				continue
			}
			seen[kid] = true
			n++
			stack = append(stack, kid)
		}
	}
	return n
}
