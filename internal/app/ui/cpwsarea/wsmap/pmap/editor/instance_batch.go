// APHELION EDIT ADDITION START - SEARCH CAPTURE OWNERSHIP
package editor

import (
	"fmt"

	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/search"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
)

// CommitInstanceBatch captures every current target and checks that replacement
// data is capturable before changing display state. A nil replacement deletes
// targets; otherwise normal same-base-type
// replacement rules apply. Submission/acknowledgement use the existing executor
// and history path; returning does not imply a network acknowledgement.
func (e *Editor) CommitInstanceBatch(instances []*dmminstance.Instance, replacement *dmmprefab.Prefab, message string) {
	if !e.CanStartMapEdit() || len(instances) == 0 {
		return
	}
	tiles, err := search.CurrentTiles(e.dmm, instances)
	if err != nil {
		e.reportCollaborationError("Unable to apply search action", err)
		return
	}
	for _, tile := range tiles {
		e.BeginTileChange(tile.Coord)
		if e.collaborationErr != nil {
			// CanStartMapEdit required an empty journal, so every capture here
			// belongs to this batch. Keep the fault, but release unused before-states.
			clear(e.pendingChanges)
			e.reportCollaborationError("Unable to apply search action", e.collaborationErr)
			return
		}
	}
	if replacement != nil {
		// Validate the shared selected prefab using the same conversion as the
		// eventual operation. The copied instance retains a captured stable ID;
		// neither it nor its temporary tile can mutate the current display.
		if err := captureBatchReplacement(instances[0], replacement); err != nil {
			clear(e.pendingChanges)
			e.reportCollaborationError("Unable to apply search action", err)
			return
		}
	}
	for _, instance := range instances {
		if replacement == nil {
			e.InstanceDelete(instance)
		} else if dm.IsPathBaseSame(instance.Prefab().Path(), replacement.Path()) {
			// Exact current membership and every before-state were checked above.
			// Preserve InstanceReplace's base-type rule without rescanning the tile.
			instance.SetPrefab(replacement)
		}
	}
	e.CommitOperation(message)
}

func captureBatchReplacement(target *dmminstance.Instance, replacement *dmmprefab.Prefab) error {
	// The inherited same-base-type comparison indexes path[1:]. An empty
	// selected path must be rejected before entering that code.
	if replacement.Path() == "" {
		return fmt.Errorf("replacement prefab has an empty path")
	}
	probe := target.Copy()
	probe.SetPrefab(replacement)
	tile := dmmap.Tile{Coord: target.Coord()}
	tile.Set(dmmap.Instances{&probe})
	if _, err := mapadapter.CaptureTile(&tile); err != nil {
		return fmt.Errorf("capture replacement prefab: %w", err)
	}
	return nil
}

// APHELION EDIT ADDITION END
