package tools

import (
	// APHELION EDIT ADDITION START - DETERMINISTIC ERASER
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmmap"
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
	stroke      *editing.EraseStroke
	strokeLevel int
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
	return t.tool.AltBehaviour()
}
func (t *ToolDelete) onMove(coord util.Point) {
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
	stroke := t.stroke
	if stroke == nil {
		return
	}
	t.stroke = nil
	if owner, ok := ed.(interface{ FinishEraseStroke(*editing.EraseStroke) }); ok {
		owner.FinishEraseStroke(stroke)
	}
}
func (t *ToolDelete) OnDeselect() { t.onStop(util.Point{}) }

// APHELION EDIT ADDITION END
