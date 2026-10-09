package tools

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// moveLintEditor adds the pieces ToolMove.onMove needs on top of the Add fake.
type moveLintEditor struct {
	*lintAddEditor
	evaluated []util.Point
}

func (e *moveLintEditor) EvaluatePlacement(p util.Point, prefab *dmmprefab.Prefab, replace bool) maplint.Verdict {
	e.evaluated = append(e.evaluated, p)
	return e.lintAddEditor.EvaluatePlacement(p, prefab, replace)
}

func (e *moveLintEditor) InstanceDelete(i *dmminstance.Instance) {
	e.m.GetTile(i.Coord()).InstancesRemoveByInstance(i)
}

// useBaseTiles gives Tile.InstancesRegenerate real base prefabs for the test.
func useBaseTiles(t *testing.T) {
	t.Helper()
	area, turf := dmmap.BaseArea, dmmap.BaseTurf
	t.Cleanup(func() { dmmap.BaseArea, dmmap.BaseTurf = area, turf })
	empty := (&dmvars.MutableVariables{}).ToImmutable()
	dmmap.BaseArea = dmmprefab.New(dmmprefab.IdNone, "/area", empty)
	dmmap.BaseTurf = dmmprefab.New(dmmprefab.IdNone, "/turf", empty)
}

// moveLintFixture builds a 1x3 map and a Move gesture holding a window on (1,1,1).
func moveLintFixture(t *testing.T) (*ToolMove, *moveLintEditor, *dmminstance.Instance) {
	t.Helper()
	useBaseTiles(t)
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	ctx := imgui.CreateContext(nil)
	t.Cleanup(ctx.Destroy)
	add, base := lintAddFixture(t) // restores ed on cleanup
	_ = add
	e := &moveLintEditor{lintAddEditor: base}
	ed = e
	e.m.GetTile(util.Point{X: 1, Y: 1, Z: 1}).InstancesAdd(base.prefab)
	instance := e.m.GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[0]
	move := newMove()
	move.instance = instance
	return move, e, instance
}

func TestMoveRefusesIdenticalDuplicateHop(t *testing.T) {
	move, e, instance := moveLintFixture(t)
	p1, p2, p3 := util.Point{X: 1, Y: 1, Z: 1}, util.Point{X: 2, Y: 1, Z: 1}, util.Point{X: 3, Y: 1, Z: 1}
	e.verdicts[p2] = maplint.Verdict{Skip: true, Violations: []maplint.Violation{{RuleFile: "w.yml", Help: "No duplicates.", Identical: true}}}

	move.onMove(p2)
	if instance.Coord() != p1 || windows(e.m.GetTile(p1)) != 1 || windows(e.m.GetTile(p2)) != 0 {
		t.Fatalf("refused hop moved the instance to %v", instance.Coord())
	}
	if len(e.reports) != 1 || e.reports[0].Skipped != 1 || e.reports[0].SkippedPath != "/obj/structure/window" {
		t.Fatalf("reports = %+v", e.reports)
	}
	move.onMove(p2) // revisiting the refused tile must not recount
	if len(e.reports) != 1 {
		t.Fatalf("refused tile recounted: %+v", e.reports)
	}

	move.onMove(p3) // the instance continues from its last valid tile
	if instance.Coord() != p3 || windows(e.m.GetTile(p3)) != 1 || windows(e.m.GetTile(p1)) != 0 {
		t.Fatalf("clean hop ended on %v", instance.Coord())
	}
	move.onStop(p3)
	if e.commits != 1 {
		t.Fatalf("commits = %d", e.commits)
	}
	if !move.lint.Empty() || move.lintSkipped != nil {
		t.Fatal("gesture state leaked into the next drag")
	}
}

func TestMoveWarnsAndAllowsOtherViolations(t *testing.T) {
	move, e, instance := moveLintFixture(t)
	p2, p3 := util.Point{X: 2, Y: 1, Z: 1}, util.Point{X: 3, Y: 1, Z: 1}
	e.verdicts[p2] = maplint.Verdict{Replace: []int{0}, Violations: []maplint.Violation{{RuleFile: "t.yml", Help: "One per tile."}}}

	move.onMove(p2)
	if instance.Coord() != p2 || windows(e.m.GetTile(p2)) != 1 {
		t.Fatalf("warned hop was refused: %v", instance.Coord())
	}
	last := e.reports[len(e.reports)-1]
	if last.Warned != 1 || last.Skipped != 0 || !last.CanReplace || last.X != 2 || last.Placed.Path != "/obj/structure/window" || last.Placed.Vars["dir"] != "4" {
		t.Fatalf("report = %+v", last)
	}

	// A clean tile afterwards leaves the earlier notice for the editor to expire.
	move.onMove(p3)
	if instance.Coord() != p3 {
		t.Fatalf("clean hop ended on %v", instance.Coord())
	}
	for _, r := range e.reports {
		if r.Empty() {
			t.Fatal("a clean hop must not clear the notice")
		}
	}
}

func TestMoveShiftOffsetDragIsNotChecked(t *testing.T) {
	move, e, instance := moveLintFixture(t)
	p2 := util.Point{X: 2, Y: 1, Z: 1}
	e.verdicts[p2] = maplint.Verdict{Skip: true, Violations: []maplint.Violation{{Identical: true}}}
	move.actionContext.Modifiers.Shift = true
	move.onMove(p2)
	if len(e.evaluated) != 0 || len(e.reports) != 0 || instance.Coord() != (util.Point{X: 1, Y: 1, Z: 1}) {
		t.Fatalf("Shift drag was linted: %v %v", e.evaluated, e.reports)
	}
}

func TestMoveToSameTileIsNotChecked(t *testing.T) {
	move, e, _ := moveLintFixture(t)
	move.onMove(util.Point{X: 1, Y: 1, Z: 1})
	if len(e.evaluated) != 0 {
		t.Fatalf("evaluated = %v", e.evaluated)
	}
}

func TestMoveWithoutLinterEditorStillMoves(t *testing.T) {
	// An editor that cannot lint must keep the old behaviour.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	useBaseTiles(t)
	previous := ed
	defer func() { ed = previous }()
	m := &dmmap.Dmm{MaxX: 2, MaxY: 1, MaxZ: 1}
	for x := 1; x <= 2; x++ {
		m.Tiles = append(m.Tiles, &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: 1}})
	}
	prefab := dmmprefab.New(dmmprefab.IdNone, "/obj/structure/window", (&dmvars.MutableVariables{}).ToImmutable())
	m.GetTile(util.Point{X: 1, Y: 1, Z: 1}).InstancesAdd(prefab)
	plain := &plainMoveEditor{lifecycleEditor: &lifecycleEditor{m: m}}
	ed = plain
	move := newMove()
	move.instance = m.GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[0]
	move.onMove(util.Point{X: 2, Y: 1, Z: 1})
	if move.instance.Coord().X != 2 {
		t.Fatalf("instance at %v", move.instance.Coord())
	}
}

type plainMoveEditor struct{ *lifecycleEditor }

func (*plainMoveEditor) TryBeginTileChange(...util.Point) bool { return true }
func (e *plainMoveEditor) InstanceDelete(i *dmminstance.Instance) {
	e.m.GetTile(i.Coord()).InstancesRemoveByInstance(i)
}
