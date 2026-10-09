package tools

import (
	// APHELION EDIT ADDITION START - HELD ROTATION
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/overlay"
	"sdmm/internal/util"
)

// ToolAdd can be used to add prefabs to the map.
// During mouse moving when the tool is active a selected prefab will be added on every tile under the mouse.
// You can't add the same prefab twice on the same tile during the one OnStart -> OnStop cycle.
//
// Default: obj placed on top, area and turfs are replaced.
// Alternative: obj replaced, area and turfs are placed on top.
type ToolAdd struct {
	tool

	editedTiles map[util.Point]bool
	// APHELION EDIT ADDITION START - HELD ROTATION
	held          editing.HeldPrefab
	shapeStroke   *editing.ShapeStroke
	shapeContext  ActionContext
	shapePrefab   *dmmprefab.Prefab
	shapeFilter   dm.PathsFilter
	shapeReplace  bool
	shapeReleased bool
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PLACEMENT LINT
	lint        maplint.PlacementReport
	lintSkipped map[util.Point]bool
	// APHELION EDIT ADDITION END
}

func (ToolAdd) Name() string {
	return TNAdd
}

func newAdd() *ToolAdd {
	return &ToolAdd{
		editedTiles: make(map[util.Point]bool),
	}
}

func (t *ToolAdd) process() {
	// APHELION EDIT ADDITION START - SHARED SHAPES
	if t.shapeStroke != nil {
		ready := t.shapeStroke.Advance()
		showShapeSelection(t.shapeStroke.Selection())
		if ready && t.shapeReleased {
			t.finishShape()
		}
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - HELD ROTATION
	t.showHeld()
	// APHELION EDIT ADDITION END
	for coord := range t.editedTiles {
		if t.AltBehaviour() {
			ed.OverlayPushTile(coord, overlay.ColorToolAddAltTileFill, overlay.ColorToolAddAltTileBorder)
		} else {
			ed.OverlayPushTile(coord, overlay.ColorToolAddTileFill, overlay.ColorToolAddTileBorder)
		}
	}
}

func (t *ToolAdd) onStart(coord util.Point) {
	// APHELION EDIT ADDITION START - SHARED SHAPES
	if t.shapeStroke != nil {
		return
	}
	if shapeBrushEnabled() {
		prefab, ok := t.HeldPrefab()
		if !ok {
			return
		}
		stroke, err := beginShapeStroke(coord)
		if err != nil {
			util.ShowErrorDialog(err.Error())
			return
		}
		t.shapeStroke = stroke
		t.shapeContext = t.gestureContext
		t.shapeReleased = false
		t.shapePrefab, t.shapeFilter, t.shapeReplace = prefab, brushFilter(), t.AltBehaviour()
		return
	}
	// APHELION EDIT ADDITION END
	t.onMove(coord)
}

func (t *ToolAdd) onMove(coord util.Point) {
	// APHELION EDIT ADDITION START - SHARED SHAPES
	if t.shapeReleased {
		return
	}
	if t.shapeStroke != nil {
		t.shapeStroke.Queue(coord)
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - HELD ROTATION - ORIGINAL: if prefab, ok := ed.SelectedPrefab(); ok && !t.editedTiles[coord] {
	if prefab, ok := t.HeldPrefab(); ok && !t.editedTiles[coord] {
		// APHELION EDIT ADDITION START - PLACEMENT LINT
		if t.lintGuard(coord, prefab) {
			return
		}
		// APHELION EDIT ADDITION END
		t.editedTiles[coord] = true // Don't add to the same tile twice

		tile := ed.Dmm().GetTile(coord)
		t.basicPrefabAdd(tile, prefab)

		ed.UpdateCanvasByCoords([]util.Point{coord})
	}
}

func (t *ToolAdd) onStop(util.Point) {
	// APHELION EDIT ADDITION START - SHARED SHAPES
	if t.shapeStroke != nil {
		t.shapeReleased = true
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PLACEMENT LINT
	t.lint, t.lintSkipped = maplint.PlacementReport{}, nil
	// APHELION EDIT ADDITION END
	if len(t.editedTiles) != 0 {
		t.editedTiles = make(map[util.Point]bool, len(t.editedTiles))
		// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: go ed.CommitChanges("Add Atoms")
		ed.CommitOperation("Add Atoms")
	}
}

// APHELION EDIT ADDITION START - SHARED SHAPES
func (t *ToolAdd) finishShape() {
	selection := t.shapeStroke.Selection()
	t.shapeStroke = nil
	t.shapeContext = ActionContext{}
	t.shapeReleased = false
	if selection.Len() > 0 {
		if err := fillShape(selection, t.shapePrefab, t.shapeReplace, t.shapeFilter); err != nil {
			util.ShowErrorDialog(err.Error())
		}
	}
	t.shapePrefab = nil
}

// APHELION EDIT ADDITION END

// APHELION EDIT ADDITION START - PLACEMENT LINT
// lintGuard checks one stroke tile against the repository lint rules before it
// is edited. Violations only warn; an exact duplicate under an `identical: true`
// rule is skipped. It reports true when the tile must be left untouched.
func (t *ToolAdd) lintGuard(coord util.Point, prefab *dmmprefab.Prefab) bool {
	linter, ok := ed.(placementLinter)
	if !ok {
		return false
	}
	if t.lintSkipped[coord] {
		return true
	}
	verdict := linter.EvaluatePlacement(coord, prefab, t.AltBehaviour())
	if len(verdict.Violations) == 0 {
		return false
	}
	if verdict.Skip {
		if t.lintSkipped == nil {
			t.lintSkipped = make(map[util.Point]bool)
		}
		t.lintSkipped[coord] = true
		t.lint.Skipped++
		t.lint.SkippedPath = prefab.Path()
	} else {
		t.lint.Warned++
		t.lint.X, t.lint.Y, t.lint.Z = coord.X, coord.Y, coord.Z
		t.lint.Placed = maplint.AtomFromPrefab(prefab)
		t.lint.Summary = verdict.Summary()
		t.lint.CanReplace = len(verdict.Replace) != 0
	}
	linter.RecordPlacementLint(t.lint)
	return verdict.Skip
}

// APHELION EDIT ADDITION END
