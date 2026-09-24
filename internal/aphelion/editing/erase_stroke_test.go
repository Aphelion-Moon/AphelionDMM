package editing

import (
	"fmt"
	"sdmm/internal/util"
	"testing"
)

func TestEraseStrokeCoversCrossedTilesWithoutDrilling(t *testing.T) {
	counts := map[util.Point]int{}
	stroke := NewEraseStroke(32, false, func(x, y, z int, all bool, processed func(util.Point) bool) EraseResult {
		p := util.Point{X: x/32 + 1, Y: y/32 + 1, Z: z}
		if all {
			t.Fatal("stroke mode changed")
		}
		counts[p]++
		return EraseResult{Outcome: MutationChanged, Coord: p}
	})
	stroke.Sample(1, 1, 1)
	stroke.Sample(127, 1, 1)
	stroke.Sample(1, 1, 1)
	if len(counts) != 4 || stroke.Changes() != 4 {
		t.Fatalf("missed crossed cells: %v", counts)
	}
	for _, count := range counts {
		if count != 1 {
			t.Fatal("stroke drilled a stack")
		}
	}
}
func TestEraseStrokeFaultDoesNotMarkTargetProcessed(t *testing.T) {
	p := util.Point{X: 1, Y: 1, Z: 1}
	calls := 0
	stroke := NewEraseStroke(32, true, func(int, int, int, bool, func(util.Point) bool) EraseResult {
		calls++
		return EraseResult{Outcome: MutationFailed, Coord: p, Err: fmt.Errorf("capture failed")}
	})
	stroke.Sample(1, 1, 1)
	stroke.Sample(63, 1, 1)
	if calls != 1 || stroke.Processed(p) || stroke.Changes() != 0 || stroke.Err() == nil {
		t.Fatal("failed capture poisoned coverage or kept mutating")
	}
}
func TestEraseStrokeFindsOffsetHitInsideSameTile(t *testing.T) {
	hit := false
	stroke := NewEraseStroke(32, false, func(x, y, z int, _ bool, processed func(util.Point) bool) EraseResult {
		if x < 15 {
			return EraseResult{Outcome: MutationEmpty}
		}
		hit = true
		return EraseResult{Outcome: MutationChanged, Coord: util.Point{X: 2, Y: 1, Z: 1}}
	})
	stroke.Sample(1, 1, 1)
	stroke.Sample(20, 1, 1)
	if !hit || !stroke.Processed(util.Point{X: 2, Y: 1, Z: 1}) {
		t.Fatal("empty sample suppressed later sprite hit")
	}
}
