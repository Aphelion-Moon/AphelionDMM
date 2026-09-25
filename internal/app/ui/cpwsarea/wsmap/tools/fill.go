package tools

import (
	"math"
	// APHELION EDIT ADDITION START - BOUNDED FILL
	"context"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	// APHELION EDIT ADDITION END

	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/overlay"

	// APHELION EDIT REMOVAL START - SHARED TOOL FEEDBACK
	// "sdmm/internal/imguiext"
	// APHELION EDIT REMOVAL END
	"sdmm/internal/util"
)

// ToolFill can be used to add prefabs to the map by filling the provided area.
// During mouse moving when the tool is active it will mark the area to fill.
// On stop the tool will fill the area a user has made.
//
// Default: obj place on top, area and turfs are replaced.
// Alternative: obj replaced, area and turfs are placed on top.
type ToolFill struct {
	tool

	start    util.Point
	fillArea util.Bounds

	dragging bool
	// APHELION EDIT ADDITION START - SHARED SHAPES
	shape       editing.ShapeDescriptor
	selection   editing.Selection
	restriction editing.Selection
	restrict    bool
	random      bool
	palette     editing.RandomPalette
	seed        uint64
	density     float64
	filter      dm.PathsFilter
	prefab      *dmmprefab.Prefab
	replace     bool
	border      bool
	// APHELION EDIT ADDITION END
}

func (ToolFill) Name() string {
	return TNFill
}

func newFill() *ToolFill {
	return &ToolFill{}
}

func (t *ToolFill) Stale() bool {
	return !t.dragging
}

func (t *ToolFill) process() {
	if t.active() {
		// APHELION EDIT ADDITION START - SHARED SHAPES
		if t.shape.Kind != editing.ShapeRectangle || t.shape.Outline || t.restrict {
			showShapeSelection(t.selection)
			return
		}
		// APHELION EDIT ADDITION END
		if t.AltBehaviour() {
			ed.OverlayPushArea(t.fillArea, overlay.ColorToolFillAltTileFill, overlay.ColorToolFillAltTileBorder)
		} else {
			ed.OverlayPushArea(t.fillArea, overlay.ColorToolFillTileFill, overlay.ColorToolFillTileBorder)
		}
	}
}

func (t *ToolFill) onStart(coord util.Point) {
	// APHELION EDIT ADDITION START - RANDOM FILL
	if !t.beginRandomFill() {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - SHARED SHAPES - ORIGINAL: if _, ok := ed.SelectedPrefab(); ok {
	if _, ok := ed.SelectedPrefab(); ok || t.random {
		// APHELION EDIT ADDITION START - SHARED SHAPES
		t.shape = currentShape()
		t.shape.Outline = t.actionContext.Outline
		t.filter = brushFilter()
		t.prefab, _ = ed.SelectedPrefab()
		t.replace = t.actionContext.Alternate
		t.border = t.actionContext.Modifiers.Ctrl
		t.restriction = SelectionForEditor(ed)
		t.restrict = shapeRestricted()
		// APHELION EDIT ADDITION END
		t.dragging = true
		t.start = coord
		t.onMove(coord)
	}
}

func (t *ToolFill) onMove(coord util.Point) {
	if !t.active() {
		return
	}

	t.fillArea.X1 = float32(math.Min(float64(t.start.X), float64(coord.X)))
	t.fillArea.Y1 = float32(math.Min(float64(t.start.Y), float64(coord.Y)))
	t.fillArea.X2 = float32(math.Max(float64(t.start.X), float64(coord.X)))
	t.fillArea.Y2 = float32(math.Max(float64(t.start.Y), float64(coord.Y)))
	// APHELION EDIT ADDITION START - SHARED SHAPES
	d := t.shape
	d.Width = int(t.fillArea.X2-t.fillArea.X1) + 1
	d.Height = int(t.fillArea.Y2-t.fillArea.Y1) + 1
	if d.Kind == editing.ShapeCircle {
		d.Width = min(d.Width, d.Height)
		d.Height = d.Width
	}
	selection, err := editing.ShapeSelection(d, util.Point{X: int(t.fillArea.X1), Y: int(t.fillArea.Y1), Z: t.start.Z})
	if err == nil {
		selection = editing.ClipSelection(selection, ed.Dmm().MaxX, ed.Dmm().MaxY)
		if t.restrict {
			selection = editing.CombineSelection(selection, t.restriction, editing.SelectionIntersect)
		}
		t.selection = selection
	}
	// APHELION EDIT ADDITION END
}

func (t *ToolFill) onStop(util.Point) {
	if !t.active() {
		return
	}
	// APHELION EDIT ADDITION START - RANDOM FILL
	if t.random {
		selection, anchor, palette, seed, density := t.selection, t.start, t.palette, t.seed, t.density
		filter := t.filter
		t.OnDeselect()
		if owner, ok := ed.(interface {
			StartRandomFillWithFilter(editing.Selection, editing.RandomPalette, uint64, float64, util.Point, dm.PathsFilter) error
		}); ok {
			if err := owner.StartRandomFillWithFilter(selection, palette, seed, density, anchor, filter); err != nil {
				util.ShowErrorDialog(err.Error())
			}
			return
		}
		if owner, ok := ed.(randomFillOwner); ok {
			if err := owner.StartRandomFill(selection, palette, seed, density, anchor); err != nil {
				util.ShowErrorDialog(err.Error())
			}
		}
		return
	}
	// APHELION EDIT ADDITION END

	// Fill the area.
	// APHELION EDIT ADDITION START - SHARED SHAPES
	if _, ok := ed.(interface {
		FillSelectionWithFilter(editing.Selection, *dmmprefab.Prefab, bool, dm.PathsFilter) error
	}); ok {
		if t.selection.Len() > 0 {
			if err := fillShape(t.selection, t.prefab, t.replace, t.filter); err != nil {
				util.ShowErrorDialog(err.Error())
			}
		}
		t.OnDeselect()
		return
	}
	// APHELION EDIT ADDITION END
	if prefab, ok := ed.SelectedPrefab(); ok {
		// APHELION EDIT ADDITION START - SHARED SHAPES
		if t.shape.Kind != editing.ShapeRectangle || t.shape.Outline || t.restrict {
			if t.selection.Len() > 0 {
				if err := fillShape(t.selection, t.prefab, t.replace, t.filter); err != nil {
					util.ShowErrorDialog(err.Error())
				}
			}
			t.OnDeselect()
			return
		}
		// APHELION EDIT ADDITION END
		// APHELION EDIT ADDITION START - BOUNDED FILL
		if owner, ok := ed.(interface {
			TryScheduleFill(util.Bounds, int, *dmmprefab.Prefab, bool, bool) bool
		}); ok && owner.TryScheduleFill(t.fillArea, t.start.Z, prefab, t.replace, t.border) {
			t.start = util.Point{}
			t.fillArea = util.Bounds{}
			t.dragging = false
			return
		}
		// APHELION EDIT ADDITION END
		// APHELION EDIT ADDITION START - BRUSH CAPTURE
		var targets []util.Point
		// APHELION EDIT ADDITION END
		fillTile := func(x, y int) {
			coord := util.Point{X: x, Y: y, Z: t.start.Z}
			tile := ed.Dmm().GetTile(coord)
			t.basicPrefabAdd(tile, prefab)
		}
		/* APHELION EDIT REMOVAL START - BOUNDED FILL
		if imguiext.IsCtrlDown() {
			for x := t.fillArea.X1; x <= t.fillArea.X2; x++ {
				for y := t.fillArea.Y1; y <= t.fillArea.Y2; y++ {
					if y > t.fillArea.Y1 && y < t.fillArea.Y2 && x > t.fillArea.X1 && x < t.fillArea.X2 {
						continue
					}
					// APHELION EDIT CHANGE - BRUSH CAPTURE - ORIGINAL: fillTile(int(x), int(y))
					targets = append(targets, util.Point{X: int(x), Y: int(y), Z: t.start.Z})
				}
			}
		} else {
			for x := t.fillArea.X1; x <= t.fillArea.X2; x++ {
				for y := t.fillArea.Y1; y <= t.fillArea.Y2; y++ {
					// APHELION EDIT CHANGE - BRUSH CAPTURE - ORIGINAL: fillTile(int(x), int(y))
					targets = append(targets, util.Point{X: int(x), Y: int(y), Z: t.start.Z})
				}
			}
		}

		APHELION EDIT REMOVAL END */
		// APHELION EDIT ADDITION START - BOUNDED FILL
		_ = editing.VisitRectangle(context.Background(), t.fillArea, t.start.Z, t.border, func(point util.Point) error { targets = append(targets, point); return nil })
		// APHELION EDIT ADDITION END
		// APHELION EDIT ADDITION START - BRUSH CAPTURE
		// Fill is one action: a later invalid tile must not leave a partial fill.
		if ed.TryBeginTileChange(targets...) {
			for _, coord := range targets {
				fillTile(coord.X, coord.Y)
			}
		}
		// APHELION EDIT ADDITION END
		// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: go ed.CommitChanges("Fill Atoms")
		ed.CommitOperation("Fill Atoms")
	}

	t.start = util.Point{}
	t.fillArea = util.Bounds{}

	t.dragging = false
}

func (t *ToolFill) active() bool {
	return !t.start.Equals(0, 0, 0)
}

// APHELION EDIT ADDITION START - SHARED SHAPES
func (t *ToolFill) OnDeselect() {
	t.dragging = false
	t.start = util.Point{}
	t.selection = editing.Selection{}
	t.restriction = editing.Selection{}
	t.palette = editing.RandomPalette{}
	t.random = false
	t.seed = 0
	t.density = 0
	t.prefab = nil
	t.filter = dm.PathsFilter{}
}

// APHELION EDIT ADDITION END
