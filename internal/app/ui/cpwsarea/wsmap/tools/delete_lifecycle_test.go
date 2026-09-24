package tools

import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
	"testing"
)

type eraserOwner struct {
	*lifecycleEditor
	finished int
}

func (e *eraserOwner) StartEraseStroke(all bool) (*editing.EraseStroke, error) {
	return editing.NewEraseStroke(32, all, func(x, y, z int, all bool, processed func(util.Point) bool) editing.EraseResult {
		return editing.EraseResult{Outcome: editing.MutationChanged, Coord: util.Point{X: editing.StrokeTile(x, 32), Y: editing.StrokeTile(y, 32), Z: z}}
	}), nil
}
func (e *eraserOwner) FinishEraseStroke(*editing.EraseStroke) { e.finished++ }
func TestEraseStrokeToolFreezesModeAndFinishesOnce(t *testing.T) {
	_, base := lifecycleFixture(t)
	owner := &eraserOwner{lifecycleEditor: base}
	previousSize := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	defer func() { dmmap.WorldIconSize = previousSize }()
	ed = owner
	previous := cs
	cs = nil
	defer func() { cs = previous }()
	tool := newDelete()
	tool.setAltBehaviour(true)
	tool.onStart(util.Point{X: 1, Y: 1, Z: 1})
	tool.setAltBehaviour(false)
	if !tool.AltBehaviour() {
		t.Fatal("mode changed within stroke")
	}
	tool.onMove(util.Point{X: 3, Y: 1, Z: 1})
	if tool.stroke.Changes() != 3 {
		t.Fatal("stroke omitted crossed tiles")
	}
	tool.onStop(util.Point{})
	tool.OnDeselect()
	tool.onStop(util.Point{})
	if owner.finished != 1 || tool.AltBehaviour() {
		t.Fatal("release/interruption duplicated completion or retained old mode")
	}
}
