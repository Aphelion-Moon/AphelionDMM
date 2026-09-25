package editing

import (
	"sdmm/internal/util"
	"testing"
)

func TestWorkingSelectionIsLevelScopedAndRestrictionIsOptIn(t *testing.T) {
	var state WorkingSelection
	a := RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 3, Y2: 1}, 1)
	state.Set(a)
	if state.Restrict || state.Get(2).Len() != 0 || state.Get(1).Len() != 3 {
		t.Fatal("selection crossed levels or implicitly restricted editing")
	}
	state.Set(RectangleSelection(util.Bounds{X1: 2, Y1: 1, X2: 2, Y2: 1}, 2))
	state.Clear(2)
	if state.Get(1).Len() != 3 || state.Get(2).Len() != 0 {
		t.Fatal("clearing a level affected another level")
	}
}

func TestSelectionSetOperationsPreserveHoles(t *testing.T) {
	a, _ := MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}})
	b := RectangleSelection(util.Bounds{X1: 3, Y1: 1, X2: 4, Y2: 1}, 1)
	for _, c := range []struct {
		op    SelectionOperation
		count int
	}{{SelectionAdd, 3}, {SelectionSubtract, 1}, {SelectionIntersect, 1}, {SelectionReplace, 2}} {
		got := CombineSelection(a, b, c.op)
		if got.Len() != c.count || got.Contains(util.Point{X: 2, Y: 1, Z: 1}) {
			t.Fatalf("operation %v: incorrect membership", c.op)
		}
	}
	if a.Len() != 2 {
		t.Fatal("set operation mutated its input")
	}
}
