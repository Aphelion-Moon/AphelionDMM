// APHELION EDIT ADDITION START - COMPOSITION ANCHORS
package editor

import (
	"fmt"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// CompositionRoot checks the exact accepted source instance. Derived pixels are
// never edit targets; the caller must enter the containing source document.
func (e *Editor) CompositionRoot(root mapping.Root, to util.Point) (*dmminstance.Instance, error) {
	generation, revision := e.SaveVersion()
	if !e.CanStartMapEdit() || e.ChangedSinceSave(generation, revision) || root.Source.DocumentID != string(e.authoritative.DocumentID) || root.Source.Generation != generation || root.Source.Revision != uint64(revision) {
		return nil, fmt.Errorf("source changed or has a pending edit; refresh the composition")
	}
	if !e.dmm.HasTile(root.Local) || !e.dmm.HasTile(to) {
		return nil, fmt.Errorf("anchor is outside the source map")
	}
	if e.workingSelection.Restrict && (!e.workingSelection.Get(root.Local.Z).Contains(root.Local) || !e.workingSelection.Get(to.Z).Contains(to)) {
		return nil, fmt.Errorf("anchor move is outside the working selection")
	}
	for _, i := range e.dmm.GetTile(root.Local).Instances() {
		if root.StableID == "" || i.StableID() != root.StableID {
			continue
		}
		if !e.IsCompositionRoot(i) || !e.app.PathsFilter().IsVisiblePath(i.Prefab().Path()) {
			return nil, fmt.Errorf("root is hidden or no longer a modular anchor")
		}
		return i, nil
	}
	return nil, fmt.Errorf("root was moved, removed or replaced; select it again")
}

// MoveCompositionRoot uses the same tile capture, identity-preserving instance
// transfer, executor and history as ordinary Move. Draft motion never calls it.
func (e *Editor) MoveCompositionRoot(root mapping.Root, to util.Point) error {
	i, err := e.CompositionRoot(root, to)
	if err != nil {
		return err
	}
	if to == root.Local {
		return nil
	}
	e.movingCompositionRoot = true
	defer func() { e.movingCompositionRoot = false }()
	if !e.TryBeginTileChange(root.Local, to) {
		return fmt.Errorf("could not capture source and destination")
	}
	e.InstanceDelete(i)
	i.SetCoord(to)
	destination := e.dmm.GetTile(to)
	destination.Set(append(destination.Instances(), i))
	destination.InstancesRegenerate()
	e.CommitOperation("Move modular anchor")
	return nil
}

func (e *Editor) compositionTileLocked(point util.Point) bool {
	if e.movingCompositionRoot {
		return false
	}
	host, ok := e.app.(interface{ CompositionTileLocked(string, util.Point) bool })
	return ok && host.CompositionTileLocked(e.dmm.Path.Absolute, point)
}
func (e *Editor) compositionEditFence() func(util.Point) bool {
	if host, ok := e.app.(interface {
		CompositionEditFence(string) func(util.Point) bool
	}); ok {
		return host.CompositionEditFence(e.dmm.Path.Absolute)
	}
	return nil
}
func validateCompositionChanges(locked func(util.Point) bool, changes []model.TileChange) error {
	if locked == nil {
		return nil
	}
	for _, change := range changes {
		p := util.Point{X: change.Coord.X, Y: change.Coord.Y, Z: change.Coord.Z}
		if locked(p) {
			return fmt.Errorf("derived contribution at %v is locked; open its source in context, or hide composition to edit the base", p)
		}
	}
	return nil
}

// TryBeginInstanceChange admits explicit properties of an actual source root.
func (e *Editor) TryBeginInstanceChange(i *dmminstance.Instance) bool {
	if i == nil {
		return false
	}
	previous := e.movingCompositionRoot
	e.SetCompositionRootGesture(i, true)
	defer func() { e.movingCompositionRoot = previous }()
	return e.TryBeginTileChange(i.Coord())
}

// SetCompositionRootGesture grants only a real source anchor's ordinary Move
// gesture access across locked derived cells. It never targets a projection atom.
func (e *Editor) SetCompositionRootGesture(i *dmminstance.Instance, active bool) {
	e.movingCompositionRoot = active && e.IsCompositionRoot(i)
}
func (e *Editor) IsCompositionRoot(i *dmminstance.Instance) bool {
	if i == nil || i.Prefab() == nil || !e.dmm.HasTile(i.Coord()) {
		return false
	}
	owned := false
	for _, candidate := range e.dmm.GetTile(i.Coord()).Instances() {
		if candidate == i {
			owned = true
			break
		}
	}
	if !owned {
		return false
	}
	environment := e.app.LoadedEnvironment()
	if environment != nil {
		for object, depth := environment.Objects[i.Prefab().Path()], 0; object != nil && depth < 512; object, depth = object.Parent(), depth+1 {
			if object.Path == "/obj/modular_map_root" {
				return true
			}
		}
	}
	return dm.IsPath(i.Prefab().Path(), "/obj/modular_map_root")
}

// APHELION EDIT ADDITION END
