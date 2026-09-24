package model

import (
	"reflect"
	"testing"
)

func TestSameOperationLeavesBorrowedPayloadsUntouched(t *testing.T) {
	left := Operation{Changes: []TileChange{{Coord: Coord{X: 2, Y: 1, Z: 1}, After: TileState{Prefabs: []PrefabState{{Path: "/obj/unknown", Vars: map[string]string{"raw": `list("a"=1)`}}}}}, {Coord: Coord{X: 1, Y: 1, Z: 1}}}}
	right := CloneOperation(left)
	right.Changes[0], right.Changes[1] = right.Changes[1], right.Changes[0]
	beforeLeft, beforeRight := CloneOperation(left), CloneOperation(right)
	if !SameOperation(left, right) {
		t.Fatal("order-independent exact retry differs")
	}
	if !reflect.DeepEqual(left, beforeLeft) || !reflect.DeepEqual(right, beforeRight) {
		t.Fatal("comparison mutated caller payload")
	}
	right.Changes[1].After.Prefabs[0].Vars["raw"] = "changed"
	if SameOperation(left, right) {
		t.Fatal("altered body matched")
	}
}
