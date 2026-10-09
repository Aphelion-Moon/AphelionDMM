package editor

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/maplint"
)

func lintFixEditor(t *testing.T) (*Editor, *editorTestApp) {
	t.Helper()
	activateLint(t)
	e := lintEditor(t, lintPrefab("/obj/structure/table"), lintPrefab("/obj/structure/table/wood"), lintPrefab("/obj/structure/window", "dir", "4"), lintPrefab("/obj/structure/window", "dir", "4"))
	app := e.app.(*editorTestApp)
	app.runLater = make(chan func(), 32)
	return e, app
}

func countPaths(tile model.TileState, path string) int {
	n := 0
	for _, p := range tile.Prefabs {
		if p.Path == path {
			n++
		}
	}
	return n
}

func TestLintScanPreviewsAutomaticFixes(t *testing.T) {
	e, _ := lintFixEditor(t)
	run, err := e.PrepareLintScan(1)
	if err != nil {
		t.Fatal(err)
	}
	result, err := run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The findings list is truncated, but the fix preview covers the whole map.
	if !result.Truncated || result.FixTiles != 1 || result.Fixable == 0 {
		t.Fatalf("preview = %+v", result)
	}
	kinds := map[maplint.FixKind]int{}
	for _, c := range result.Fixes {
		kinds[c.Kind] += c.Count
	}
	if kinds[maplint.FixRemoveDuplicate] != 1 || kinds[maplint.FixRemoveSuperseded] != 1 {
		t.Fatalf("fix counts = %v", kinds)
	}
}

func TestApplyLintFixesIsOneUndoableLocalRevision(t *testing.T) {
	e, app := lintFixEditor(t)
	before, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var result *LintFixResult
	if err := e.ApplyLintFixes(maplint.AllFixes, func(r LintFixResult) { result = &r }); err != nil {
		t.Fatal(err)
	}
	for e.localWork != nil {
		app.runScheduled(t)
	}
	// Duplicate window, superseded table, and the fixture's /obj/foo dir = 2,
	// which equals its default (editor audit).
	if result == nil || result.Err != nil || result.Tiles != 1 || result.Fixes != 3 {
		t.Fatalf("result = %+v", result)
	}
	after, err := e.SaveSnapshot(context.Background())
	if err != nil || after.Revision != before.Revision+1 {
		t.Fatalf("revision %d -> %d err %v", before.Revision, after.Revision, err)
	}
	tile := after.Tiles[0].State
	if countPaths(tile, "/obj/structure/table") != 0 || countPaths(tile, "/obj/structure/table/wood") != 1 || countPaths(tile, "/obj/structure/window") != 1 {
		t.Fatalf("fixed tile = %+v", tile.Prefabs)
	}
	// Kept instances keep their identity.
	for _, p := range tile.Prefabs {
		found := false
		for _, q := range before.Tiles[0].State.Prefabs {
			found = found || (q.StableID == p.StableID && q.Path == p.Path)
		}
		if !found {
			t.Fatalf("instance %s lost its stable ID", p.Path)
		}
	}
	app.commands.UndoV("test")
	for e.localWork != nil {
		app.runScheduled(t)
	}
	undone, err := e.SaveSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(before.Tiles, undone.Tiles) {
		t.Fatalf("undo did not restore the map: %v", err)
	}
}

func TestApplyLintFixesHonoursDisabledKindsAndNoOps(t *testing.T) {
	e, app := lintFixEditor(t)
	before, _ := e.SaveSnapshot(context.Background())
	var result *LintFixResult
	kinds := maplint.AllFixes.Without(maplint.FixRemoveDuplicate).Without(maplint.FixRemoveSuperseded).Without(maplint.FixStripRedundant)
	if err := e.ApplyLintFixes(kinds, func(r LintFixResult) { result = &r }); err != nil {
		t.Fatal(err)
	}
	for e.localWork != nil {
		app.runScheduled(t)
	}
	after, _ := e.SaveSnapshot(context.Background())
	if result == nil || result.Tiles != 0 || after.Revision != before.Revision {
		t.Fatalf("disabled kinds still edited: result %+v revision %d -> %d", result, before.Revision, after.Revision)
	}
}

func TestApplyLintFixesSubmitsOneSessionOperation(t *testing.T) {
	e, _ := lintFixEditor(t)
	transport := newEditorNetworkTransport()
	network, err := client.NewNetworkExecutor(transport, e.authoritative, e.actorID, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	var result *LintFixResult
	if err := e.ApplyLintFixes(maplint.AllFixes, func(r LintFixResult) { result = &r }); err != nil {
		t.Fatal(err)
	}
	decoded, err := protocol.DecodeClient(mustEditorJSON(t, transport.next(t)))
	if err != nil {
		t.Fatal(err)
	}
	operation := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	if len(operation.Changes) != 1 || countPaths(operation.Changes[0].After, "/obj/structure/table") != 0 || countPaths(operation.Changes[0].After, "/obj/structure/window") != 1 {
		t.Fatalf("operation = %+v", operation.Changes)
	}
	if result == nil || result.Tiles != 1 || len(e.pendingChanges) != 0 {
		t.Fatalf("result %+v pending %d", result, len(e.pendingChanges))
	}
}
