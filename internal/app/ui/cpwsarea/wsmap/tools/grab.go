package tools

import (
	"math"
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	"sdmm/internal/aphelion/editing"
	// APHELION EDIT ADDITION END

	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/overlay"
	"sdmm/internal/dmapi/dmmap/dmmdata"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"

	"github.com/rs/zerolog/log"
)

type tSelectMode int

const (
	tSelectModeSelectArea tSelectMode = iota
	tSelectModeMoveArea
)

// ToolGrab can be used to select a specific tiles area and to manipulate the selected area state.
// Tool works in two modes:
//  1. Select the area
//  2. Move the area
//
// The first one is available when no area selected or user selects the area outside the currently selected.
// The second mode is activated automatically when dragging mouse on the currently selected area.
//
// Copy/Paste operations will automatically use selected area for them.
type ToolGrab struct {
	tool

	fillStart    util.Point
	fillAreaInit util.Bounds
	fillArea     util.Bounds

	initTiles []dmmap.Tile
	prevTiles map[util.Point]dmmdata.Prefabs

	startMovePoint util.Point

	dragging bool

	mode tSelectMode
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	move             *editing.Move
	selectionHistory *editing.SelectionHistory
	placement        *grabPlacement
	// APHELION EDIT ADDITION END
}

func (ToolGrab) Name() string {
	return TNGrab
}

func (t *ToolGrab) Bounds() util.Bounds {
	return t.fillArea
}

func (t *ToolGrab) HasSelectedArea() bool {
	return t.fillStart != util.Point{}
}

func (t *ToolGrab) Reset() {
	// APHELION EDIT ADDITION START - PASTE PLACEMENT
	if t.placement != nil {
		if t.placement.controller != nil {
			t.placement.controller.CancelPastePlacement()
		} else if t.placement.move != nil {
			t.placement.owner.FinishSelectionMove(t.placement.move, true)
		}
		t.placement = nil
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	t.selectionHistory = nil
	if t.move != nil {
		ed.FinishSelectionMove(t.move, true)
		t.move = nil
	}
	t.dragging = false
	t.mode = tSelectModeSelectArea
	// APHELION EDIT ADDITION END
	t.fillStart = util.Point{}
	t.fillAreaInit = util.Bounds{}
	t.fillArea = util.Bounds{X1: math.MaxFloat32, Y1: math.MaxFloat32}

	t.initTiles = nil
	t.prevTiles = nil

	log.Print("grab tools reset")
}

func newGrab() *ToolGrab {
	return &ToolGrab{
		fillArea: util.Bounds{X1: math.MaxFloat32, Y1: math.MaxFloat32},
	}
}

func (t *ToolGrab) Stale() bool {
	// APHELION EDIT CHANGE - PASTE PLACEMENT - ORIGINAL: return !t.dragging
	return !t.dragging && !t.Placing()
}

func (ToolGrab) AltBehaviour() bool {
	return false
}

func (t *ToolGrab) SelectArea(tiles []util.Point) {
	if len(tiles) == 0 {
		return
	}
	// APHELION EDIT ADDITION START - SELECTION HISTORY
	t.selectionHistory = nil
	// APHELION EDIT ADDITION END

	t.fillStart = tiles[0]
	for _, tile := range tiles {
		t.selectArea(float64(t.fillArea.X1), float64(t.fillArea.Y1), float64(t.fillArea.X2), float64(t.fillArea.Y2), tile)
	}
	t.stopMoveArea()
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	t.mode = tSelectModeMoveArea
	// APHELION EDIT ADDITION END
}

func (t *ToolGrab) PreSelectArea(tiles []util.Point) {
	t.prevTiles = make(map[util.Point]dmmdata.Prefabs, len(tiles))
	for _, tile := range tiles {
		if ed.Dmm().HasTile(tile) {
			t.prevTiles[tile] = ed.Dmm().GetTile(tile).Instances().Prefabs().Copy()
		}
	}
}

func (t *ToolGrab) process() {
	// APHELION EDIT ADDITION START - PASTE PLACEMENT
	t.processPlacement()
	// APHELION EDIT ADDITION END
	if t.active() {
		ed.OverlayPushArea(t.fillArea, overlay.ColorToolSelectTileFill, overlay.ColorToolSelectTileBorder)
	}
}

func (t *ToolGrab) onStart(coord util.Point) {
	// APHELION EDIT ADDITION START - PASTE PLACEMENT
	if t.Placing() {
		t.clickPlacement(coord)
		return
	}
	// APHELION EDIT ADDITION END
	t.dragging = true

	switch t.mode {
	case tSelectModeSelectArea:
		t.startSelectArea(coord)
	case tSelectModeMoveArea:
		t.startMoveArea(coord)
	}
}

func (t *ToolGrab) startSelectArea(coord util.Point) {
	t.Reset()
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	t.dragging = true
	// APHELION EDIT ADDITION END
	t.fillStart = coord
	t.onMove(coord)
}

func (t *ToolGrab) startMoveArea(coord util.Point) {
	// APHELION EDIT CHANGE - SELECTION LIFECYCLE - ORIGINAL: if t.fillArea.Contains(float32(coord.X), float32(coord.Y)) {
	if coord.Z == t.fillStart.Z && t.fillArea.Contains(float32(coord.X), float32(coord.Y)) {
		// APHELION EDIT ADDITION START - SELECTION ROTATION
		// Undo and remote acknowledgements can replace contents between gestures.
		t.initTiles = collectTiles(ed.Dmm(), t.fillArea, t.fillStart.Z)
		// APHELION EDIT ADDITION END
		t.startMovePoint = coord
		// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
		var err error
		t.move, err = ed.BeginSelectionMove(t.fillArea, t.fillStart.Z)
		if err != nil {
			t.dragging = false
			util.ShowErrorDialog("Unable to move selection: " + err.Error())
		}
		// APHELION EDIT ADDITION END
	} else {
		t.mode = tSelectModeSelectArea
		t.onStart(coord)
	}
}

func (t *ToolGrab) onMove(coord util.Point) {
	// APHELION EDIT ADDITION START - PASTE PLACEMENT
	if t.Placing() {
		t.UpdatePlacement(coord)
		return
	}
	// APHELION EDIT ADDITION END
	if !t.active() {
		return
	}

	switch t.mode {
	case tSelectModeSelectArea:
		x, y := float64(t.fillStart.X), float64(t.fillStart.Y)
		t.selectArea(x, y, x, y, coord)
	case tSelectModeMoveArea:
		t.moveArea(coord)
	}
}

func (t *ToolGrab) selectArea(minX, minY, maxX, maxY float64, coord util.Point) {
	t.fillArea.X1 = float32(math.Min(minX, float64(coord.X)))
	t.fillArea.Y1 = float32(math.Min(minY, float64(coord.Y)))
	t.fillArea.X2 = float32(math.Max(maxX, float64(coord.X)))
	t.fillArea.Y2 = float32(math.Max(maxY, float64(coord.Y)))
	t.fillAreaInit = t.fillArea
}

func (t *ToolGrab) moveArea(coord util.Point) {
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	if t.move == nil {
		return
	}
	if area, err := ed.PreviewSelectionMove(t.move, coord.Minus(t.startMovePoint)); err == nil {
		t.fillArea = area
	}
	/* APHELION EDIT REMOVAL START - SELECTION LIFECYCLE
	dmm := ed.Dmm()

	shift := coord.Minus(t.startMovePoint)
	nextArea := t.fillAreaInit.Plus(float32(shift.X), float32(shift.Y))

	if nextArea.X1 <= 0 || nextArea.Y1 <= 0 || int(nextArea.X2) > dmm.MaxX || int(nextArea.Y2) > dmm.MaxY {
		return
	}

	t.fillArea = nextArea

	var updateCoords []util.Point

	// Clear moved tiles (they're moved tho...)
	for _, initTile := range t.initTiles {
		updateCoords = append(updateCoords, initTile.Coord)
		ed.TileDelete(initTile.Coord)
	}

	// Restore previous tiles content (tiles we've moved through)
	for tile, prevTile := range t.prevTiles {
		updateCoords = append(updateCoords, tile)
		ed.TileReplace(tile, prevTile)
	}

	// Move a content to a new place
	for _, initTile := range t.initTiles {
		nextTilePoint := initTile.Coord.Plus(shift)

		if !dmm.HasTile(initTile.Coord) {
			continue
		}

		tile := dmm.GetTile(nextTilePoint)
		if _, ok := t.prevTiles[tile.Coord]; !ok {
			t.prevTiles[tile.Coord] = tile.Instances().Prefabs().Copy()
		}
		updateCoords = append(updateCoords, tile.Coord)

		ed.TileReplace(nextTilePoint, initTile.Instances().Prefabs())
	}

	ed.UpdateCanvasByCoords(updateCoords)
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION END
}

func (t *ToolGrab) onStop(util.Point) {
	if !t.active() {
		return
	}

	switch t.mode {
	case tSelectModeSelectArea:
		t.stopSelectArea()
	case tSelectModeMoveArea:
		// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
		if t.move != nil {
			move := t.move
			_ = t.trackSelectionTransform(t.fillAreaInit, true, func() (util.Bounds, error) {
				ed.FinishSelectionMove(move, false)
				return move.Bounds(), nil
			})
			t.move = nil
		} else {
			t.stopMoveArea()
		}
		/* APHELION EDIT REMOVAL START - SELECTION LIFECYCLE
		// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: go ed.CommitChanges("Move Grabbed Area")
		ed.CommitOperation("Move Grabbed Area")
		APHELION EDIT REMOVAL END */
		// APHELION EDIT ADDITION END
	}

	t.dragging = false
}

func (t *ToolGrab) stopSelectArea() {
	t.mode = tSelectModeMoveArea
	// APHELION EDIT CHANGE - SELECTION MEMBERSHIP - ORIGINAL: t.initTiles = collectTiles(ed.Dmm(), t.fillArea, t.fillStart.Z)
	t.initTiles = nil
	t.prevTiles = make(map[util.Point]dmmdata.Prefabs)
}

func (t *ToolGrab) stopMoveArea() {
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	if !ed.Dmm().HasTile(util.Point{X: int(t.fillArea.X1), Y: int(t.fillArea.Y1), Z: t.fillStart.Z}) || !ed.Dmm().HasTile(util.Point{X: int(t.fillArea.X2), Y: int(t.fillArea.Y2), Z: t.fillStart.Z}) {
		t.Reset()
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - SELECTION MEMBERSHIP - ORIGINAL: t.initTiles = collectTiles(ed.Dmm(), t.fillArea, t.fillStart.Z)
	t.initTiles = nil
	t.fillAreaInit = t.fillArea
}

func (t *ToolGrab) OnDeselect() {
	t.Reset()
}

func (t *ToolGrab) active() bool {
	return !t.fillStart.Equals(0, 0, 0)
}

func collectTiles(dmm *dmmap.Dmm, area util.Bounds, zLevel int) (tiles []dmmap.Tile) {
	for x := area.X1; x <= area.X2; x++ {
		for y := area.Y1; y <= area.Y2; y++ {
			coord := util.Point{X: int(x), Y: int(y), Z: zLevel}
			tiles = append(tiles, dmm.GetTile(coord).Copy())
		}
	}
	return tiles
}

// APHELION EDIT ADDITION START - SELECTION MEMBERSHIP
// Coordinates are selection geometry, independent of captured object contents.
// Commands materialize their data from the current document when consumed.
func (t *ToolGrab) selectedCoordinates() []util.Point {
	if !t.HasSelectedArea() {
		return nil
	}
	var points []util.Point
	for x := int(t.fillArea.X1); x <= int(t.fillArea.X2); x++ {
		for y := int(t.fillArea.Y1); y <= int(t.fillArea.Y2); y++ {
			points = append(points, util.Point{X: x, Y: y, Z: t.fillStart.Z})
		}
	}
	return points
}

// APHELION EDIT ADDITION END
