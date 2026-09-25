package tools

import (
	"fmt"
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
	previewMove         *editing.SelectionMove
	selectionHistory    *editing.SelectionHistory
	placement           *grabPlacement
	selection           editing.Selection
	selectionOwner      *dmmap.Dmm
	AreaMode            bool
	AllMatchingAreas    bool
	SelectionOperation  editing.SelectionOperation
	selectionOperation  editing.SelectionOperation
	gestureSelection    editing.Selection
	selectionAnchor     util.Point
	toggleClick         bool
	ctrlSelection       bool
	altSelection        bool
	shape               editing.ShapeDescriptor
	areaQuery           *editing.AreaSelectionQuery
	areaQueryGeneration uint64
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

// APHELION EDIT ADDITION START - PERSISTENT SELECTION
func (t *ToolGrab) Reset() {
	// Explicit deselect is independent of cancellation/tool switching.
	if source, ok := ed.(workingSelectionOwner); ok {
		source.WorkingSelection().Clear(source.ActiveLevel())
	}
	t.resetGesture()
}

// APHELION EDIT ADDITION END

// APHELION EDIT CHANGE - PERSISTENT SELECTION - ORIGINAL: func (t *ToolGrab) Reset() {
func (t *ToolGrab) resetGesture() {
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
	t.selection = editing.Selection{}
	t.selectionOwner = nil
	t.gestureSelection = editing.Selection{}
	t.areaQuery = nil
	if t.previewMove != nil {
		if owner, ok := ed.(selectionMovePreviewOwner); ok {
			_ = owner.FinishSelectionMovePreview(t.previewMove, true)
		}
		t.previewMove = nil
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
	return t.areaQuery == nil && !t.dragging && !t.Placing() && (t.previewMove == nil || t.previewMove.Closed())
}

func (ToolGrab) AltBehaviour() bool {
	// APHELION EDIT CHANGE - PERSISTENT SELECTION - ORIGINAL: return false
	return true
}

func (t *ToolGrab) SelectArea(tiles []util.Point) {
	if len(tiles) == 0 {
		return
	}
	// APHELION EDIT ADDITION START - SELECTION HISTORY
	t.selectionHistory = nil
	// APHELION EDIT ADDITION END

	t.fillStart = tiles[0]
	// APHELION EDIT ADDITION START - SELECTION MEMBERSHIP
	t.fillArea = util.Bounds{X1: float32(tiles[0].X), Y1: float32(tiles[0].Y), X2: float32(tiles[0].X), Y2: float32(tiles[0].Y)}
	// APHELION EDIT ADDITION END
	for _, tile := range tiles {
		t.selectArea(float64(t.fillArea.X1), float64(t.fillArea.Y1), float64(t.fillArea.X2), float64(t.fillArea.Y2), tile)
	}
	t.stopMoveArea()
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	t.mode = tSelectModeMoveArea
	t.publishSelection()
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
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	t.processAreaQuery()
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PASTE PLACEMENT
	t.processPlacement()
	// APHELION EDIT ADDITION END
	if t.active() {
		// APHELION EDIT ADDITION START - SELECTION MEMBERSHIP
		selection := t.Selection()
		if selection.Sparse() {
			selection.VisitRuns(func(area util.Bounds) {
				ed.OverlayPushArea(area, overlay.ColorToolSelectTileFill, overlay.ColorToolSelectTileBorder)
			})
		} else {
			ed.OverlayPushArea(t.fillArea, overlay.ColorToolSelectTileFill, overlay.ColorToolSelectTileBorder)
		}
		// APHELION EDIT ADDITION END
	}
}

func (t *ToolGrab) onStart(coord util.Point) {
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	if t.areaQuery != nil {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PASTE PLACEMENT
	if t.Placing() {
		t.clickPlacement(coord)
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	t.gestureSelection = t.Selection()
	if t.ctrlSelection || t.altSelection || t.SelectionOperation != editing.SelectionReplace {
		op := t.SelectionOperation
		if t.ctrlSelection {
			op = editing.SelectionAdd
		}
		t.startSelectionGesture(coord, op, t.altSelection || t.AreaMode && !t.ctrlSelection, t.ctrlSelection && !t.altSelection)
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
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	t.startSelectionGesture(coord, editing.SelectionReplace, t.AreaMode, false)
	return
	// APHELION EDIT ADDITION END
	/* APHELION EDIT REMOVAL START - PERSISTENT SELECTION
	t.Reset()
	// APHELION EDIT ADDITION START - AREA SELECTION
	if t.AreaMode {
		selection, err := editing.SelectAreaMask(ed.Dmm(), coord, t.AllMatchingAreas)
		if err != nil {
			util.ShowErrorDialog(err.Error())
			return
		}
		t.setSelection(selection)
		t.mode = tSelectModeMoveArea
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	t.dragging = true
	// APHELION EDIT ADDITION END
	t.fillStart = coord
	t.onMove(coord)
	APHELION EDIT REMOVAL END */
}

func (t *ToolGrab) startMoveArea(coord util.Point) {
	// APHELION EDIT CHANGE - SELECTION LIFECYCLE - ORIGINAL: if t.fillArea.Contains(float32(coord.X), float32(coord.Y)) {
	if t.Selection().Contains(coord) {
		// APHELION EDIT ADDITION START - SELECTION ROTATION
		// Undo and remote acknowledgements can replace contents between gestures.
		t.initTiles = nil
		// APHELION EDIT ADDITION END
		t.startMovePoint = coord
		// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
		var err error
		t.previewMove, err = t.beginSelectionMovePreview()
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
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	if t.areaQuery != nil {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PASTE PLACEMENT
	if t.Placing() {
		t.UpdatePlacement(coord)
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - PERSISTENT SELECTION - ORIGINAL: if !t.active() {
	if !t.active() && !t.dragging {
		return
	}

	switch t.mode {
	case tSelectModeSelectArea:
		/* APHELION EDIT REMOVAL START - PERSISTENT SELECTION
		x, y := float64(t.fillStart.X), float64(t.fillStart.Y)
		t.selectArea(x, y, x, y, coord)
		APHELION EDIT REMOVAL END */
		// APHELION EDIT ADDITION START - PERSISTENT SELECTION
		anchor := t.selectionAnchor
		area := util.Bounds{X1: float32(anchor.X), Y1: float32(anchor.Y), X2: float32(coord.X), Y2: float32(coord.Y)}
		selection := editing.RectangleSelection(area, anchor.Z)
		if t.shape.Kind != editing.ShapeRectangle || t.shape.Outline {
			d := t.shape
			bounds := selection.Bounds()
			d.Width = int(bounds.X2-bounds.X1) + 1
			d.Height = int(bounds.Y2-bounds.Y1) + 1
			if d.Kind == editing.ShapeCircle {
				d.Width = min(d.Width, d.Height)
				d.Height = d.Width
			}
			if shaped, err := editing.ShapeSelection(d, util.Point{X: int(bounds.X1), Y: int(bounds.Y1), Z: anchor.Z}); err == nil {
				selection = shaped
			}
		}
		op := t.selectionOperation
		if t.toggleClick && coord == anchor && t.gestureSelection.Contains(coord) {
			op = editing.SelectionSubtract
		}
		t.setSelection(editing.CombineSelection(t.gestureSelection, selection, op))
		// APHELION EDIT ADDITION END
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
	// APHELION EDIT ADDITION START - SELECTION MEMBERSHIP
	t.selection = editing.RectangleSelection(t.fillArea, t.fillStart.Z)
	t.selectionOwner = ed.Dmm()
	// APHELION EDIT ADDITION END
}

func (t *ToolGrab) moveArea(coord util.Point) {
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	if t.previewMove == nil {
		return
	}
	if owner, ok := ed.(selectionMovePreviewOwner); ok {
		if area, err := owner.PreviewSelectionMovePreview(t.previewMove, coord.Minus(t.startMovePoint)); err == nil {
			t.fillArea = area
		}
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
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	if t.areaQuery != nil {
		t.dragging = false
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - PERSISTENT SELECTION - ORIGINAL: if !t.active() {
	if !t.active() && !t.dragging {
		return
	}

	switch t.mode {
	case tSelectModeSelectArea:
		t.stopSelectArea()
	case tSelectModeMoveArea:
		// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
		if t.previewMove != nil {
			move := t.previewMove
			if err := t.trackSelectionTransform(t.fillAreaInit, true, func() (util.Bounds, error) {
				owner, ok := ed.(selectionMovePreviewOwner)
				if !ok {
					return move.Bounds(), fmt.Errorf("selection move presentation is unavailable")
				}
				return move.Bounds(), owner.FinishSelectionMovePreview(move, false)
			}); err != nil {
				util.ShowErrorDialog("Unable to move selection: " + err.Error())
			}
			t.previewMove = nil
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
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	t.publishSelection()
	// APHELION EDIT ADDITION END
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
	// APHELION EDIT ADDITION START - PASTE COMMIT OWNERSHIP
	if t.Placing() {
		t.CancelPlacement()
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - PERSISTENT SELECTION - ORIGINAL: t.Reset()
	t.CancelGesture()
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
	return t.Selection().Coordinates()
}

func (t *ToolGrab) Selection() editing.Selection {
	if t.selectionOwner != nil && (ed == nil || t.selectionOwner != ed.Dmm()) {
		return editing.Selection{}
	}
	if t.selection.Len() == 0 {
		return editing.RectangleSelection(t.fillArea, t.fillStart.Z)
	}
	a := t.selection.Bounds()
	return t.selection.Translate(util.Point{X: int(t.fillArea.X1 - a.X1), Y: int(t.fillArea.Y1 - a.Y1)})
}
func (t *ToolGrab) setSelection(s editing.Selection) {
	t.selection = s
	t.selectionOwner = ed.Dmm()
	t.fillArea = s.Bounds()
	t.fillAreaInit = s.Bounds()
	t.fillStart = util.Point{X: int(s.Bounds().X1), Y: int(s.Bounds().Y1), Z: s.Level()}
	t.initTiles = nil
	if s.Len() == 0 {
		t.fillStart = util.Point{}
	}
}
func (t *ToolGrab) SelectMask(points []util.Point) error {
	s, err := editing.MaskSelection(points)
	if err != nil {
		return err
	}
	t.Reset()
	if s.Len() != 0 {
		t.setSelection(s)
		t.mode = tSelectModeMoveArea
	}
	t.publishSelection()
	return nil
}

type selectionMovePreviewOwner interface {
	BeginSelectionMovePreview(editing.Selection) (*editing.SelectionMove, error)
	PreviewSelectionMovePreview(*editing.SelectionMove, util.Point) (util.Bounds, error)
	FinishSelectionMovePreview(*editing.SelectionMove, bool) error
}

func (t *ToolGrab) beginSelectionMovePreview() (*editing.SelectionMove, error) {
	owner, ok := ed.(selectionMovePreviewOwner)
	if !ok {
		return nil, fmt.Errorf("selection move presentation is unavailable")
	}
	return owner.BeginSelectionMovePreview(t.Selection())
}

// APHELION EDIT ADDITION END
