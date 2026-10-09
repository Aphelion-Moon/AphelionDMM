// APHELION EDIT ADDITION START - BRUSH TOOL
package tools

import (
	"strconv"

	"sdmm/internal/aphelion/disposals"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/overlay"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// TNBrush lays a run of utilities along a drag: the configured bundle (by
// default air supply and scrubber pipes and a power cable) and, optionally, a
// disposal pipe whose straight, bent, junction and trunk pieces are chosen per
// tile. The route is 4-connected so every piece meets its neighbours, and
// dragging back retracts it. Release applies the run as one edit.
const TNBrush = "Brush"

type ToolBrush struct {
	tool
	route []util.Point
}

func newBrush() *ToolBrush { return &ToolBrush{} }

func (ToolBrush) Name() string { return TNBrush }

// UtilityPrefabs resolves the enabled bundle lines the environment defines.
func UtilityPrefabs(settings *editing.MapperSettings) []*dmmprefab.Prefab {
	if settings == nil {
		return nil
	}
	var out []*dmmprefab.Prefab
	for _, line := range settings.Bundle() {
		if !line.Enabled {
			continue
		}
		if prefab, ok := dmmap.PrefabStorage.InitialV(line.Path); ok {
			out = append(out, prefab)
		}
	}
	return out
}

func (t *ToolBrush) settings() *editing.MapperSettings {
	if ed == nil {
		return nil
	}
	return ed.Prefs().Mapper
}

func (t *ToolBrush) process() {
	for _, coord := range t.route {
		ed.OverlayPushTile(coord, overlay.ColorToolAddTileFill, overlay.ColorToolAddTileBorder)
	}
}

func (t *ToolBrush) onStart(coord util.Point) { t.route = []util.Point{coord} }

func (t *ToolBrush) onMove(coord util.Point) {
	if len(t.route) != 0 && coord.Z == t.route[0].Z {
		t.route = disposals.Extend(t.route, coord)
	}
}

func (t *ToolBrush) onStop(util.Point) {
	route := t.route
	t.route = nil
	settings := t.settings()
	if len(route) == 0 || settings == nil {
		return
	}
	if err := LayBrush(ed.Dmm(), route, UtilityPrefabs(settings), settings.BrushDisposals); err != nil {
		util.ShowErrorDialog("Brush run not laid: " + err.Error())
	}
}

// DisposalAtoms reads a tile for the disposal planner.
func DisposalAtoms(tile *dmmap.Tile) []disposals.Atom {
	var out []disposals.Atom
	for _, instance := range tile.Instances() {
		vars := instance.Prefab().Vars()
		out = append(out, disposals.Atom{
			Path:           instance.Prefab().Path(),
			Dir:            vars.IntV("dir", 0),
			InitializeDirs: vars.IntV("initialize_dirs", 0),
		})
	}
	return out
}

// LayBrush lays bundle on every route tile that does not already hold an
// identical atom and, with withDisposals, a planned disposal pipe, all as one
// operation. Planning happens first: when it fails nothing changes.
func LayBrush(dmm *dmmap.Dmm, route []util.Point, bundle []*dmmprefab.Prefab, withDisposals bool) error {
	pipes := map[util.Point]*dmmprefab.Prefab{}
	if withDisposals {
		edits, err := disposals.Plan(route, func(p util.Point) []disposals.Atom {
			if !dmm.HasTile(p) {
				return nil
			}
			return DisposalAtoms(dmm.GetTile(p))
		})
		if err != nil {
			return err
		}
		for _, edit := range edits {
			base, ok := dmmap.PrefabStorage.InitialV(edit.Path)
			if !ok {
				return toolError(edit.Path + " is not defined by the loaded environment")
			}
			pipes[edit.Coord] = dmmap.PrefabStorage.Get(edit.Path, dmvars.Set(base.Vars(), "dir", strconv.Itoa(edit.Dir)))
		}
	}
	if len(bundle) == 0 && len(pipes) == 0 {
		return toolError("nothing to lay: enable a utility line or the disposal pipe in Options")
	}
	var changed []util.Point
	adds := map[util.Point][]*dmmprefab.Prefab{}
	for _, p := range route {
		tile := dmm.GetTile(p)
		for _, prefab := range bundle {
			if !tileHolds(tile, prefab) {
				adds[p] = append(adds[p], prefab)
			}
		}
		if len(adds[p]) != 0 || pipes[p] != nil {
			changed = append(changed, p)
		}
	}
	if len(changed) == 0 {
		return nil // the run is already there
	}
	if !ed.TryBeginTileChange(changed...) {
		return toolError("another edit is in progress")
	}
	for _, p := range changed {
		tile := dmm.GetTile(p)
		if pipe := pipes[p]; pipe != nil {
			tile.InstancesRemoveByPath(disposals.PipeRoot)
			tile.InstancesAdd(pipe)
		}
		for _, prefab := range adds[p] {
			tile.InstancesAdd(prefab)
		}
		tile.InstancesRegenerate()
	}
	ed.UpdateCanvasByCoords(changed)
	ed.CommitOperation("Brush Run")
	return nil
}

func tileHolds(tile *dmmap.Tile, prefab *dmmprefab.Prefab) bool {
	want := maplint.AtomFromPrefab(prefab)
	for _, instance := range tile.Instances() {
		if maplint.SameAtom(maplint.AtomFromPrefab(instance.Prefab()), want) {
			return true
		}
	}
	return false
}

type toolError string

func (e toolError) Error() string { return string(e) }

func (t *ToolBrush) ActionContext(input ActionInput) ActionContext {
	context := actionContext(TNBrush, "Lay utility run", "the dragged route; see Options > Brush for what is laid", input)
	context.ModifierHelp = "Drag back to retract. Start on an existing disposal pipe to branch it; end on a bin, chute or outlet for a trunk."
	if t.route != nil {
		context.Captured = true
		context.Badge = "Route " + strconv.Itoa(len(t.route)) + " tiles"
	}
	if settings := t.settings(); settings == nil {
		setUnavailable(&context, "No map is active")
	} else if len(UtilityPrefabs(settings)) == 0 && !settings.BrushDisposals {
		setUnavailable(&context, "Nothing is enabled; see Options > Brush")
	}
	if bounds, ok := pointFootprint(input.Position); ok {
		context.Footprint, context.HasFootprint = bounds, true
	}
	return context
}

// APHELION EDIT ADDITION END
