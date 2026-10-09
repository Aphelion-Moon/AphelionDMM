package tools

import (
	"testing"

	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// lintAddEditor adds the placement-lint capability to the lifecycle fake.
type lintAddEditor struct {
	*lifecycleEditor
	prefab   *dmmprefab.Prefab
	verdicts map[util.Point]maplint.Verdict
	reports  []maplint.PlacementReport
}

func (e *lintAddEditor) SelectedPrefab() (*dmmprefab.Prefab, bool) { return e.prefab, true }
func (e *lintAddEditor) TryBeginTileChange(...util.Point) bool     { return true }
func (e *lintAddEditor) EvaluatePlacement(p util.Point, _ *dmmprefab.Prefab, _ bool) maplint.Verdict {
	return e.verdicts[p]
}
func (e *lintAddEditor) RecordPlacementLint(r maplint.PlacementReport) {
	e.reports = append(e.reports, r)
}

func lintAddFixture(t *testing.T) (*ToolAdd, *lintAddEditor) {
	t.Helper()
	previous := ed
	t.Cleanup(func() { ed = previous })
	m := &dmmap.Dmm{MaxX: 3, MaxY: 1, MaxZ: 1}
	for x := 1; x <= 3; x++ {
		m.Tiles = append(m.Tiles, &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: 1}})
	}
	vars := &dmvars.MutableVariables{}
	vars.Put("dir", "4")
	e := &lintAddEditor{
		lifecycleEditor: &lifecycleEditor{m: m},
		prefab:          dmmprefab.New(dmmprefab.IdNone, "/obj/structure/window", vars.ToImmutable()),
		verdicts:        map[util.Point]maplint.Verdict{},
	}
	ed = e
	return newAdd(), e
}

func TestAddStrokeSkipsIdenticalTilesWarnsOthersAndCounts(t *testing.T) {
	add, e := lintAddFixture(t)
	skip := maplint.Verdict{Skip: true, Violations: []maplint.Violation{{RuleFile: "w.yml", Help: "No duplicates.", Identical: true}}}
	warn := maplint.Verdict{Replace: []int{0}, Violations: []maplint.Violation{{RuleFile: "t.yml", Help: "One per tile."}}}
	p1, p2, p3 := util.Point{X: 1, Y: 1, Z: 1}, util.Point{X: 2, Y: 1, Z: 1}, util.Point{X: 3, Y: 1, Z: 1}
	e.verdicts[p1], e.verdicts[p2] = skip, warn

	add.onStart(p1)
	add.onMove(p2)
	add.onMove(p1) // revisiting a skipped tile must not recount
	add.onMove(p3)
	add.onStop(p3)

	if n := windows(e.m.GetTile(p1)); n != 0 {
		t.Fatalf("identical tile received %d instances", n)
	}
	if n := windows(e.m.GetTile(p2)); n != 1 {
		t.Fatalf("warned tile must still be placed, has %d", n)
	}
	if n := windows(e.m.GetTile(p3)); n != 1 {
		t.Fatalf("clean tile has %d", n)
	}
	last := e.reports[len(e.reports)-1]
	if last.Skipped != 1 || last.Warned != 1 || !last.CanReplace || last.X != 2 || last.Placed.Path != "/obj/structure/window" || last.Placed.Vars["dir"] != "4" {
		t.Fatalf("report = %+v", last)
	}
	if e.commits != 1 {
		t.Fatalf("commits = %d", e.commits)
	}
	if !add.lint.Empty() || add.lintSkipped != nil {
		t.Fatal("stroke state leaked into the next stroke")
	}
}

func TestAddStrokeOfOnlyDuplicatesCommitsNothing(t *testing.T) {
	add, e := lintAddFixture(t)
	p := util.Point{X: 1, Y: 1, Z: 1}
	e.verdicts[p] = maplint.Verdict{Skip: true, Violations: []maplint.Violation{{Identical: true}}}
	add.onStart(p)
	add.onStop(p)
	if e.commits != 0 || windows(e.m.GetTile(p)) != 0 {
		t.Fatalf("commits=%d", e.commits)
	}
}

func TestAddHoverShowsWouldBeViolation(t *testing.T) {
	add, e := lintAddFixture(t)
	p := util.Point{X: 2, Y: 1, Z: 1}
	e.verdicts[p] = maplint.Verdict{Violations: []maplint.Violation{{RuleFile: "t.yml", Help: "One per tile."}}}
	context := add.ActionContext(ActionInput{Position: p, InBounds: true})
	if context.LintWarning == "" || !context.Available {
		t.Fatalf("hover = %+v", context)
	}
	e.verdicts[p] = maplint.Verdict{Skip: true, Violations: []maplint.Violation{{RuleFile: "w.yml", Identical: true}}}
	if got := add.ActionContext(ActionInput{Position: p, InBounds: true}).LintWarning; got == "" {
		t.Fatal("duplicate hover missing")
	}
	if got := add.ActionContext(ActionInput{Position: util.Point{X: 3, Y: 1, Z: 1}, InBounds: true}).LintWarning; got != "" {
		t.Fatalf("clean tile warned: %q", got)
	}
	if got := add.ActionContext(ActionInput{Position: p, InBounds: false}).LintWarning; got != "" {
		t.Fatal("out-of-bounds tile warned")
	}
}

func windows(tile *dmmap.Tile) (n int) {
	for _, i := range tile.Instances() {
		if i != nil && i.Prefab() != nil && i.Prefab().Path() == "/obj/structure/window" {
			n++
		}
	}
	return n
}
