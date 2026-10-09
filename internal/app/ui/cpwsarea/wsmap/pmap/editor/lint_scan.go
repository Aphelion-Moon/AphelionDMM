// APHELION EDIT ADDITION START - PLACEMENT LINT
package editor

import (
	"context"
	"fmt"

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
	Truncated bool // stopped at the findings limit
}

// PrepareLintScan captures an immutable committed revision of the map on the
// UI thread and returns the job to run on a worker. The job reads only that
// capture and the immutable rule set, so edits made meanwhile cannot race it;
// it checks ctx between tiles and stops after limit findings.
func (e *Editor) PrepareLintScan(limit int) (func(context.Context) (LintScanResult, error), error) {
	rs, file := e.lintRules()
	if rs == nil {
		return nil, fmt.Errorf("no map lint rules are loaded for this environment")
	}
	capture, _, err := e.CaptureSaveSnapshot(context.Background())
	if err != nil {
		return nil, fmt.Errorf("finish the current edit before scanning: %w", err)
	}
	return func(ctx context.Context) (LintScanResult, error) {
		snapshot := capture.Snapshot()
		var result LintScanResult
		for _, tile := range snapshot.Tiles {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			result.Tiles++
			if len(tile.State.Prefabs) == 0 {
				continue
			}
			atoms := make([]maplint.Atom, len(tile.State.Prefabs))
			for i, p := range tile.State.Prefabs {
				atoms[i] = prefabStateAtom(p)
			}
			for _, v := range rs.CheckTileInFile(file, atoms) {
				if limit > 0 && len(result.Findings) >= limit {
					result.Truncated = true
					return result, nil
				}
				result.Findings = append(result.Findings, LintFinding{
					Coord: util.Point{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z},
					Rule:  v.RuleFile, Help: v.Help, Message: v.Message,
				})
			}
		}
		return result, nil
	}, nil
}

// APHELION EDIT ADDITION END
