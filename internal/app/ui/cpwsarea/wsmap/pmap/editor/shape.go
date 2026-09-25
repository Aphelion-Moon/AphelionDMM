// APHELION EDIT ADDITION START - SHARED SHAPES
package editor

import (
	"fmt"
	"maps"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/prefabidentity"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

func (e *Editor) BrushFilter() dm.PathsFilter { return e.app.PathsFilter().Copy() }

func (e *Editor) RandomFillSettings() *editing.MapperSettings { return e.app.Prefs().Mapper }

func (e *Editor) BrushShape() editing.ShapeDescriptor {
	if settings := e.app.Prefs().Mapper; settings != nil {
		return settings.Shape
	}
	return editing.ShapeDescriptor{Width: 1, Height: 1}
}

func (e *Editor) DeleteShapeSelection(selection editing.Selection) error {
	return e.DeleteShapeSelectionWithFilter(selection, e.BrushFilter())
}

func (e *Editor) DeleteShapeSelectionWithFilter(selection editing.Selection, filter dm.PathsFilter) error {
	fence, err := e.BeginShapeDelete(selection.Level())
	if err != nil {
		return err
	}
	return e.EraseShape(selection, true, filter, fence, nil)
}

func (e *Editor) BeginShapeDelete(level int) (editing.ShapeDeleteFence, error) {
	if !e.CanStartMapEdit() || len(e.unresolvedSubmissions) != 0 || level != e.ActiveLevel() {
		return editing.ShapeDeleteFence{}, fmt.Errorf("finish the current edit before erasing a shape")
	}
	return editing.ShapeDeleteFence{
		DocumentID: e.authoritative.DocumentID, Revision: e.authoritative.Revision,
		AttachmentGeneration: e.attachmentGeneration, ViewGeneration: e.mapViewGeneration, Level: level,
	}, nil
}

func (e *Editor) validateShapeDeleteFence(fence editing.ShapeDeleteFence) error {
	if !e.CanStartMapEdit() || len(e.unresolvedSubmissions) != 0 ||
		fence.DocumentID != e.authoritative.DocumentID || fence.Revision != e.authoritative.Revision ||
		fence.AttachmentGeneration != e.attachmentGeneration || fence.ViewGeneration != e.mapViewGeneration ||
		fence.Level != e.ActiveLevel() {
		return fmt.Errorf("shape eraser assessment belongs to a changed document revision")
	}
	return nil
}

func (e *Editor) PickShapeDeleteTarget(coord util.Point, selection editing.Selection, filter dm.PathsFilter, fence editing.ShapeDeleteFence) (editing.ShapeDeleteTarget, bool, error) {
	if err := e.validateShapeDeleteFence(fence); err != nil {
		return editing.ShapeDeleteTarget{}, false, err
	}
	if coord.Z != fence.Level || !e.dmm.HasTile(coord) {
		return editing.ShapeDeleteTarget{}, false, fmt.Errorf("shape eraser target is outside the active map level")
	}
	renderer := e.pMap.Canvas().Render()
	if renderer == nil {
		return editing.ShapeDeleteTarget{}, false, fmt.Errorf("map picking is not available")
	}
	x := (coord.X-1)*dmmap.WorldIconSize + dmmap.WorldIconSize/2
	y := (coord.Y-1)*dmmap.WorldIconSize + dmmap.WorldIconSize/2
	picked := renderer.PickAt(x, y, coord.Z, func(candidate *dmminstance.Instance) bool {
		return candidate != nil && selection.Contains(candidate.Coord()) && candidate.Prefab() != nil &&
			filter.IsVisiblePath(candidate.Prefab().Path()) && !e.isShapeDefault(candidate)
	})
	if picked == nil {
		return editing.ShapeDeleteTarget{}, false, nil
	}
	if picked.StableID() == "" {
		return editing.ShapeDeleteTarget{}, false, fmt.Errorf("picked shape target has no stable identity")
	}
	point := picked.Coord()
	state, exists := e.authoritativeTiles[model.Coord{X: point.X, Y: point.Y, Z: point.Z}]
	if !exists {
		return editing.ShapeDeleteTarget{}, false, fmt.Errorf("picked shape target is outside document authority")
	}
	found := false
	for _, prefab := range state.Prefabs {
		if prefab.StableID == model.StableID(picked.StableID()) {
			found = true
			break
		}
	}
	if !found {
		return editing.ShapeDeleteTarget{}, false, fmt.Errorf("map picking is stale; retry the shape erase")
	}
	return editing.ShapeDeleteTarget{Coord: point, StableID: model.StableID(picked.StableID())}, true, nil
}

func (e *Editor) ShapeDeleteBudget() *resources.Budget { return e.editWorkBudget() }

// EraseShape takes ownership of assessment admission until the edit worker has
// relinquished its stable target index, including cancellation and failure.
func (e *Editor) EraseShape(selection editing.Selection, all bool, filter dm.PathsFilter, fence editing.ShapeDeleteFence, targets editing.ShapeDeleteTargets, admissions ...*resources.Reservation) error {
	previousWork := e.localWork
	err := e.eraseShape(selection, all, filter, fence, targets)
	release := func() {
		for _, admission := range admissions {
			admission.Release()
		}
	}
	if err == nil && e.localWork != nil && e.localWork != previousWork {
		complete := e.localWork.complete
		e.localWork.complete = func(accepted engine.LocalAcceptance, backward []model.TileChange, failure error) {
			defer release()
			if complete != nil {
				complete(accepted, backward, failure)
			}
		}
	} else {
		release()
	}
	return err
}

func (e *Editor) eraseShape(selection editing.Selection, all bool, filter dm.PathsFilter, fence editing.ShapeDeleteFence, targets editing.ShapeDeleteTargets) error {
	if err := e.validateShapeDeleteFence(fence); err != nil {
		return err
	}
	if selection.Len() == 0 {
		return nil
	}
	if selection.Level() != fence.Level {
		return fmt.Errorf("shape selection is on another map level")
	}
	if all {
		if e.tryScheduleShapeEraseAll(selection, filter) {
			return nil
		}
		coords := make([]util.Point, 0, selection.Len())
		var failure error
		selection.Visit(func(point util.Point) {
			if failure != nil {
				return
			}
			tile := e.dmm.GetTile(point)
			if tile == nil {
				failure = fmt.Errorf("shape erase destination is unavailable")
				return
			}
			for _, instance := range tile.Instances() {
				if filter.IsVisiblePath(instance.Prefab().Path()) && !e.isShapeDefault(instance) {
					coords = append(coords, point)
					break
				}
			}
		})
		if failure != nil {
			return failure
		}
		if len(coords) == 0 {
			return nil
		}
		if !e.TryBeginTileChange(coords...) {
			return fmt.Errorf("unable to capture shape footprint")
		}
		for _, point := range coords {
			tile := e.dmm.GetTile(point)
			kept := make(dmmap.Instances, 0, len(tile.Instances()))
			for _, instance := range tile.Instances() {
				if !filter.IsVisiblePath(instance.Prefab().Path()) || e.isShapeDefault(instance) {
					kept = append(kept, instance)
				}
			}
			tile.Set(kept)
			tile.InstancesRegenerate()
		}
		if e.pMap.Canvas().Render() != nil {
			e.UpdateCanvasByCoords(coords)
		}
		e.CommitOperation("Erase Shape")
		return nil
	}
	if len(targets) == 0 {
		return nil
	}
	if e.tryScheduleShapeEraseTargets(selection, targets, filter) {
		return nil
	}
	byCoord := make(map[util.Point]map[string]struct{})
	coords := make([]util.Point, 0, len(targets))
	for id, point := range targets {
		if id == "" || point.Z != fence.Level {
			return fmt.Errorf("shape erase target identity is invalid")
		}
		tile := e.dmm.GetTile(point)
		if tile == nil {
			return fmt.Errorf("shape erase target is no longer available")
		}
		found := false
		for _, instance := range tile.Instances() {
			if instance.StableID() != id {
				continue
			}
			if !filter.IsVisiblePath(instance.Prefab().Path()) || e.isShapeDefault(instance) {
				return fmt.Errorf("shape erase target is no longer eligible")
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("shape erase target is no longer available")
		}
		if _, exists := byCoord[point]; !exists {
			byCoord[point] = make(map[string]struct{})
			coords = append(coords, point)
		}
		byCoord[point][id] = struct{}{}
	}
	if len(coords) == 0 {
		return nil
	}
	if !e.TryBeginTileChange(coords...) {
		return fmt.Errorf("unable to capture shape targets")
	}
	for _, point := range coords {
		tile := e.dmm.GetTile(point)
		kept := make(dmmap.Instances, 0, len(tile.Instances()))
		for _, instance := range tile.Instances() {
			if _, remove := byCoord[point][instance.StableID()]; !remove {
				kept = append(kept, instance)
			}
		}
		tile.Set(kept)
		tile.InstancesRegenerate()
	}
	if e.pMap.Canvas().Render() != nil {
		e.UpdateCanvasByCoords(coords)
	}
	e.CommitOperation("Erase Shape")
	return nil
}

func (e *Editor) isShapeDefault(instance *dmminstance.Instance) bool {
	if instance == nil || instance.Prefab() == nil {
		return false
	}
	prefab := instance.Prefab()
	for _, base := range []*dmmprefab.Prefab{dmmap.BaseArea, dmmap.BaseTurf} {
		if base != nil && prefabidentity.Equal(prefab.Path(), prefab.Vars(), base.Path(), base.Vars()) {
			return true
		}
	}
	return false
}

func captureShapeDefaults() ([]model.PrefabState, error) {
	defaults := make([]model.PrefabState, 0, 2)
	for _, base := range []*dmmprefab.Prefab{dmmap.BaseArea, dmmap.BaseTurf} {
		value, err := captureBulkPrefab(base)
		if err != nil || value == nil {
			return nil, fmt.Errorf("map defaults are unavailable")
		}
		defaults = append(defaults, *value)
	}
	return defaults, nil
}

func isShapeDefaultState(prefab model.PrefabState, defaults []model.PrefabState) bool {
	for _, base := range defaults {
		if prefab.Path == base.Path && maps.Equal(prefab.Vars, base.Vars) {
			return true
		}
	}
	return false
}

func (e *Editor) tryScheduleShapeEraseAll(selection editing.Selection, filter dm.PathsFilter) bool {
	local, ok := e.executor.(localEditExecutor)
	if !ok || e.sessionOwned || selection.Len() <= directLocalTiles {
		return false
	}
	if !e.CanStartMapEdit() {
		return true
	}
	defaults, err := captureShapeDefaults()
	if err != nil {
		e.reportCollaborationError("Unable to prepare shape erase", err)
		return true
	}
	visit := func(visitor func(model.Coord)) {
		selection.Visit(func(point util.Point) { visitor(model.Coord{X: point.X, Y: point.Y, Z: point.Z}) })
	}
	match := func(_ model.Coord, prefab model.PrefabState) bool {
		return filter.IsVisiblePath(prefab.Path) && !isShapeDefaultState(prefab, defaults)
	}
	e.scheduleBulkEdit(local, "Erase Shape", visit, match, nil, false, false)
	return true
}

func (e *Editor) tryScheduleShapeEraseTargets(selection editing.Selection, targets editing.ShapeDeleteTargets, filter dm.PathsFilter) bool {
	local, ok := e.executor.(localEditExecutor)
	if !ok || e.sessionOwned || len(targets) <= directLocalTiles {
		return false
	}
	if !e.CanStartMapEdit() {
		return true
	}
	defaults, err := captureShapeDefaults()
	if err != nil {
		e.reportCollaborationError("Unable to prepare shape erase", err)
		return true
	}
	visit := func(visitor func(model.Coord)) {
		selection.Visit(func(point util.Point) { visitor(model.Coord{X: point.X, Y: point.Y, Z: point.Z}) })
	}
	match := func(coord model.Coord, prefab model.PrefabState) bool {
		point, exists := targets[string(prefab.StableID)]
		return exists && point.X == coord.X && point.Y == coord.Y && point.Z == coord.Z &&
			filter.IsVisiblePath(prefab.Path) && !isShapeDefaultState(prefab, defaults)
	}
	e.scheduleBulkEdit(local, "Erase Shape", visit, match, nil, false, false)
	return true
}

// APHELION EDIT ADDITION END
