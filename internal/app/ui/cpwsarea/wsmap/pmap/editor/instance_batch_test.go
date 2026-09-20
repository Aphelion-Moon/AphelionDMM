package editor

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestInstanceBatchReplacementPreservesMixedTargetsAndHistory(t *testing.T) {
	e := selectionEditor(t)
	first := e.dmm.Tiles[0]
	second := &dmmap.Tile{Coord: util.Point{X: 2, Y: 1, Z: 1}}
	second.InstancesSet(first.Instances().Prefabs())
	e.dmm.Tiles = append(e.dmm.Tiles, second)
	e.dmm.MaxX = 2
	for range 64 {
		first.InstancesAdd(first.Instances()[2].Prefab())
	}
	e.initializeCollaboration()
	before := model.CloneSnapshot(e.authoritative)
	// Interleave tiles, repeat a target, and include an incompatible base type.
	targets := []*dmminstance.Instance{second.Instances()[2], first.Instances()[0]}
	targets = append(targets, first.Instances()[2:]...)
	targets = append(targets, second.Instances()[2])
	replacement := dmmprefab.New(dmmprefab.IdNone, "/obj/replaced", dmvars.Set(first.Instances()[2].Prefab().Vars(), "dir", "4"))
	e.CommitInstanceBatch(targets, replacement, "Replace mixed search results")
	after, err := e.SaveSnapshot(context.Background())
	if err != nil || after.Revision != before.Revision+1 || !e.CanStartMapEdit() {
		t.Fatalf("batch failed: %v", err)
	}
	for index, tile := range after.Tiles {
		for offset, instance := range tile.State.Prefabs {
			original := before.Tiles[index].State.Prefabs[offset]
			if instance.StableID != original.StableID {
				t.Fatal("replacement changed instance order or identity")
			}
			if offset < 2 {
				if !reflect.DeepEqual(instance, original) {
					t.Fatal("replacement changed an incompatible or unselected base")
				}
			} else if instance.Path != "/obj/replaced" {
				t.Fatal("replacement omitted a selected object")
			}
		}
	}
	e.app.CommandStorage().UndoV("test")
	undone, err := e.SaveSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(undone.Tiles, before.Tiles) {
		t.Fatalf("undo failed: %v", err)
	}
	e.app.CommandStorage().RedoV("test")
	redone, err := e.SaveSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(redone.Tiles, after.Tiles) {
		t.Fatalf("redo failed: %v", err)
	}
}

func TestInstanceBatchRetainsIntentAfterSnapshotFailure(t *testing.T) {
	e := selectionEditor(t)
	app := &noopReportingApp{editorTestApp: e.app.(*editorTestApp)}
	e.app = app
	e.executor = &countedSnapshotExecutor{Executor: e.executor, failFirst: true}
	instance := e.dmm.Tiles[0].Instances()[2]
	before := model.CloneSnapshot(e.authoritative)
	replacement := dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "4"))
	e.CommitInstanceBatch([]*dmminstance.Instance{instance}, replacement, "Search replacement")
	if len(app.errors) != 1 || e.CanStartMapEdit() || len(e.pendingChanges) != 1 || instance.Prefab() != replacement || e.app.CommandStorage().HasUndoV("test") {
		t.Fatal("snapshot failure discarded intent or permitted a new edit/history")
	}
	if _, err := e.SaveSnapshot(context.Background()); err == nil {
		t.Fatal("unsubmitted replacement became saveable")
	}
	// A later read failure is different from an invalid selected prefab: the
	// valid edit remains explicitly retryable without recapturing its before.
	e.CommitOperation("Retry search replacement")
	after, err := e.SaveSnapshot(context.Background())
	if err != nil || after.Revision != before.Revision+1 || !e.CanStartMapEdit() || !e.app.CommandStorage().HasUndoV("test") {
		t.Fatalf("retry failed: %v", err)
	}
	e.app.CommandStorage().UndoV("test")
	undone, err := e.SaveSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(undone.Tiles, before.Tiles) {
		t.Fatalf("undo did not restore exact before values: %v", err)
	}
}

// The real editor batch and no-op commit paths are timed. This deliberately
// excludes changed-operation submission, rendering and global update jobs.
func BenchmarkInstanceBatchUnchangedDenseTile(b *testing.B) {
	for _, count := range []int{32, 256, 2048, 4096} {
		b.Run(fmt.Sprintf("instances=%d", count), func(b *testing.B) {
			e := selectionEditor(b)
			tile := e.dmm.Tiles[0]
			prefab := tile.Instances()[2].Prefab()
			for index := 1; index < count; index++ {
				tile.InstancesAdd(prefab)
			}
			for index, instance := range tile.Instances() {
				instance.SetStableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", index+1))
			}
			e.documentID = "01890f3e-7b5c-7abc-8def-111111111111"
			e.initializeCollaboration()
			if e.collaborationErr != nil {
				b.Fatal(e.collaborationErr)
			}
			instances := tile.Instances()[2:]
			before, err := mapadapter.CaptureTile(tile)
			if err != nil {
				b.Fatal(err)
			}
			want, err := e.authoritative.Hash()
			if err != nil {
				b.Fatal(err)
			}
			counted := &countedSnapshotExecutor{Executor: e.executor}
			e.executor = counted
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				e.CommitInstanceBatch(instances, prefab, "Unchanged search batch")
			}
			b.StopTimer()
			if counted.snapshots != 0 {
				b.Fatal("no-op batch read authority during timing")
			}
			after, err := mapadapter.CaptureTile(tile)
			if err != nil || !before.Equal(after) || !e.CanStartMapEdit() || e.app.CommandStorage().HasUndoV("test") {
				b.Fatal("no-op batch changed display/history or left a fault")
			}
			snapshot, err := e.SaveSnapshot(context.Background())
			if err != nil {
				b.Fatal(err)
			}
			got, err := snapshot.Hash()
			if err != nil || got != want || snapshot.Revision != 0 {
				b.Fatal("no-op batch changed authority")
			}
			b.Logf("instances=%d sha256=%s", count, got)
		})
	}
}
