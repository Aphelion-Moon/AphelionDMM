package editing

import (
	"testing"

	"sdmm/internal/util"
)

func TestSelectionMovePoseIsCheapAndSparse(t *testing.T) {
	selection, err := MaskSelection([]util.Point{{X: 2, Y: 2, Z: 1}, {X: 4, Y: 2, Z: 1}})
	if err != nil {
		t.Fatal(err)
	}
	move, err := NewSelectionMove(selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := move.Update(util.Point{X: 1}, 8, 8, 1); err != nil || !changed {
		t.Fatalf("first translated pose changed=%v err=%v", changed, err)
	}
	if got, want := move.Bounds(), (util.Bounds{X1: 3, Y1: 2, X2: 5, Y2: 2}); got != want {
		t.Fatalf("moved bounds %v, want %v", got, want)
	}
	if !selection.Contains(util.Point{X: 2, Y: 2, Z: 1}) || selection.Contains(util.Point{X: 3, Y: 2, Z: 1}) {
		t.Fatal("pose update changed sparse source membership")
	}
	if _, changed, err := move.Update(util.Point{X: 1}, 8, 8, 1); err != nil || changed {
		t.Fatalf("same pose should do no work, changed=%v err=%v", changed, err)
	}
	if _, _, err := move.Update(util.Point{X: 5}, 8, 8, 1); err == nil {
		t.Fatal("out-of-bounds pose was accepted")
	}
	if move.Shift() != (util.Point{X: 1}) {
		t.Fatalf("invalid pose changed current shift to %v", move.Shift())
	}
	move.Finish()
	if !move.Closed() {
		t.Fatal("move pose did not close")
	}
}
