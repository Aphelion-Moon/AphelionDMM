package wsmap

import (
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
	"testing"
)

func TestAcceptedPasteIncludesInterveningDisjointRevision(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	network, transport, document := selectionNetwork(t, ws)
	e := ws.Map().Editor()
	before := document.Snapshot()
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}})
	ws.Map().CanvasState().SetMousePosition(32, 0, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	if !tools.Selected().(*tools.ToolGrab).ConfirmPlacement() {
		t.Fatal("paste did not confirm")
	}
	paste := transport.next(t)
	remoteTile := before.Tiles[len(before.Tiles)-1]
	after := model.CloneTileState(remoteTile.State)
	if after.Prefabs[0].Vars == nil {
		after.Prefabs[0].Vars = make(map[string]string)
	}
	after.Prefabs[0].Vars["remote_note"] = `"preserved disjoint edit"`
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	remote := model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: before.DocumentID, ActorID: actor, OperationID: id, BaseRevision: before.Revision, BaseMapHash: resizeHash(t, before), EnvironmentHash: before.EnvironmentHash, Kind: model.OperationKindTileChange, Changes: []model.TileChange{{Coord: remoteTile.Coord, Before: remoteTile.State, After: after}}}
	acceptSelection(t, network, document, remote)
	acceptSelection(t, network, document, paste)
	runSelectionJob(t, app)
	settlePastePreview(t, ws, app)
	current := resizeSnapshot(t, e)
	if current.Revision != before.Revision+2 || resizeHash(t, current) != resizeHash(t, document.Snapshot()) {
		t.Fatal("paste acceptance relabelled an incomplete snapshot")
	}
	assertLargeRegionMatches(t, ws, snapshotStateIndex(document.Snapshot()), 1, e.Dmm().MaxX, 1, e.Dmm().MaxY)
	app.commands.UndoV(e.Dmm().Path.Absolute)
	acceptSelection(t, network, document, transport.next(t))
	runSelectionJob(t, app)
	current = resizeSnapshot(t, e)
	if !snapshotStateIndex(current)[remoteTile.Coord].Equal(after) {
		t.Fatal("paste inverse reverted another actor's edit")
	}
	if !saveForTest(t, ws, app.jobs) {
		t.Fatal("coherent accepted state could not be saved")
	}
}
