// APHELION EDIT ADDITION START - DETERMINISTIC ERASER
package editor

import (
	"fmt"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/prefabidentity"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

func (e *Editor) StartEraseStroke(all bool) (*editing.EraseStroke, error) {
	if !e.CanStartMapEdit() {
		return nil, fmt.Errorf("finish the current edit before erasing")
	}
	filter := e.app.PathsFilter().Copy()
	generation := e.attachmentGeneration
	level := e.pMap.ActiveLevel()
	removed := make(map[string]bool)
	eligible := func(i *dmminstance.Instance) bool {
		if i == nil || i.Prefab() == nil || !filter.IsVisiblePath(i.Prefab().Path()) {
			return false
		}
		// Required default area/turf values regenerate unchanged after deletion;
		// leave their identities in place instead of creating a no-op history item.
		prefab := i.Prefab()
		for _, base := range []*dmmprefab.Prefab{dmmap.BaseArea, dmmap.BaseTurf} {
			if base != nil && prefabidentity.Equal(prefab.Path(), prefab.Vars(), base.Path(), base.Vars()) {
				return false
			}
		}
		return true
	}
	live := func(candidate *dmminstance.Instance) bool {
		if !eligible(candidate) || removed[candidate.StableID()] || !e.dmm.HasTile(candidate.Coord()) {
			return false
		}
		for _, current := range e.dmm.GetTile(candidate.Coord()).Instances() {
			if current == candidate {
				return true
			}
		}
		return false
	}
	return editing.NewEraseStroke(dmmap.WorldIconSize, all, func(x, y, z int, all bool, processed func(util.Point) bool) editing.EraseResult {
		fail := func(err error) editing.EraseResult {
			return editing.EraseResult{Outcome: editing.MutationFailed, Err: err}
		}
		if generation != e.attachmentGeneration || e.mapViewClosed || z != level || level != e.pMap.ActiveLevel() {
			return fail(fmt.Errorf("erase stroke belongs to an inactive map or level"))
		}
		if e.localWork != nil || e.paste != nil || e.collaborationErr != nil {
			return fail(fmt.Errorf("erase stroke is blocked by unfinished work"))
		}
		coord := util.Point{X: editing.StrokeTile(x, dmmap.WorldIconSize), Y: editing.StrokeTile(y, dmmap.WorldIconSize), Z: z}
		var target *dmminstance.Instance
		if !all {
			r := e.pMap.Canvas().Render()
			if r == nil {
				return fail(fmt.Errorf("map picking is not available"))
			}
			target = r.PickAt(x, y, z, live)
			if target == nil {
				return editing.EraseResult{Outcome: editing.MutationEmpty}
			}
			coord = target.Coord()
		}
		if !e.dmm.HasTile(coord) || processed(coord) {
			return editing.EraseResult{Outcome: editing.MutationEmpty}
		}
		tile := e.dmm.GetTile(coord)
		changed := false
		for _, i := range tile.Instances() {
			if eligible(i) && (all || i == target) {
				changed = true
				break
			}
		}
		if !changed {
			return editing.EraseResult{Outcome: editing.MutationEmpty}
		}
		if !e.TryBeginTileChange(coord) {
			err := e.collaborationErr
			if err == nil {
				err = fmt.Errorf("unable to capture eraser target")
			}
			return fail(err)
		}
		retained := make(dmmap.Instances, 0, len(tile.Instances()))
		for _, i := range tile.Instances() {
			if !eligible(i) || !all && i != target {
				retained = append(retained, i)
			} else {
				removed[i.StableID()] = true
			}
		}
		tile.Set(retained)
		tile.InstancesRegenerate()
		e.UpdateCanvasByCoords([]util.Point{coord})
		return editing.EraseResult{Outcome: editing.MutationChanged, Coord: coord}
	}), nil
}

// Failed capture keeps earlier intent available through the existing recovery
// owner. It must not also submit a partial stroke and show a second error.
func (e *Editor) FinishEraseStroke(stroke *editing.EraseStroke) {
	if stroke.Err() != nil {
		if e.collaborationErr == nil {
			e.collaborationErr = stroke.Err()
		}
		e.reportCollaborationError("Erase stopped; use recovery to retry or discard the unfinished edit", stroke.Err())
		return
	}
	if stroke.Changes() != 0 {
		e.CommitOperation("Erase Stroke")
	}
}

// APHELION EDIT ADDITION END
