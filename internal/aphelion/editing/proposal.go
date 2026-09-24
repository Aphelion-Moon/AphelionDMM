package editing

import (
	"context"
	"fmt"
	"sort"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

// PlacementDefaults captures environment-owned immutable prefabs on the UI
// thread before work starts, so a worker never reads changing map globals.
type PlacementDefaults struct {
	Area *dmmprefab.Prefab
	Turf *dmmprefab.Prefab
}

// BuildPlacementProposal builds the immutable operation used by paste preview
// and commit. snapshot must be an owned executor snapshot, and source must stay
// immutable for the duration of the call. Clipboard snapshots satisfy that
// contract: they retain immutable prefab values while later copies replace the
// clipboard slice instead of editing this one.
//
// The function never reads or mutates the displayed DMM. Call it on a worker,
// then use operation.Changes for bounded UI-thread preview application.
func BuildPlacementProposal(
	ctx context.Context,
	snapshot model.Snapshot,
	actor model.ActorID,
	source []dmmap.Tile,
	visible func(string) bool,
	target util.Point,
	progress func(completed, total int),
) (model.Operation, error) {
	operation, reservation, err := BuildPlacementProposalAdmitted(ctx, snapshot, actor, source, visible, target, progress, resources.DefaultBudget())
	if reservation != nil {
		reservation.Release()
	}
	return operation, err
}

// BuildPlacementProposalAdmitted builds the exact operation under a byte
// reservation that the caller retains for as long as the proposal and its
// indexes remain live. The reservation is acquired before any map-sized index
// or operation buffers are created.
func BuildPlacementProposalAdmitted(
	ctx context.Context,
	snapshot model.Snapshot,
	actor model.ActorID,
	source []dmmap.Tile,
	visible func(string) bool,
	target util.Point,
	progress func(completed, total int),
	budget *resources.Budget,
) (model.Operation, *resources.Reservation, error) {
	reservation, err := ReservePlacementMemory(snapshot, source, 0, budget)
	if err != nil {
		return model.Operation{}, nil, err
	}
	operation, err := BuildPlacementProposalReserved(ctx, snapshot, actor, source, visible, target, progress, reservation)
	if err != nil {
		reservation.Release()
		return model.Operation{}, nil, err
	}
	return operation, reservation, nil
}

// BuildPlacementProposalReserved consumes an existing reservation after the
// caller has accounted for source transforms and any surrounding snapshot
// indexes. The reservation remains owned by the caller.
func BuildPlacementProposalReserved(
	ctx context.Context,
	snapshot model.Snapshot,
	actor model.ActorID,
	source []dmmap.Tile,
	visible func(string) bool,
	target util.Point,
	progress func(completed, total int),
	reservation *resources.Reservation,
) (model.Operation, error) {
	return BuildPlacementProposalReservedWithIdentities(ctx, snapshot, actor, source, visible, target, progress, reservation, nil, PlacementDefaults{Area: dmmap.BaseArea, Turf: dmmap.BaseTurf})
}

// BuildPlacementProposalReservedWithIdentities reuses copied instance IDs for
// one placement's successive previews. identities belongs to its sole worker;
// source instance IDs must remain valid and unique across template transforms.
// A nil map preserves the standalone builder's fresh-copy behavior.
func BuildPlacementProposalReservedWithIdentities(
	ctx context.Context,
	snapshot model.Snapshot,
	actor model.ActorID,
	source []dmmap.Tile,
	visible func(string) bool,
	target util.Point,
	progress func(completed, total int),
	reservation *resources.Reservation,
	identities map[model.StableID]model.StableID,
	defaults PlacementDefaults,
) (model.Operation, error) {
	if reservation == nil || reservation.Bytes() < EstimatePlacementMemory(snapshot, source, 0) {
		return model.Operation{}, fmt.Errorf("placement memory reservation is smaller than the estimated requirement")
	}
	return buildPlacementProposal(ctx, snapshot, actor, source, visible, target, progress, identities, defaults)
}

// ReservePlacementMemory estimates the temporary snapshot indexes, transformed
// source copies, preview patches, and before/after operation state required for
// a placement. transformCount scales the retained template and transform work.
func ReservePlacementMemory(snapshot model.Snapshot, source []dmmap.Tile, transformCount int, budget *resources.Budget) (*resources.Reservation, error) {
	return budgetOrDefault(budget).Reserve(EstimatePlacementMemory(snapshot, source, transformCount))
}

// EstimatePlacementMemory returns a conservative byte estimate for additional
// placement work. The estimate uses actual path/variable payload lengths and
// snapshot cardinalities; it is not a turf-count product limit.
func EstimatePlacementMemory(snapshot model.Snapshot, source []dmmap.Tile, transformCount int) uint64 {
	snapshotPayload, snapshotPrefabs := estimateSnapshotPayload(snapshot)
	templatePayload := estimateTemplatePayload(source)
	perCell := uint64(256)
	if len(snapshot.Tiles) != 0 {
		perCell = max(perCell, snapshotPayload/uint64(len(snapshot.Tiles)))
	}
	changedCells := uint64(len(source))
	// The input snapshot is already resident. Admission covers one fresh
	// executor snapshot, the coordinate/state indexes, the touched before/after
	// copies, and the preview patch set.
	needed := saturatingAdd(snapshotPayload, saturatingMul(uint64(len(snapshot.Tiles)), 320))
	needed = saturatingAdd(needed, saturatingMul(snapshotPrefabs, 96))
	needed = saturatingAdd(needed, saturatingMul(saturatingMul(perCell, changedCells), 2))
	needed = saturatingAdd(needed, saturatingMul(templatePayload, uint64(max(2, transformCount+1))))
	needed = saturatingAdd(needed, saturatingMul(changedCells, 256))
	return saturatingAdd(needed, 1<<20)
}

// EstimateSnapshotIndexMemory covers an executor snapshot plus the indexes and
// touched-coordinate rollback patches built from it.
func EstimateSnapshotIndexMemory(snapshot model.Snapshot, touched int) uint64 {
	payload, prefabs := estimateSnapshotPayload(snapshot)
	needed := saturatingAdd(payload, saturatingMul(uint64(len(snapshot.Tiles)), 320))
	needed = saturatingAdd(needed, saturatingMul(prefabs, 96))
	needed = saturatingAdd(needed, saturatingMul(uint64(touched), 128))
	return saturatingAdd(needed, 1<<20)
}

func buildPlacementProposal(
	ctx context.Context,
	snapshot model.Snapshot,
	actor model.ActorID,
	source []dmmap.Tile,
	visible func(string) bool,
	target util.Point,
	progress func(completed, total int),
	identities map[model.StableID]model.StableID,
	defaults PlacementDefaults,
) (model.Operation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return model.Operation{}, err
	}
	if visible == nil || len(source) == 0 {
		return model.Operation{}, fmt.Errorf("paste requires a nonempty selection")
	}
	if err := actor.Validate(); err != nil {
		return model.Operation{}, fmt.Errorf("paste actor: %w", err)
	}
	baseHash, err := snapshot.Hash()
	if err != nil {
		return model.Operation{}, fmt.Errorf("validate paste base: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return model.Operation{}, err
	}
	if target.Z < 1 || target.Z > snapshot.MaxZ || target.X < 1 || target.Y < 1 {
		return model.Operation{}, fmt.Errorf("paste target is outside the map")
	}

	minX, minY, maxX, maxY := source[0].Coord.X, source[0].Coord.Y, source[0].Coord.X, source[0].Coord.Y
	level := source[0].Coord.Z
	seen := make(map[util.Point]struct{}, len(source))
	for _, tile := range source {
		coord := tile.Coord
		if coord.X < 1 || coord.Y < 1 || coord.Z != level {
			return model.Operation{}, fmt.Errorf("clipboard must contain positive coordinates on one level")
		}
		if _, exists := seen[coord]; exists {
			return model.Operation{}, fmt.Errorf("clipboard contains duplicate tiles")
		}
		seen[coord] = struct{}{}
		minX, minY = min(minX, coord.X), min(minY, coord.Y)
		maxX, maxY = max(maxX, coord.X), max(maxY, coord.Y)
	}
	width, height := maxX-minX+1, maxY-minY+1
	if width > snapshot.MaxX || height > snapshot.MaxY || target.X+width-1 > snapshot.MaxX || target.Y+height-1 > snapshot.MaxY {
		return model.Operation{}, fmt.Errorf("paste selection would leave the map or selected level")
	}

	base := make(map[model.Coord]model.TileState, len(snapshot.Tiles))
	usedIDs := make(map[model.StableID]struct{})
	for index, tile := range snapshot.Tiles {
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return model.Operation{}, err
			}
		}
		base[tile.Coord] = tile.State
		for _, prefab := range tile.State.Prefabs {
			usedIDs[prefab.StableID] = struct{}{}
		}
	}

	type sourceTile struct {
		tile dmmap.Tile
		dest model.Coord
	}
	ordered := make([]sourceTile, 0, len(source))
	for _, tile := range source {
		destination := model.Coord{
			X: target.X + tile.Coord.X - minX,
			Y: target.Y + tile.Coord.Y - minY,
			Z: target.Z,
		}
		ordered = append(ordered, sourceTile{tile: tile, dest: destination})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].dest.Y != ordered[j].dest.Y {
			return ordered[i].dest.Y < ordered[j].dest.Y
		}
		return ordered[i].dest.X < ordered[j].dest.X
	})

	changes := make([]model.TileChange, 0, len(ordered))
	for index, entry := range ordered {
		if err := ctx.Err(); err != nil {
			return model.Operation{}, err
		}
		before, exists := base[entry.dest]
		if !exists {
			return model.Operation{}, fmt.Errorf("paste destination (%d,%d,%d) is missing from the base snapshot", entry.dest.X, entry.dest.Y, entry.dest.Z)
		}
		before = model.CloneTileState(before)
		after := model.TileState{Prefabs: make([]model.PrefabState, 0, len(before.Prefabs)+len(entry.tile.Instances()))}
		for _, prefab := range before.Prefabs {
			if !visible(prefab.Path) {
				after.Prefabs = append(after.Prefabs, clonePrefabState(prefab))
			}
		}
		for _, instance := range entry.tile.Instances() {
			if err := ctx.Err(); err != nil {
				return model.Operation{}, err
			}
			if instance == nil || instance.Prefab() == nil || instance.Prefab().Vars() == nil {
				return model.Operation{}, fmt.Errorf("clipboard contains an invalid instance")
			}
			prefab := instance.Prefab()
			if !visible(prefab.Path()) {
				continue
			}
			state := model.PrefabState{Path: prefab.Path(), Vars: make(map[string]string, prefab.Vars().Len())}
			for _, name := range prefab.Vars().Iterate() {
				value, ok := prefab.Vars().Value(name)
				if !ok {
					return model.Operation{}, fmt.Errorf("clipboard variable %q on %q has no value", name, prefab.Path())
				}
				state.Vars[name] = value
			}
			key := model.StableID(instance.StableID())
			if identities != nil {
				if err := key.Validate(); err != nil {
					return model.Operation{}, fmt.Errorf("paste source identity: %w", err)
				}
			}
			state.StableID, err = placementIdentity(key, usedIDs, identities)
			if err != nil {
				return model.Operation{}, err
			}
			after.Prefabs = append(after.Prefabs, state)
		}
		if !hasPrefabPath(after, "/area") {
			key := model.StableID(fmt.Sprintf("area:%d:%d:%d", entry.dest.X, entry.dest.Y, entry.dest.Z))
			state, stateErr := prefabState(defaults.Area, usedIDs, identities, key)
			if stateErr != nil {
				return model.Operation{}, stateErr
			}
			after.Prefabs = append(after.Prefabs, state)
		}
		if !hasPrefabPath(after, "/turf") {
			key := model.StableID(fmt.Sprintf("turf:%d:%d:%d", entry.dest.X, entry.dest.Y, entry.dest.Z))
			state, stateErr := prefabState(defaults.Turf, usedIDs, identities, key)
			if stateErr != nil {
				return model.Operation{}, stateErr
			}
			after.Prefabs = append(after.Prefabs, state)
		}
		if !before.Equal(after) {
			changes = append(changes, model.TileChange{Coord: entry.dest, Before: before, After: after})
		}
		if progress != nil {
			progress(index+1, len(ordered))
		}
	}
	if err := ctx.Err(); err != nil {
		return model.Operation{}, err
	}
	operationID, err := model.NewOperationID()
	if err != nil {
		return model.Operation{}, fmt.Errorf("create paste operation id: %w", err)
	}
	return model.Operation{
		ProtocolVersion: model.ProtocolVersion,
		DocumentID:      snapshot.DocumentID,
		ActorID:         actor,
		OperationID:     operationID,
		BaseRevision:    snapshot.Revision,
		EnvironmentHash: snapshot.EnvironmentHash,
		BaseMapHash:     baseHash,
		Kind:            model.OperationKindTileChange,
		Changes:         changes,
	}, nil
}

func estimateSnapshotPayload(snapshot model.Snapshot) (uint64, uint64) {
	bytes, prefabs := uint64(1<<20), uint64(0)
	for _, tile := range snapshot.Tiles {
		bytes = saturatingAdd(bytes, 96)
		for _, prefab := range tile.State.Prefabs {
			prefabs++
			bytes = saturatingAdd(bytes, 96+uint64(len(prefab.Path))+uint64(len(prefab.StableID)))
			for name, value := range prefab.Vars {
				bytes = saturatingAdd(bytes, 48+uint64(len(name))+uint64(len(value)))
			}
		}
	}
	return bytes, prefabs
}

func estimateTemplatePayload(source []dmmap.Tile) uint64 {
	bytes := uint64(0)
	for _, tile := range source {
		bytes = saturatingAdd(bytes, 64)
		for _, instance := range tile.Instances() {
			if instance == nil || instance.Prefab() == nil || instance.Prefab().Vars() == nil {
				continue
			}
			prefab := instance.Prefab()
			bytes = saturatingAdd(bytes, 96+uint64(len(prefab.Path())))
			for _, name := range prefab.Vars().Iterate() {
				value, _ := prefab.Vars().Value(name)
				bytes = saturatingAdd(bytes, 48+uint64(len(name))+uint64(len(value)))
			}
		}
	}
	return bytes
}

func budgetOrDefault(budget *resources.Budget) *resources.Budget {
	if budget == nil {
		return resources.DefaultBudget()
	}
	return budget
}

func saturatingAdd(left, right uint64) uint64 {
	if right > ^uint64(0)-left {
		return ^uint64(0)
	}
	return left + right
}

func saturatingMul(left, right uint64) uint64 {
	if left != 0 && right > ^uint64(0)/left {
		return ^uint64(0)
	}
	return left * right
}

func hasPrefabPath(state model.TileState, base string) bool {
	for _, prefab := range state.Prefabs {
		if dm.IsPath(prefab.Path, base) {
			return true
		}
	}
	return false
}

func prefabState(prefab *dmmprefab.Prefab, usedIDs map[model.StableID]struct{}, identities map[model.StableID]model.StableID, key model.StableID) (model.PrefabState, error) {
	if prefab == nil || prefab.Vars() == nil {
		return model.PrefabState{}, fmt.Errorf("map base prefab is invalid")
	}
	state := model.PrefabState{Path: prefab.Path(), Vars: make(map[string]string, prefab.Vars().Len())}
	for _, name := range prefab.Vars().Iterate() {
		value, ok := prefab.Vars().Value(name)
		if !ok {
			return model.PrefabState{}, fmt.Errorf("map base variable %q on %q has no value", name, prefab.Path())
		}
		state.Vars[name] = value
	}
	var err error
	state.StableID, err = placementIdentity(key, usedIDs, identities)
	return state, err
}

func placementIdentity(key model.StableID, usedIDs map[model.StableID]struct{}, identities map[model.StableID]model.StableID) (model.StableID, error) {
	if id, exists := identities[key]; exists {
		if err := id.Validate(); err != nil {
			return "", fmt.Errorf("retained paste identity: %w", err)
		}
		if _, collision := usedIDs[id]; collision {
			return "", fmt.Errorf("copied instance identity conflicts with the current map or another source instance")
		}
		usedIDs[id] = struct{}{}
		return id, nil
	}
	for {
		id, err := model.NewStableID()
		if err != nil {
			return "", fmt.Errorf("create pasted instance identity: %w", err)
		}
		if _, exists := usedIDs[id]; exists {
			continue
		}
		usedIDs[id] = struct{}{}
		if identities != nil {
			identities[key] = id
		}
		return id, nil
	}
}

func clonePrefabState(prefab model.PrefabState) model.PrefabState {
	clone := model.PrefabState{StableID: prefab.StableID, Path: prefab.Path}
	if prefab.Vars != nil {
		clone.Vars = make(map[string]string, len(prefab.Vars))
		for name, value := range prefab.Vars {
			clone.Vars[name] = value
		}
	}
	return clone
}
