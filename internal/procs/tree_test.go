package procs

import "testing"

// family is a small tree: init - shell - editor, plus two browser children.
func family() []Process {
	return []Process{
		{PID: 1, PPID: 0, Command: "/sbin/init"},
		{PID: 10, PPID: 1, Command: "/bin/zsh"},
		{PID: 20, PPID: 10, Command: "/usr/bin/nvim"},
		{PID: 30, PPID: 1, Command: "/Applications/Arc.app/Contents/MacOS/Arc"},
		{PID: 42, PPID: 30, Command: "helper-b"},
		{PID: 41, PPID: 30, Command: "helper-a"},
	}
}

func TestAncestorsReadTopDown(t *testing.T) {
	ix := NewIndex(family())

	got := ix.Ancestors(20)
	want := []int{1, 10} // outermost first, excluding 20 itself
	if len(got) != len(want) {
		t.Fatalf("chain = %v, want %v", pids(got), want)
	}
	for i := range want {
		if got[i].PID != want[i] {
			t.Fatalf("chain = %v, want %v", pids(got), want)
		}
	}
}

func TestAncestorsOfARootIsEmpty(t *testing.T) {
	ix := NewIndex(family())
	if got := ix.Ancestors(1); len(got) != 0 {
		t.Fatalf("init has ancestors %v, want none", pids(got))
	}
}

// A process whose parent has already exited must not invent a parent, and must
// not walk into whatever else happens to hold that PID.
func TestAncestorsStopAtAMissingParent(t *testing.T) {
	orphan := []Process{{PID: 99, PPID: 12345, Command: "orphan"}}
	ix := NewIndex(orphan)
	if got := ix.Ancestors(99); len(got) != 0 {
		t.Fatalf("orphan has ancestors %v, want none", pids(got))
	}
}

// A corrupt table must not hang the renderer.
func TestAncestorsSurviveACycle(t *testing.T) {
	cyclic := []Process{
		{PID: 1, PPID: 2, Command: "a"},
		{PID: 2, PPID: 1, Command: "b"},
	}
	ix := NewIndex(cyclic)
	if got := ix.Ancestors(1); len(got) > maxAncestry {
		t.Fatalf("cycle produced %d ancestors", len(got))
	}
}

// A process that reports itself as its own parent (PID 1 does on some systems)
// must not become its own child.
func TestSelfParentIsNotAChild(t *testing.T) {
	ix := NewIndex([]Process{{PID: 1, PPID: 1, Command: "init"}})
	if got := ix.Children(1); len(got) != 0 {
		t.Fatalf("init is its own child: %v", pids(got))
	}
	if got := ix.Ancestors(1); len(got) != 0 {
		t.Fatalf("init is its own ancestor: %v", pids(got))
	}
}

func TestChildrenAreOrderedByPID(t *testing.T) {
	ix := NewIndex(family())
	got := ix.Children(30)
	if len(got) != 2 || got[0].PID != 41 || got[1].PID != 42 {
		t.Fatalf("children = %v, want [41 42]", pids(got))
	}
}

func TestDescendantsCountsTheWholeSubtree(t *testing.T) {
	ix := NewIndex(family())
	if got := ix.Descendants(1); got != 5 {
		t.Errorf("descendants of init = %d, want 5", got)
	}
	if got := ix.Descendants(30); got != 2 {
		t.Errorf("descendants of Arc = %d, want 2", got)
	}
	if got := ix.Descendants(20); got != 0 {
		t.Errorf("leaf has %d descendants, want 0", got)
	}
}

func TestGet(t *testing.T) {
	ix := NewIndex(family())
	if p, ok := ix.Get(10); !ok || p.Name() != "zsh" {
		t.Errorf("Get(10) = %+v, %v", p, ok)
	}
	if _, ok := ix.Get(9999); ok {
		t.Error("Get returned a process that is not in the sample")
	}
}

func pids(ps []Process) []int {
	out := make([]int, len(ps))
	for i, p := range ps {
		out[i] = p.PID
	}
	return out
}
