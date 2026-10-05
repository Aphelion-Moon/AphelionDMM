package editor

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/repath"
	"sdmm/internal/aphelion/repath/envtypes"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// pathMigrationEditor holds tiles of area, turf, a known object and an
// unknown /obj/old carrying a variable the environment does not define.
func pathMigrationEditor(t *testing.T, tiles int) (*Editor, *editorTestApp) {
	t.Helper()
	e := selectionEditor(t)
	variables := &dmvars.MutableVariables{}
	variables.Put("legacy", `"kept"`)
	e.dmm.Tiles[0].InstancesAdd(dmmprefab.New(dmmprefab.IdNone, "/obj/old", variables.ToImmutable()))
	// Rows of at most 128 keep every dimension within model limits; the last row
	// is complete so the grid stays rectangular.
	width := min(tiles, 128)
	height := (tiles + width - 1) / width
	e.dmm.Tiles = e.dmm.Tiles[:1]
	for n := 1; n < width*height; n++ {
		tile := &dmmap.Tile{Coord: util.Point{X: n%width + 1, Y: n/width + 1, Z: 1}}
		tile.InstancesSet(e.dmm.Tiles[0].Instances().Prefabs())
		e.dmm.Tiles = append(e.dmm.Tiles, tile)
	}
	e.dmm.MaxX, e.dmm.MaxY = width, height
	e.initializeCollaboration()
	if len(e.authoritativeTiles) != width*height {
		t.Fatalf("fixture has %d authoritative tiles, want %d", len(e.authoritativeTiles), width*height)
	}
	e.pMap.Snapshot().Sync()
	app := e.app.(*editorTestApp)
	app.runLater = make(chan func(), 32)
	return e, app
}

func pathMigrationTransformer(t *testing.T, e *Editor) *repath.Transformer {
	t.Helper()
	target, err := repath.NewIndex(context.Background(), envtypes.New(e.app.LoadedEnvironment()))
	if err != nil {
		t.Fatal(err)
	}
	plan := repath.NewPlan()
	plan.Paths["/obj/old"] = repath.Decision{Kind: repath.Apply, Via: repath.ViaRule, Rule: repath.RepathRule("/obj/old", "/obj/foo", map[string]string{"dir": "4"})}
	transformer, err := repath.Compile(plan, target, []string{"/obj/old"}, repath.Resolvers{})
	if err != nil {
		t.Fatal(err)
	}
	return transformer
}

func TestPathMigrationIsOneUndoableLocalRevision(t *testing.T) {
	for _, size := range []int{1, 256} {
		e, app := pathMigrationEditor(t, size)
		before, err := e.SaveSnapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var result *PathMigrationResult
		if err := e.ApplyPathMigration(pathMigrationTransformer(t, e), "Migrate paths", func(r PathMigrationResult) { result = &r }); err != nil {
			t.Fatal(err)
		}
		for e.localWork != nil {
			app.runScheduled(t)
		}
		if result == nil || result.Err != nil || result.Report.Changed["/obj/old"] != size || result.Report.Tiles != size {
			t.Fatalf("size %d result = %#v", size, result)
		}
		after, err := e.SaveSnapshot(context.Background())
		if err != nil || after.Revision != before.Revision+1 {
			t.Fatalf("size %d: revision %d -> %d err %v", size, before.Revision, after.Revision, err)
		}
		for n, tile := range after.Tiles {
			migrated, original := tile.State.Prefabs[3], before.Tiles[n].State.Prefabs[3]
			if migrated.Path != "/obj/foo" || migrated.StableID != original.StableID || migrated.Vars["legacy"] != `"kept"` || migrated.Vars["dir"] != "4" {
				t.Fatalf("size %d tile %d = %#v", size, n, migrated)
			}
			if !reflect.DeepEqual(tile.State.Prefabs[:3], before.Tiles[n].State.Prefabs[:3]) {
				t.Fatal("unrelated content changed")
			}
		}
		display := e.dmm.Tiles[size-1].Instances()[3].Prefab()
		if display.Path() != "/obj/foo" || !display.Vars().HasParent() {
			t.Fatal("display was not linked to the environment type")
		}
		app.commands.UndoV("test")
		for e.localWork != nil {
			app.runScheduled(t)
		}
		undone, err := e.SaveSnapshot(context.Background())
		if err != nil || !reflect.DeepEqual(before.Tiles, undone.Tiles) {
			t.Fatalf("size %d: undo did not restore the exact source: %v", size, err)
		}
		app.commands.RedoV("test")
		for e.localWork != nil {
			app.runScheduled(t)
		}
		redone, err := e.SaveSnapshot(context.Background())
		if err != nil || !reflect.DeepEqual(after.Tiles, redone.Tiles) {
			t.Fatalf("size %d: redo did not restore the migration: %v", size, err)
		}
	}
}

func TestPathMigrationRefusesWhileWorkIsInProgress(t *testing.T) {
	e, app := pathMigrationEditor(t, 2)
	transformer := pathMigrationTransformer(t, e)
	if err := e.ApplyPathMigration(transformer, "first", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.ApplyPathMigration(transformer, "second", nil); err == nil {
		t.Fatal("second migration started while the first owned the document")
	}
	for e.localWork != nil {
		app.runScheduled(t)
	}
}

func TestPathMigrationSubmitsOneSessionOperation(t *testing.T) {
	e, _ := pathMigrationEditor(t, 3)
	transport := newEditorNetworkTransport()
	network, err := client.NewNetworkExecutor(transport, e.authoritative, e.actorID, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	before := e.authoritative
	if err := e.ApplyPathMigration(pathMigrationTransformer(t, e), "Migrate paths", nil); err != nil {
		t.Fatal(err)
	}
	decoded, err := protocol.DecodeClient(mustEditorJSON(t, transport.next(t)))
	if err != nil {
		t.Fatal(err)
	}
	operation := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	if len(operation.Changes) != 3 {
		t.Fatalf("operation has %d changes", len(operation.Changes))
	}
	for n, change := range operation.Changes {
		migrated := change.After.Prefabs[3]
		if migrated.Path != "/obj/foo" || migrated.StableID != before.Tiles[n].State.Prefabs[3].StableID || migrated.Vars["legacy"] != `"kept"` {
			t.Fatalf("change %d = %#v", n, migrated)
		}
	}
	if e.dmm.Tiles[2].Instances()[3].Prefab().Path() != "/obj/foo" || len(e.pendingChanges) != 0 {
		t.Fatal("display or capture state was not settled")
	}
}

func TestOversizedSessionMigrationIsRefusedBeforeMutation(t *testing.T) {
	e, _ := pathMigrationEditor(t, protocol.MaxOperationChanges+1)
	network, err := client.NewNetworkExecutor(newEditorNetworkTransport(), e.authoritative, e.actorID, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	err = e.ApplyPathMigration(pathMigrationTransformer(t, e), "Migrate paths", nil)
	if err == nil || !strings.Contains(err.Error(), "bulk edits") {
		t.Fatalf("err = %v", err)
	}
	if len(e.pendingChanges) != 0 || !e.CanStartMapEdit() || e.dmm.Tiles[0].Instances()[3].Prefab().Path() != "/obj/old" || len(network.Conflicts()) != 0 {
		t.Fatal("refused migration changed editor state")
	}
}

func TestPathMigrationTouchesOnlyPlannedTiles(t *testing.T) {
	tiles := map[model.Coord]model.TileState{
		{X: 2, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{{Path: "/obj/old"}}},
		{X: 1, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{{Path: "/obj/foo"}}},
		{X: 1, Y: 1, Z: 2}: {Prefabs: []model.PrefabState{{Path: "/obj/old"}}},
	}
	e, _ := pathMigrationEditor(t, 1)
	got := touchedCoords(tiles, pathMigrationTransformer(t, e))
	if !reflect.DeepEqual(got, []model.Coord{{X: 2, Y: 1, Z: 1}, {X: 1, Y: 1, Z: 2}}) {
		t.Fatalf("coords = %v", got)
	}
}
