package tools

import (
	// APHELION EDIT ADDITION START - DETERMINISTIC ERASER
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"time"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/overlay"
	"sdmm/internal/util"
)

// ToolDelete can be used to delete a hovered object instance.
// It has an alt behaviour which is able to delete all instances on the tile.
type ToolDelete struct {
	tool

	/* APHELION EDIT REMOVAL START - DETERMINISTIC ERASER
	deletedTiles map[util.Point]bool
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - DETERMINISTIC ERASER
	stroke         *editing.EraseStroke
	strokeLevel    int
	shapeStroke    *editing.ShapeStroke
	shapeFence     editing.ShapeDeleteFence
	shapeFilter    dm.PathsFilter
	shapeAll       bool
	shapeReleased  bool
	shapeCursor    *editing.SelectionCursor
	shapeTargets   editing.ShapeDeleteTargets
	shapeAdmission *resources.Reservation
	shapeSelection editing.Selection
	// APHELION EDIT ADDITION END
}

func (t ToolDelete) IgnoreBounds() bool {
	return !t.AltBehaviour()
}

func (ToolDelete) Name() string {
	return TNDelete
}

func newDelete() *ToolDelete {
	return &ToolDelete{
		/* APHELION EDIT REMOVAL START - DETERMINISTIC ERASER
		deletedTiles: make(map[util.Point]bool),
		APHELION EDIT REMOVAL END */
	}
}

func (t *ToolDelete) process() {
	// APHELION EDIT ADDITION START - SHARED SHAPES
	if t.shapeStroke != nil {
		ready := t.shapeStroke.Advance()
		showShapeSelection(t.shapeStroke.Selection())
		if ready && t.shapeReleased {
			t.advanceShapeDelete()
		}
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - DETERMINISTIC ERASER
	if t.stroke != nil && t.stroke.All() {
		t.stroke.VisitProcessed(func(p util.Point) {
			ed.OverlayPushTile(p, overlay.ColorToolDeleteAltTileFill, overlay.ColorToolDeleteAltTileBorder)
		})
	}
	// APHELION EDIT ADDITION END
	/* APHELION EDIT REMOVAL START - DETERMINISTIC ERASER
	for coord := range t.deletedTiles {
		if t.AltBehaviour() {
			ed.OverlayPushTile(coord, overlay.ColorToolDeleteAltTileFill, overlay.ColorToolDeleteAltTileBorder)
		}
	}
	APHELION EDIT REMOVAL END */
}

func (t *ToolDelete) onStart(coord util.Point) {
	// APHELION EDIT ADDITION START - SHARED SHAPES
	if t.shapeStroke != nil {
		return
	}
	if shapeBrushEnabled() {
		owner, ok := ed.(interface {
			BeginShapeDelete(int) (editing.ShapeDeleteFence, error)
		})
		if !ok {
			util.ShowErrorDialog("shape eraser is unavailable")
			return
		}
		fence, err := owner.BeginShapeDelete(coord.Z)
		if err != nil {
			util.ShowErrorDialog(err.Error())
			return
		}
		stroke, err := beginShapeStroke(coord)
		if err != nil {
			util.ShowErrorDialog(err.Error())
			return
		}
		t.shapeStroke = stroke
		t.shapeReleased = false
		t.shapeFilter = brushFilter()
		t.shapeAll = t.tool.AltBehaviour()
		t.shapeFence = fence
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - DETERMINISTIC ERASER
	owner, ok := ed.(interface {
		StartEraseStroke(bool) (*editing.EraseStroke, error)
	})
	if !ok {
		return
	}
	stroke, err := owner.StartEraseStroke(t.tool.AltBehaviour())
	if err != nil {
		util.ShowErrorDialog(err.Error())
		return
	}
	t.stroke = stroke
	t.strokeLevel = coord.Z
	t.onMove(coord)
	/* APHELION EDIT REMOVAL START - DETERMINISTIC ERASER
		if t.AltBehaviour() {
			t.onMove(coord)
		} else if hoveredInstance := ed.HoveredInstance(); hoveredInstance != nil {
			ed.InstanceDelete(hoveredInstance)
			// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: go ed.CommitChanges("Delete Instance")
			 ed.CommitOperation("Delete Instance")
		}
	}

	func (t *ToolDelete) onMove(coord util.Point) {
		if t.AltBehaviour() && !t.deletedTiles[coord] {
			t.deletedTiles[coord] = true // Don't delete to the same tile twice
			ed.TileDeleteSelected()
			ed.UpdateCanvasByCoords([]util.Point{coord})
		}
	}

	func (t *ToolDelete) onStop(util.Point) {
		if len(t.deletedTiles) != 0 {
			t.deletedTiles = make(map[util.Point]bool, len(t.deletedTiles))
			// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: go ed.CommitChanges("Delete Tiles")
			ed.CommitOperation("Delete Tiles")
		}
	}
	 APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - DETERMINISTIC ERASER
func (t *ToolDelete) AltBehaviour() bool {
	if t.stroke != nil {
		return t.stroke.All()
	}
	if t.shapeStroke != nil {
		return t.shapeAll
	}
	return t.tool.AltBehaviour()
}
func (t *ToolDelete) onMove(coord util.Point) {
	if t.shapeReleased {
		return
	}
	if t.shapeStroke != nil {
		t.shapeStroke.Queue(coord)
		return
	}
	if t.stroke == nil {
		return
	}
	x, y := (coord.X-1)*dmmap.WorldIconSize+dmmap.WorldIconSize/2, (coord.Y-1)*dmmap.WorldIconSize+dmmap.WorldIconSize/2
	if state, ok := cs.(interface {
		RelMouseX() int
		RelMouseY() int
	}); ok {
		x, y = state.RelMouseX(), state.RelMouseY()
	}
	t.stroke.Sample(x, y, t.strokeLevel)
}
func (t *ToolDelete) onStop(util.Point) {
	if t.shapeStroke != nil {
		t.shapeReleased = true
		return
	}
	t.finishErase()
}
func (t *ToolDelete) advanceShapeDelete() {
	if t.shapeSelection.Len() == 0 {
		t.shapeSelection = t.shapeStroke.Selection()
	}
	if t.shapeAll {
		t.finishShape()
		return
	}
	if t.shapeCursor == nil {
		t.shapeSelection = t.shapeStroke.Selection()
		budget := resources.DefaultBudget()
		if owner, ok := ed.(interface{ ShapeDeleteBudget() *resources.Budget }); ok {
			budget = owner.ShapeDeleteBudget()
		}
		// One stable ID and source coordinate per sample, including map overhead.
		admission, err := budget.Reserve(1024 + uint64(t.shapeSelection.Len())*160)
		if err != nil {
			t.cancelShape()
			util.ShowErrorDialog(err.Error())
			return
		}
		t.shapeAdmission = admission
		t.shapeCursor = t.shapeSelection.Cursor()
		t.shapeTargets = make(editing.ShapeDeleteTargets)
	}
	owner, ok := ed.(interface {
		PickShapeDeleteTarget(util.Point, editing.Selection, dm.PathsFilter, editing.ShapeDeleteFence) (editing.ShapeDeleteTarget, bool, error)
	})
	if !ok {
		t.cancelShape()
		util.ShowErrorDialog("shape eraser picking is unavailable")
		return
	}
	started := time.Now()
	for count := 0; count < 128; count++ {
		point, more := t.shapeCursor.Next()
		if !more {
			t.finishShape()
			return
		}
		target, found, err := owner.PickShapeDeleteTarget(point, t.shapeSelection, t.shapeFilter, t.shapeFence)
		if err != nil {
			t.cancelShape()
			util.ShowErrorDialog(err.Error())
			return
		}
		if found {
			t.shapeTargets[string(target.StableID)] = target.Coord
		}
		if time.Since(started) >= 2*time.Millisecond {
			return
		}
	}
}
func (t *ToolDelete) finishShape() {
	owner, ok := ed.(interface {
		EraseShape(editing.Selection, bool, dm.PathsFilter, editing.ShapeDeleteFence, editing.ShapeDeleteTargets, ...*resources.Reservation) error
	})
	if !ok {
		t.cancelShape()
		util.ShowErrorDialog("shape eraser is unavailable")
		return
	}
	err := owner.EraseShape(t.shapeSelection, t.shapeAll, t.shapeFilter, t.shapeFence, t.shapeTargets, t.shapeAdmission)
	t.shapeAdmission = nil // The edit owner releases it after direct or worker completion.
	t.cancelShape()
	if err != nil {
		util.ShowErrorDialog(err.Error())
	}
}
func (t *ToolDelete) cancelShape() {
	t.shapeAdmission.Release()
	t.shapeAdmission = nil
	t.shapeStroke = nil
	t.shapeFence = editing.ShapeDeleteFence{}
	t.shapeFilter = dm.PathsFilter{}
	t.shapeAll = false
	t.shapeReleased = false
	t.shapeCursor = nil
	t.shapeTargets = nil
	t.shapeSelection = editing.Selection{}
}
func (t *ToolDelete) finishErase() {
	stroke := t.stroke
	if stroke == nil {
		return
	}
	t.stroke = nil
	if owner, ok := ed.(interface{ FinishEraseStroke(*editing.EraseStroke) }); ok {
		owner.FinishEraseStroke(stroke)
	}
}
func (t *ToolDelete) OnDeselect() {
	t.shapeReleased = false
	if t.shapeStroke != nil {
		t.cancelShape()
		return
	}
	t.onStop(util.Point{})
}

// APHELION EDIT ADDITION END
