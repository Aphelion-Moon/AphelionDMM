// APHELION EDIT ADDITION START - PLACEMENT LINT
package editor

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/disposals"
	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/util"
)

// LintFinding is one violation located on the map.
type LintFinding struct {
	Coord   util.Point
	Rule    string // rule file, e.g. "multiple_windows.yml"
	Help    string
	Message string
}

// LintScanResult is the outcome of a map scan.
type LintScanResult struct {
	Findings  []LintFinding
	Tiles     int  // tiles examined
	Truncated bool // the findings list stopped at the limit
	// Violations counts every violation, including those past the limit.
	Violations int
	// The automatic-fix preview covers the whole map with every kind enabled.
	Fixes    []LintFixCount
	FixTiles int // tiles a fix would change
	Fixable  int // violations those fixes resolve
}

// PrepareLintScan captures an immutable committed revision of the map on the
// UI thread and returns the job to run on a worker. The job reads only that
// capture and the immutable rule set, so edits made meanwhile cannot race it;
// it checks ctx between tiles and lists at most limit findings, but still
// counts and previews fixes for the whole map.
func (e *Editor) PrepareLintScan(limit int) (func(context.Context) (LintScanResult, error), error) {
	rs, file := e.lintRules()
	if rs == nil {
		return nil, fmt.Errorf("no map lint rules are loaded for this environment")
	}
	capture, _, err := e.CaptureSaveSnapshot(context.Background())
	if err != nil {
		return nil, fmt.Errorf("finish the current edit before scanning: %w", err)
	}
	env := e.app.LoadedEnvironment()
	return func(ctx context.Context) (LintScanResult, error) {
		snapshot := capture.Snapshot()
		types := maplint.EnvironmentTypes(env)
		rs := auditedRules(rs, types)
		var result LintScanResult
		counts := lintFixCounter{}
		for _, tile := range snapshot.Tiles {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			result.Tiles++
			if len(tile.State.Prefabs) == 0 {
				continue
			}
			atoms := tileAtoms(tile.State)
			violations := rs.CheckTileInFile(file, atoms)
			result.Violations += len(violations)
			for _, v := range violations {
				if limit > 0 && len(result.Findings) >= limit {
					result.Truncated = true
					break
				}
				result.Findings = append(result.Findings, LintFinding{
					Coord: util.Point{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z},
					Rule:  v.RuleFile, Help: v.Help, Message: v.Message,
				})
			}
			if len(violations) == 0 {
				continue
			}
			if plan := rs.FixTile(file, atoms, types, maplint.AllFixes); plan.Changed() {
				result.FixTiles++
				result.Fixable += len(violations) - len(plan.Remaining)
				counts.add(plan.Fixes)
			}
		}
		result.Fixes = counts.list()
		// Disposal networks span tiles, so they are checked once over the map.
		for _, problem := range disposalProblems(snapshot.Tiles, types) {
			result.Violations++
			if limit > 0 && len(result.Findings) >= limit {
				result.Truncated = true
				continue
			}
			result.Findings = append(result.Findings, LintFinding{
				Coord: problem.Coord, Rule: DisposalsRule, Message: problem.Message,
				Help: "Disposal pipes must connect end to end; trunks sit under a bin, chute or outlet. The Brush tool lays connected runs.",
			})
		}
		return result, nil
	}, nil
}

// DisposalsRule names disposal network findings in Map Lint.
const DisposalsRule = "disposals network"

// disposalProblems checks every disposal pipe on the captured tiles. A pipe's
// connections come from its dir and its type's initialize_dirs.
func disposalProblems(tiles []model.Tile, types maplint.TypeTree) []disposals.Problem {
	byCoord := make(map[util.Point][]disposals.Atom, len(tiles))
	var pipes []util.Point
	inBounds := make(map[util.Point]bool, len(tiles))
	for _, tile := range tiles {
		p := util.Point{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z}
		inBounds[p] = true
		hasPipe := false
		for _, prefab := range tile.State.Prefabs {
			if !disposals.IsPipe(prefab.Path) && !disposals.IsMachine(prefab.Path) {
				continue
			}
			value := func(name string) int {
				raw, ok := prefab.Vars[name]
				if !ok && types != nil {
					raw, _ = types.Value(prefab.Path, name)
				}
				n, _ := strconv.Atoi(strings.TrimSpace(raw))
				return n
			}
			byCoord[p] = append(byCoord[p], disposals.Atom{Path: prefab.Path, Dir: value("dir"), InitializeDirs: value("initialize_dirs")})
			hasPipe = hasPipe || disposals.IsPipe(prefab.Path)
		}
		if hasPipe {
			pipes = append(pipes, p)
		}
	}
	return disposals.Check(pipes, func(p util.Point) ([]disposals.Atom, bool) { return byCoord[p], inBounds[p] })
}

// APHELION EDIT ADDITION END
