// APHELION EDIT ADDITION START - LINT AUTOFIX
package editor

import (
	"context"
	"fmt"
	"sort"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/aphelion/spritedirs"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

const lintFixLabel = "Fix Map Lint"

// LintFixCount is how many fixes of one kind one rule file would receive.
type LintFixCount struct {
	Kind     maplint.FixKind
	RuleFile string
	Count    int
}

// LintFixResult reports an applied fix pass.
type LintFixResult struct {
	Tiles int // tiles changed
	Fixes int // individual fixes applied
	Err   error
}

type lintFixCounter map[LintFixCount]int

func (c lintFixCounter) add(fixes []maplint.Fix) {
	for _, f := range fixes {
		c[LintFixCount{Kind: f.Kind, RuleFile: f.RuleFile}]++
	}
}

func (c lintFixCounter) list() []LintFixCount {
	out := make([]LintFixCount, 0, len(c))
	for key, n := range c {
		key.Count = n
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].RuleFile < out[j].RuleFile
	})
	return out
}

func tileAtoms(state model.TileState) []maplint.Atom {
	atoms := make([]maplint.Atom, len(state.Prefabs))
	for i, p := range state.Prefabs {
		atoms[i] = prefabStateAtom(p)
	}
	return atoms
}

// auditedRules adds the editor audit (default-equal edits, inert dir) to a
// whole-map scan or fix. types is owned by the calling goroutine.
func auditedRules(rs *maplint.RuleSet, types maplint.TypeTree) *maplint.RuleSet {
	return rs.WithAudit(&maplint.Audit{Types: types, Dirs: spritedirs.ActiveDirs})
}

// withTypes binds the fixer to a goroutine-owned type tree.
func (f lintFixer) withTypes(types maplint.TypeTree) lintFixer {
	f.types, f.rules = types, auditedRules(f.rules, types)
	return f
}

// lintFixer plans one tile at a time. It reads only immutable inputs, so the
// local path can run it on the work owner's goroutine.
type lintFixer struct {
	rules *maplint.RuleSet
	file  string
	types maplint.TypeTree
	kinds maplint.FixKinds
}

// tile returns the fixed state of before. Kept and edited prefabs keep their
// stable IDs; untouched prefabs are reused exactly.
func (f lintFixer) tile(before model.TileState) (model.TileState, int, bool) {
	if len(before.Prefabs) == 0 {
		return before, 0, false
	}
	plan := f.rules.FixTile(f.file, tileAtoms(before), f.types, f.kinds)
	if !plan.Changed() {
		return before, 0, false
	}
	after := model.TileState{Prefabs: make([]model.PrefabState, len(plan.Atoms))}
	for i, atom := range plan.Atoms {
		original := before.Prefabs[plan.Source[i]]
		if maplint.SameAtom(prefabStateAtom(original), atom) {
			after.Prefabs[i] = original
			continue
		}
		vars := atom.Vars
		if vars == nil {
			vars = map[string]string{}
		}
		after.Prefabs[i] = model.PrefabState{StableID: original.StableID, Path: atom.Path, Vars: vars}
	}
	return after, len(plan.Fixes), true
}

func sortedCoords(tiles map[model.Coord]model.TileState) []model.Coord {
	coords := make([]model.Coord, 0, len(tiles))
	for coord := range tiles {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		a, b := coords[i], coords[j]
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	return coords
}

// ApplyLintFixes resolves what it can of the current map's lint violations as
// one operation with one history entry. Fixes are planned from the current
// authoritative tiles, never from an earlier scan, so they cannot act on stale
// state. Unshared documents prepare on the local work owner; session documents
// submit one tile-change operation. done runs on the UI thread.
func (e *Editor) ApplyLintFixes(kinds maplint.FixKinds, done func(LintFixResult)) error {
	rs, file := e.lintRules()
	if rs == nil {
		return fmt.Errorf("no map lint rules are loaded for this environment")
	}
	if !e.CanStartMapEdit() || e.HasPastePlacement() || e.localWork != nil {
		return fmt.Errorf("finish or cancel the current map edit first")
	}
	fixer := lintFixer{rules: rs, file: file, kinds: kinds}
	env := e.app.LoadedEnvironment()
	if local, ok := e.executor.(localEditExecutor); ok && !e.sessionOwned {
		return e.applyLocalLintFixes(local, fixer, env, done)
	}
	return e.applySessionLintFixes(fixer.withTypes(maplint.EnvironmentTypes(env)), done)
}

func (e *Editor) applyLocalLintFixes(local localEditExecutor, fixer lintFixer, env *dmenv.Dme, done func(LintFixResult)) error {
	base, generation := e.authoritativeTiles, e.attachmentGeneration
	var result LintFixResult
	return e.startLocalWork(local, true, 1024, func(ctx context.Context, reservation *resources.Reservation) ([]model.TileChange, error) {
		fixer := fixer.withTypes(maplint.EnvironmentTypes(env))
		var changes []model.TileChange
		bytes := uint64(1024)
		for _, coord := range sortedCoords(base) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			before := base[coord]
			after, fixes, changed := fixer.tile(before)
			if !changed {
				continue
			}
			bytes = addWorkBytes(addWorkBytes(bytes, localTileBytes(before)), localTileBytes(after))
			changes = append(changes, model.TileChange{Coord: coord, Before: before, After: after})
			result.Fixes += fixes
		}
		if err := reservation.Resize(bytes); err != nil {
			return nil, err
		}
		result.Tiles = len(changes)
		return changes, nil
	}, func(accepted engine.LocalAcceptance, backward []model.TileChange, err error) {
		if generation != e.attachmentGeneration || e.mapViewClosed {
			return
		}
		if err == nil && len(accepted.Changes) > 0 {
			e.pushLocalCommandOwned(local, lintFixLabel, accepted.Changes, backward, nil)
		}
		if err != nil {
			result = LintFixResult{Err: err}
		}
		if done != nil {
			done(result)
		}
	})
}

func (e *Editor) applySessionLintFixes(fixer lintFixer, done func(LintFixResult)) error {
	var coords []model.Coord
	for _, coord := range sortedCoords(e.authoritativeTiles) {
		if _, _, changed := fixer.tile(e.authoritativeTiles[coord]); changed {
			coords = append(coords, coord)
		}
	}
	if len(coords) == 0 {
		if done != nil {
			done(LintFixResult{})
		}
		return nil
	}
	if len(coords) > protocol.MaxOperationChanges {
		if bulk, ok := e.executor.(bulkCapableExecutor); !ok || !bulk.BulkEditsEnabled() {
			return fmt.Errorf("these fixes touch %d tiles; the session accepts at most %d per edit unless the host enables bulk edits", len(coords), protocol.MaxOperationChanges)
		}
	}
	points := make([]util.Point, len(coords))
	for n, coord := range coords {
		points[n] = util.Point{X: coord.X, Y: coord.Y, Z: coord.Z}
	}
	if !e.TryBeginTileChange(points...) {
		return fmt.Errorf("unable to capture the affected tiles")
	}
	release := func() { clear(e.pendingChanges) }
	var result LintFixResult
	afters := make([]model.TileState, len(coords))
	changed := make([]bool, len(coords))
	for n, coord := range coords {
		after, fixes, ok := fixer.tile(e.pendingChanges[coord])
		afters[n], changed[n] = after, ok
		result.Fixes += fixes
	}
	// Prepare every display tile before changing any, so a failure leaves the
	// display and the captured before-states untouched.
	prepared := make([]dmmap.Instances, len(coords))
	for n, coord := range coords {
		if !changed[n] {
			continue
		}
		instances, err := e.preparePasteTileState(coord, afters[n])
		if err != nil {
			release()
			return err
		}
		prepared[n] = instances
	}
	for n, coord := range coords {
		if changed[n] {
			e.dmm.GetTile(util.Point{X: coord.X, Y: coord.Y, Z: coord.Z}).Set(prepared[n])
			result.Tiles++
		}
	}
	e.CommitOperation(lintFixLabel)
	if done != nil {
		done(result)
	}
	return nil
}

// APHELION EDIT ADDITION END
