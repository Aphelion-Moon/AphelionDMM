package editor

import (
	"context"
	"reflect"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

func largeBulkEditor(t *testing.T) *Editor {
	e := selectionEditor(t)
	dmmap.BaseArea = e.dmm.Tiles[0].Instances()[0].Prefab()
	dmmap.BaseTurf = e.dmm.Tiles[0].Instances()[1].Prefab()
	t.Cleanup(func() { dmmap.BaseArea = nil; dmmap.BaseTurf = nil })
	for x := 2; x <= 160; x++ {
		tile := &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: 1}}
		tile.InstancesSet(e.dmm.Tiles[0].Instances().Prefabs())
		e.dmm.Tiles = append(e.dmm.Tiles, tile)
	}
	e.dmm.MaxX = 160
	e.initializeCollaboration()
	e.pMap.Snapshot().Sync()
	e.app.(*editorTestApp).runLater = make(chan func(), 32)
	return e
}
func TestBulkPrefabAndSelectionDeleteUseOneHistoryRevision(t *testing.T) {
	for _, mode := range []string{"replace", "delete matching", "delete selection"} {
		t.Run(mode, func(t *testing.T) {
			e := largeBulkEditor(t)
			app := e.app.(*editorTestApp)
			before, err := e.SaveSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			original := e.dmm.Tiles[0].Instances()[2].Prefab()
			switch mode {
			case "replace":
				e.ReplacePrefab(original, dmmprefab.New(0, original.Path(), dmvars.Set(original.Vars(), "dir", "4")))
			case "delete matching":
				e.InstancesDeleteByPrefab(original)
			case "delete selection":
				if !e.tryScheduleSelectionDelete(editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 160, Y2: 1}, 1)) {
					t.Fatal("not scheduled")
				}
			}
			if e.localWork == nil || len(e.pendingChanges) != 0 {
				t.Fatal("bulk work synchronously captured display")
			}
			if e.dmm.Tiles[0].Instances()[2].Prefab() != original {
				t.Fatal("display changed before worker acceptance")
			}
			for e.localWork != nil {
				app.runScheduled(t)
			}
			after, err := e.SaveSnapshot(context.Background())
			if err != nil || after.Revision != before.Revision+1 {
				t.Fatal("bulk edit was not atomic", err)
			}
			if mode == "replace" {
				if after.Tiles[159].State.Prefabs[2].Vars["dir"] != "4" {
					t.Fatal("replacement missed last tile")
				}
			} else if len(after.Tiles[159].State.Prefabs) != 2 {
				t.Fatal("delete lost defaults or missed a tile")
			}
			app.commands.UndoV("test")
			for e.localWork != nil {
				app.runScheduled(t)
			}
			undone, err := e.SaveSnapshot(context.Background())
			if err != nil || !reflect.DeepEqual(before.Tiles, undone.Tiles) {
				t.Fatal("bulk undo changed exact source", err)
			}
		})
	}
}
func TestBulkAdmissionFailureLeavesCommittedDisplayIntact(t *testing.T) {
	e := largeBulkEditor(t)
	e.app = &noopReportingApp{editorTestApp: e.app.(*editorTestApp)}
	e.workBudget = resources.NewFixedBudget(1)
	before := e.dmm.Copy()
	e.InstancesDeleteByPrefab(e.dmm.Tiles[0].Instances()[2].Prefab())
	if e.localWork != nil || len(e.pendingChanges) != 0 || !reflect.DeepEqual(before, e.dmm.Copy()) {
		t.Fatal("failed bulk admission mutated display")
	}
}

func TestLargeSearchBatchKeepsStableIDsAndExactUndo(t *testing.T) {
	e := largeBulkEditor(t)
	app := e.app.(*editorTestApp)
	before, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var targets []*dmminstance.Instance
	for _, tile := range e.dmm.Tiles {
		targets = append(targets, tile.Instances()[2])
	}
	targets = append(targets, targets[0], e.dmm.Tiles[0].Instances()[0])
	replacement := dmmprefab.New(0, "/obj/replaced", dmvars.FromParent(nil))
	e.CommitInstanceBatch(targets, replacement, "Replace search results")
	if e.localWork == nil {
		t.Fatal("large search bypassed owner")
	}
	for e.localWork != nil {
		app.runScheduled(t)
	}
	after, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for n, tile := range after.Tiles {
		if tile.State.Prefabs[2].StableID != before.Tiles[n].State.Prefabs[2].StableID || tile.State.Prefabs[2].Path != "/obj/replaced" || tile.State.Prefabs[0].Path != before.Tiles[n].State.Prefabs[0].Path {
			t.Fatal("batch changed identity or incompatible target")
		}
	}
	app.commands.UndoV("test")
	for e.localWork != nil {
		app.runScheduled(t)
	}
	undone, err := e.SaveSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(before.Tiles, undone.Tiles) {
		t.Fatal("batch undo lost source", err)
	}
}
