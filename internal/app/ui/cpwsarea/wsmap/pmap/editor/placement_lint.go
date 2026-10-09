// APHELION EDIT ADDITION START - PLACEMENT LINT
package editor

import (
	"context"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// placementLintTTL is how long placement feedback stays on screen.
const placementLintTTL = 12 * time.Second

var lintNow = time.Now

// lintRules returns the published repository rules and the map file name rules
// match skip_files against. Rules are advisory: a nil result means "no check".
func (e *Editor) lintRules() (*maplint.RuleSet, string) {
	rs := maplint.Active().RuleSet()
	if rs == nil || rs.RuleCount() == 0 || e.dmm == nil {
		return nil, ""
	}
	root := ""
	if env := e.app.LoadedEnvironment(); env != nil {
		root = env.RootDir
	}
	return rs, maplint.MapFile(e.dmm.Path.Absolute, root)
}

func prefabStateAtom(p model.PrefabState) maplint.Atom {
	return maplint.Atom{Path: p.Path, Vars: p.Vars}
}

// EvaluatePlacement predicts the lint result of placing prefab on the tile
// using the same channel-replacement rules as the Add tool, without touching
// the map. replaceObjects is Add's Alt mode.
func (e *Editor) EvaluatePlacement(coord util.Point, prefab *dmmprefab.Prefab, replaceObjects bool) maplint.Verdict {
	rs, file := e.lintRules()
	if rs == nil || prefab == nil || !e.dmm.HasTile(coord) {
		return maplint.Verdict{}
	}
	var existing []maplint.Atom
	for _, instance := range e.dmm.GetTile(coord).Instances() {
		path := instance.Prefab().Path()
		switch {
		case !replaceObjects && dm.IsPath(prefab.Path(), "/area") && dm.IsPath(path, "/area"):
			continue
		case !replaceObjects && dm.IsPath(prefab.Path(), "/turf") && dm.IsPath(path, "/turf"):
			continue
		case replaceObjects && dm.IsPath(prefab.Path(), "/obj") && dm.IsPath(path, "/obj"):
			continue
		}
		existing = append(existing, maplint.AtomFromPrefab(instance.Prefab()))
	}
	return maplint.EvaluateRules(rs, file, existing, maplint.AtomFromPrefab(prefab))
}

// RecordPlacementLint publishes transient placement feedback. An empty report
// clears it.
func (e *Editor) RecordPlacementLint(report maplint.PlacementReport) {
	if report.Empty() {
		e.lintReport = maplint.PlacementReport{}
		return
	}
	e.lintReport, e.lintReportAt = report, lintNow()
}

// PlacementLintNotice is the current feedback text and whether "Replace
// existing" is offered. It expires on its own and never touches the map.
func (e *Editor) PlacementLintNotice() (message string, canReplace bool) {
	if e.lintReport.Empty() {
		return "", false
	}
	if lintNow().Sub(e.lintReportAt) > placementLintTTL {
		e.lintReport = maplint.PlacementReport{}
		return "", false
	}
	return e.lintReport.Message(), e.lintReport.CanReplace && !e.lintReport.Paste
}

// DismissPlacementLint hides the feedback.
func (e *Editor) DismissPlacementLint() { e.lintReport = maplint.PlacementReport{} }

// ReplaceLintConflicts removes the existing instances that conflict with the
// last warned placement, as one undoable batch through the normal operation
// path. It re-reads the tile, so stale feedback cannot delete the wrong thing.
// Instances hidden by the local filter are never touched.
func (e *Editor) ReplaceLintConflicts() {
	report := e.lintReport
	e.lintReport = maplint.PlacementReport{}
	rs, file := e.lintRules()
	coord := util.Point{X: report.X, Y: report.Y, Z: report.Z}
	if rs == nil || !report.CanReplace || !e.dmm.HasTile(coord) || !e.CanStartMapEdit() {
		return
	}
	instances := e.dmm.GetTile(coord).Instances()
	placedIndex := -1
	for i := len(instances) - 1; i >= 0; i-- {
		if maplint.SameAtom(maplint.AtomFromPrefab(instances[i].Prefab()), report.Placed) {
			placedIndex = i
			break
		}
	}
	if placedIndex < 0 {
		return
	}
	existing := make([]maplint.Atom, 0, len(instances)-1)
	owners := make([]*dmminstance.Instance, 0, len(instances)-1)
	for i, instance := range instances {
		if i != placedIndex {
			existing = append(existing, maplint.AtomFromPrefab(instance.Prefab()))
			owners = append(owners, instance)
		}
	}
	verdict := maplint.EvaluateRules(rs, file, existing, report.Placed)
	filter := e.app.PathsFilter()
	var targets []*dmminstance.Instance
	for _, index := range verdict.Replace {
		if filter == nil || filter.IsVisiblePath(owners[index].Prefab().Path()) {
			targets = append(targets, owners[index])
		}
	}
	if len(targets) != 0 {
		e.CommitInstanceBatch(targets, nil, "Replace Conflicting Atoms")
	}
}

// lintStats accumulates one gesture's placement results. Workers fill it and
// the UI thread publishes it after completion.
type lintStats struct {
	warned, skipped int
	skippedPath     string
	last            util.Point
	placed          maplint.Atom
	summary         string
	canReplace      bool
	paste           bool
}

func (s *lintStats) skip(placed maplint.Atom) {
	s.skipped++
	s.skippedPath = placed.Path
}

func (s *lintStats) warn(p util.Point, placed maplint.Atom, v maplint.Verdict) {
	s.warned++
	s.last, s.placed, s.summary, s.canReplace = p, placed, v.Summary(), len(v.Replace) != 0
}

func (s *lintStats) report() maplint.PlacementReport {
	return maplint.PlacementReport{
		X: s.last.X, Y: s.last.Y, Z: s.last.Z, Placed: s.placed, Summary: s.summary,
		Warned: s.warned, Skipped: s.skipped, SkippedPath: s.skippedPath, CanReplace: s.canReplace, Paste: s.paste,
	}
}

// atomsWithout returns the atoms of prefabs except the placed one, found by
// stable ID (falling back to the last equal atom).
func atomsWithout(prefabs []model.PrefabState, placed model.PrefabState) []maplint.Atom {
	skip := -1
	for i := len(prefabs) - 1; i >= 0; i-- {
		if placed.StableID != "" && prefabs[i].StableID == placed.StableID {
			skip = i
			break
		}
	}
	if skip < 0 {
		for i := len(prefabs) - 1; i >= 0; i-- {
			if maplint.SameAtom(prefabStateAtom(prefabs[i]), prefabStateAtom(placed)) {
				skip = i
				break
			}
		}
	}
	out := make([]maplint.Atom, 0, len(prefabs))
	for i, p := range prefabs {
		if i != skip {
			out = append(out, prefabStateAtom(p))
		}
	}
	return out
}

// newPlacedPrefabs returns the indices of after prefabs that did not come from
// before. Prefabs match by stable ID when they have one, otherwise one-to-one
// by equal content, so identity-less payload data still compares correctly.
func newPlacedPrefabs(before, after []model.PrefabState) []int {
	ids := make(map[model.StableID]bool, len(before))
	unmatched := make([]bool, len(before))
	for i, p := range before {
		unmatched[i] = true
		if p.StableID != "" {
			ids[p.StableID] = true
		}
	}
	var out []int
	for i, p := range after {
		if p.StableID != "" {
			if !ids[p.StableID] {
				out = append(out, i)
			}
			continue
		}
		found := false
		for j := range before {
			if unmatched[j] && before[j].StableID == "" && maplint.SameAtom(prefabStateAtom(before[j]), prefabStateAtom(p)) {
				unmatched[j], found = false, true
				break
			}
		}
		if !found {
			out = append(out, i)
		}
	}
	return out
}

// lintRandomFill applies Fill's lint semantics to the composed tiles of a
// random Fill: a tile whose composed state holds an exact `identical: true`
// duplicate of a newly placed atom is dropped from the result, any other new
// violation only warns. changes is not modified. It runs on the worker for
// large selections, so it checks ctx between tiles and reads only its inputs.
func lintRandomFill(ctx context.Context, rs *maplint.RuleSet, file string, changes []model.TileChange) ([]model.TileChange, *lintStats, error) {
	stats := &lintStats{}
	if rs == nil {
		return changes, stats, nil
	}
	kept := make([]model.TileChange, 0, len(changes))
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		point := util.Point{X: change.Coord.X, Y: change.Coord.Y, Z: change.Coord.Z}
		skip := false
		var warns []maplint.Verdict
		var warned []maplint.Atom
		for _, index := range newPlacedPrefabs(change.Before.Prefabs, change.After.Prefabs) {
			placed := change.After.Prefabs[index]
			verdict := maplint.EvaluateRules(rs, file, atomsWithout(change.After.Prefabs, placed), prefabStateAtom(placed))
			if verdict.Skip {
				stats.skip(prefabStateAtom(placed))
				skip = true
				break
			}
			if len(verdict.Violations) != 0 {
				warns, warned = append(warns, verdict), append(warned, prefabStateAtom(placed))
			}
		}
		if skip {
			continue // the tile is left untouched
		}
		for i := range warns {
			stats.warn(point, warned[i], warns[i])
		}
		kept = append(kept, change)
	}
	return kept, stats, nil
}

// lintChanges summarises violations a set of tile changes introduces (paste).
func lintChanges(rs *maplint.RuleSet, file string, changes []model.TileChange, limit int) *lintStats {
	stats := &lintStats{paste: true}
	if rs == nil {
		return stats
	}
	for i, change := range changes {
		if limit > 0 && i >= limit {
			break
		}
		before := make([]maplint.Atom, len(change.Before.Prefabs))
		for j, p := range change.Before.Prefabs {
			before[j] = prefabStateAtom(p)
		}
		after := make([]maplint.Atom, len(change.After.Prefabs))
		for j, p := range change.After.Prefabs {
			after[j] = prefabStateAtom(p)
		}
		if violations := rs.NewViolations(file, before, after); len(violations) != 0 {
			stats.warned++
			stats.last = util.Point{X: change.Coord.X, Y: change.Coord.Y, Z: change.Coord.Z}
			stats.summary = maplint.Describe(violations)
		}
	}
	return stats
}

// APHELION EDIT ADDITION END
