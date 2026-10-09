// APHELION EDIT ADDITION START - SHARED SHAPES
package tools

import (
	"fmt"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/overlay"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

func brushFilter() dm.PathsFilter {
	if owner, ok := ed.(interface{ BrushFilter() dm.PathsFilter }); ok {
		return owner.BrushFilter()
	}
	return *dm.NewPathsFilterEmpty()
}
func fillShape(selection editing.Selection, prefab *dmmprefab.Prefab, replace bool, filter dm.PathsFilter) error {
	if owner, ok := ed.(interface {
		FillSelectionWithFilter(editing.Selection, *dmmprefab.Prefab, bool, dm.PathsFilter) error
	}); ok {
		return owner.FillSelectionWithFilter(selection, prefab, replace, filter)
	}
	if owner, ok := ed.(interface {
		FillSelection(editing.Selection, *dmmprefab.Prefab, bool) error
	}); ok {
		return owner.FillSelection(selection, prefab, replace)
	}
	return fmt.Errorf("selection fill is unavailable")
}

func PreparingShape() bool {
	switch t := Selected().(type) {
	case *ToolAdd:
		return t.shapeStroke != nil && t.shapeReleased
	case *ToolDelete:
		return t.shapeStroke != nil && t.shapeReleased
	}
	return false
}

func currentShape() editing.ShapeDescriptor {
	d := editing.ShapeDescriptor{Width: 1, Height: 1}
	if owner, ok := ed.(interface {
		BrushShape() editing.ShapeDescriptor
	}); ok {
		d = owner.BrushShape()
	}
	d.Width = max(1, d.Width)
	d.Height = max(1, d.Height)
	return d
}
func shapeRestricted() bool {
	if owner, ok := ed.(workingSelectionOwner); ok {
		return owner.WorkingSelection().Restrict
	}
	return false
}

func beginShapeStroke(coord util.Point) (*editing.ShapeStroke, error) {
	shape, err := editing.ShapeSelection(currentShape(), util.Point{X: 1, Y: 1, Z: coord.Z})
	if err != nil {
		return nil, err
	}
	stroke := editing.NewShapeStroke(shape, ed.Dmm().MaxX, ed.Dmm().MaxY, SelectionForEditor(ed), shapeRestricted())
	stroke.Queue(coord)
	return stroke, nil
}
func showShapeSelection(selection editing.Selection) {
	selection.VisitRuns(func(r util.Bounds) {
		ed.OverlayPushArea(r, overlay.ColorToolSelectTileFill, overlay.ColorToolSelectTileBorder)
	})
}
func shapeBrushEnabled() bool {
	d := currentShape()
	return d.Kind != editing.ShapeRectangle || d.Width > 1 || d.Height > 1 || d.Outline || shapeRestricted()
}

// APHELION EDIT ADDITION END

// APHELION EDIT ADDITION START - PLACEMENT LINT
// placementLinter is implemented by editors that can check placements against
// the repository lint rules and show the transient result.
type placementLinter interface {
	EvaluatePlacement(util.Point, *dmmprefab.Prefab, bool) maplint.Verdict
	RecordPlacementLint(maplint.PlacementReport)
}

// applyLintHover adds a would-be violation hint to a hover context.
func applyLintHover(context *ActionContext, position util.Point, inBounds bool, prefab *dmmprefab.Prefab, replaceObjects bool) {
	linter, ok := ed.(placementLinter)
	if !ok || !inBounds || prefab == nil {
		return
	}
	verdict := linter.EvaluatePlacement(position, prefab, replaceObjects)
	if len(verdict.Violations) == 0 {
		return
	}
	if verdict.Skip {
		context.LintWarning = verdict.Summary() + " This tile already holds an identical object and will be skipped."
	} else {
		context.LintWarning = verdict.Summary() + " It will be placed with a warning; Replace existing is offered afterwards."
	}
}

// APHELION EDIT ADDITION END
