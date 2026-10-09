package editor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

const (
	lintWindowRule = "help: No duplicate windows.\n/obj/structure/window:\n  banned_neighbors:\n    /obj/structure/window:\n      identical: true\n"
	lintTableRule  = "help: One table per tile.\n/obj/structure/table:\n  banned_neighbors:\n    /obj/structure/table: {}\n"
)

func activateLint(t *testing.T) {
	t.Helper()
	activateLintFiles(t, map[string]string{"w.yml": lintWindowRule, "t.yml": lintTableRule})
}

// activateLintFiles loads the named rule files as the process-wide guard's
// repository rules and returns the guard once loading has finished.
func activateLintFiles(t *testing.T, files map[string]string) *maplint.Guard {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "tools", "maplint", "lints")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	g := maplint.Active()
	g.Begin(filepath.Join(root, "game.dme"), func(f func()) { f() })
	t.Cleanup(g.Reset)
	for i := 0; g.Status().Loading; i++ {
		if i > 2000 {
			t.Fatal("rules did not load")
		}
		time.Sleep(2 * time.Millisecond)
	}
	return g
}
func lintPrefab(path string, kv ...string) *dmmprefab.Prefab {
	vars := &dmvars.MutableVariables{}
	for i := 0; i+1 < len(kv); i += 2 {
		vars.Put(kv[i], kv[i+1])
	}
	return dmmprefab.New(dmmprefab.IdNone, path, vars.ToImmutable())
}

func lintEditor(t *testing.T, existing ...*dmmprefab.Prefab) *Editor {
	e := selectionEditor(t)
	for _, p := range existing {
		e.dmm.Tiles[0].InstancesAdd(p)
	}
	e.dmm.Tiles[0].InstancesRegenerate()
	e.initializeCollaboration()
	e.pMap.Snapshot().Sync()
	return e
}

func TestEvaluatePlacementPredictsWithoutMutating(t *testing.T) {
	activateLint(t)
	e := lintEditor(t, lintPrefab("/obj/structure/window", "dir", "4"))
	before := len(e.dmm.Tiles[0].Instances())
	point := util.Point{X: 1, Y: 1, Z: 1}
	if v := e.EvaluatePlacement(point, lintPrefab("/obj/structure/window", "dir", "4"), false); !v.Skip {
		t.Fatalf("identical window not predicted: %+v", v)
	}
	if v := e.EvaluatePlacement(point, lintPrefab("/obj/structure/window", "dir", "8"), false); len(v.Violations) != 0 {
		t.Fatalf("different window flagged: %+v", v)
	}
	if len(e.dmm.Tiles[0].Instances()) != before {
		t.Fatal("evaluation mutated the tile")
	}
	maplint.Active().Reset()
	if v := e.EvaluatePlacement(point, lintPrefab("/obj/structure/window", "dir", "4"), false); len(v.Violations) != 0 {
		t.Fatal("inactive guard produced violations")
	}
}

func TestFillSkipsIdenticalAndCountsSkippedTiles(t *testing.T) {
	activateLint(t)
	window := lintPrefab("/obj/structure/window", "dir", "4")
	e := lintEditor(t, lintPrefab("/obj/structure/window", "dir", "4"))
	for x := 2; x <= 3; x++ {
		tile := e.dmm.Tiles[0].Copy()
		tile.Coord = util.Point{X: x, Y: 1, Z: 1}
		tile.InstancesSet(nil)
		e.dmm.Tiles = append(e.dmm.Tiles, &tile)
	}
	e.dmm.MaxX = 3
	e.initializeCollaboration()
	e.pMap.Snapshot().Sync()
	before, _ := e.SaveSnapshot(context.Background())
	selection := editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 3, Y2: 1}, 1)
	if err := e.FillSelection(selection, window, false); err != nil {
		t.Fatal(err)
	}
	after, _ := e.SaveSnapshot(context.Background())
	if len(after.Tiles[0].State.Prefabs) != len(before.Tiles[0].State.Prefabs) {
		t.Fatal("identical window was placed on top of itself")
	}
	if !reflect.DeepEqual(before.Tiles[0], after.Tiles[0]) {
		t.Fatal("skipped tile changed")
	}
	for _, i := range []int{1, 2} {
		if len(after.Tiles[i].State.Prefabs) != len(before.Tiles[i].State.Prefabs)+1 {
			t.Fatalf("tile %d not filled", i)
		}
	}
	message, canReplace := e.PlacementLintNotice()
	if !strings.Contains(message, "Skipped 1 tile") || canReplace {
		t.Fatalf("notice = %q replace=%v", message, canReplace)
	}
}

func TestFillOfOnlyIdenticalTilesIsANoOp(t *testing.T) {
	activateLint(t)
	e := lintEditor(t, lintPrefab("/obj/structure/window", "dir", "4"))
	before, _ := e.SaveSnapshot(context.Background())
	selection := editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1)
	if err := e.FillSelection(selection, lintPrefab("/obj/structure/window", "dir", "4"), false); err != nil {
		t.Fatal(err)
	}
	after, _ := e.SaveSnapshot(context.Background())
	if after.Revision != before.Revision || len(e.pendingChanges) != 0 {
		t.Fatalf("no-op fill advanced revision %d -> %d", before.Revision, after.Revision)
	}
	if m, _ := e.PlacementLintNotice(); !strings.Contains(m, "Skipped 1 tile") {
		t.Fatalf("notice = %q", m)
	}
}

func TestFillWarnsButPlacesAndReplaceIsOneUndoableOperation(t *testing.T) {
	activateLint(t)
	e := lintEditor(t, lintPrefab("/obj/structure/table"))
	before, _ := e.SaveSnapshot(context.Background())
	selection := editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1)
	if err := e.FillSelection(selection, lintPrefab("/obj/structure/table/wood"), false); err != nil {
		t.Fatal(err)
	}
	placed, _ := e.SaveSnapshot(context.Background())
	if len(placed.Tiles[0].State.Prefabs) != len(before.Tiles[0].State.Prefabs)+1 {
		t.Fatal("violating placement was blocked")
	}
	message, canReplace := e.PlacementLintNotice()
	if !strings.Contains(message, "One table per tile.") || !strings.Contains(message, "1,1,1") || !canReplace {
		t.Fatalf("notice = %q replace=%v", message, canReplace)
	}
	e.ReplaceLintConflicts()
	replaced, _ := e.SaveSnapshot(context.Background())
	if replaced.Revision != placed.Revision+1 {
		t.Fatalf("replace used %d operations", replaced.Revision-placed.Revision)
	}
	tables := 0
	for _, p := range replaced.Tiles[0].State.Prefabs {
		if strings.HasPrefix(p.Path, "/obj/structure/table") {
			tables++
			if p.Path != "/obj/structure/table/wood" {
				t.Fatalf("kept the conflicting table %s", p.Path)
			}
		}
	}
	if tables != 1 {
		t.Fatalf("tables after replace = %d", tables)
	}
	if m, _ := e.PlacementLintNotice(); m != "" {
		t.Fatalf("notice persisted after replace: %q", m)
	}
	e.app.CommandStorage().UndoV("test")
	undone, _ := e.SaveSnapshot(context.Background())
	if !reflect.DeepEqual(undone.Tiles, placed.Tiles) {
		t.Fatal("undo did not restore the pre-replace tile")
	}
}

func TestReplaceNeverDeletesFilteredInstancesAndNoticeExpires(t *testing.T) {
	activateLint(t)
	e := lintEditor(t, lintPrefab("/obj/structure/table"))
	if err := e.FillSelection(editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1), lintPrefab("/obj/structure/table/wood"), false); err != nil {
		t.Fatal(err)
	}
	e.app.PathsFilter().TogglePath("/obj/structure/table")
	if e.app.PathsFilter().IsVisiblePath("/obj/structure/table") {
		t.Fatal("test filter did not hide the type")
	}
	snap, _ := e.SaveSnapshot(context.Background())
	e.ReplaceLintConflicts()
	after, _ := e.SaveSnapshot(context.Background())
	if !reflect.DeepEqual(snap.Tiles, after.Tiles) {
		t.Fatal("replace removed a type hidden by the filter")
	}
	start := lintNow()
	lintNow = func() time.Time { return start }
	t.Cleanup(func() { lintNow = time.Now })
	e.RecordPlacementLint(maplint.PlacementReport{Warned: 1, Summary: "x"})
	lintNow = func() time.Time { return start.Add(2 * placementLintTTL) }
	if m, _ := e.PlacementLintNotice(); m != "" {
		t.Fatalf("expired notice shown: %q", m)
	}
}

func TestLintScanReportsCoordinatesRespectsLimitAndCancel(t *testing.T) {
	activateLint(t)
	e := lintEditor(t, lintPrefab("/obj/structure/table"), lintPrefab("/obj/structure/table/wood"))
	run, err := e.PrepareLintScan(100)
	if err != nil {
		t.Fatal(err)
	}
	result, err := run(context.Background())
	if err != nil || result.Tiles == 0 || len(result.Findings) == 0 {
		t.Fatalf("scan = %+v err=%v", result, err)
	}
	for _, f := range result.Findings {
		if f.Coord != (util.Point{X: 1, Y: 1, Z: 1}) || f.Rule != "t.yml" || f.Help != "One table per tile." {
			t.Fatalf("finding = %+v", f)
		}
	}
	limited, _ := e.PrepareLintScan(1)
	if r, _ := limited(context.Background()); len(r.Findings) != 1 || !r.Truncated {
		t.Fatalf("limit ignored: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := run(ctx); err == nil {
		t.Fatal("cancelled scan completed")
	}
	maplint.Active().Reset()
	if _, err := e.PrepareLintScan(1); err == nil {
		t.Fatal("scan admitted without rules")
	}
}

func TestLintChangesCountsOnlyNewViolations(t *testing.T) {
	activateLint(t)
	rs, file := (&Editor{app: &editorTestApp{}, dmm: &dmmap.Dmm{}}).lintRules()
	table := func(path string) model.PrefabState { return model.PrefabState{Path: path} }
	changes := []model.TileChange{
		{Coord: model.Coord{X: 1, Y: 1, Z: 1}, Before: model.TileState{Prefabs: []model.PrefabState{table("/obj/structure/table")}}, After: model.TileState{Prefabs: []model.PrefabState{table("/obj/structure/table"), table("/obj/structure/table/wood")}}},
		{Coord: model.Coord{X: 2, Y: 1, Z: 1}, After: model.TileState{Prefabs: []model.PrefabState{table("/obj/structure/table")}}},
	}
	stats := lintChanges(rs, file, changes, 0)
	if stats.warned != 1 || stats.last.X != 1 || !stats.paste || stats.canReplace {
		t.Fatalf("stats = %+v", stats)
	}
	if got := stats.report().Message(); !strings.Contains(got, "Pasted with a lint warning") {
		t.Fatal(got)
	}
}
