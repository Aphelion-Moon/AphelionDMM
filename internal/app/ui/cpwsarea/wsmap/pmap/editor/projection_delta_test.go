package editor

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func deltaEditor(t *testing.T) (*Editor, *client.NetworkExecutor, *engine.Document, *editorNetworkTransport) {
	t.Helper()
	e := selectionEditor(t)
	for z := 1; z <= 2; z++ {
		for x := 1; x <= 3; x++ {
			if x == 1 && z == 1 {
				continue
			}
			tile := &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: z}}
			tile.InstancesSet(e.dmm.Tiles[0].Instances().Prefabs())
			e.dmm.Tiles = append(e.dmm.Tiles, tile)
		}
	}
	e.dmm.MaxX, e.dmm.MaxZ = 3, 2
	e.initializeCollaboration()
	transport := newEditorNetworkTransport()
	network, err := client.NewNetworkExecutor(transport, e.authoritative, e.actorID, "delta-session")
	if err != nil {
		t.Fatal(err)
	}
	document, err := engine.NewDocument(e.authoritative)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	e.ProcessCollaborationUpdates()
	return e, network, document, transport
}

func acceptDelta(t *testing.T, network *client.NetworkExecutor, document *engine.Document, coord model.Coord, path string) {
	t.Helper()
	before := document.Tile(coord)
	after := model.CloneTileState(before)
	after.Prefabs[0].Path = path
	metadata, err := document.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := document.Snapshot()
	id, _ := model.NewOperationID()
	actor, _ := model.NewActorID()
	op := model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: metadata.DocumentID, ActorID: actor, OperationID: id, BaseRevision: metadata.Revision, BaseMapHash: metadata.MapHash, EnvironmentHash: snapshot.EnvironmentHash, Kind: model.OperationKindTileChange, Changes: []model.TileChange{{Coord: coord, Before: before, After: after}}}
	accepted, err := document.Apply(op, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err = document.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: string(id), SessionID: "delta-session", Type: protocol.ServerOperationAccepted, Payload: mustEditorJSON(t, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: metadata.MapHash})}); err != nil {
		t.Fatal(err)
	}
}

func TestNetworkDeltaKeepsUntouchedDisplayAndCoalescesLevels(t *testing.T) {
	e, network, document, _ := deltaEditor(t)
	untouched := util.Point{X: 3, Y: 1, Z: 2}
	instance := e.dmm.GetTile(untouched).Instances()[2]
	compatibility := e.pMap.Snapshot().Initial().GetTile(untouched).Instances()[2]
	old, err := network.CaptureProjection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	acceptDelta(t, network, document, model.Coord{X: 1, Y: 1, Z: 1}, "/area/first")
	acceptDelta(t, network, document, model.Coord{X: 2, Y: 1, Z: 2}, "/area/second")
	e.ProcessCollaborationUpdates()
	if e.authoritative.Revision != 2 {
		t.Fatalf("display revision = %d", e.authoritative.Revision)
	}
	if e.dmm.GetTile(untouched).Instances()[2] != instance || e.pMap.Snapshot().Initial().GetTile(untouched).Instances()[2] != compatibility {
		t.Fatal("sparse publication rebuilt untouched display or compatibility instances")
	}
	for _, c := range []model.Coord{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 2}} {
		state, _ := old.AcceptedTile(c)
		if state.Prefabs[0].Path != "/area/foo" {
			t.Fatal("publication mutated a worker capture")
		}
		if !e.authoritativeTiles[c].Equal(document.Tile(c)) {
			t.Fatal("coalesced publication lost authority")
		}
	}
	borders := areaBorderSet(e.AreasZones())
	e.updateAreasZones()
	if !reflect.DeepEqual(borders, areaBorderSet(e.AreasZones())) {
		t.Fatal("sparse cross-level areas differ from full rebuild")
	}
}

func TestNetworkDeltaWaitsForActiveGesture(t *testing.T) {
	e, network, document, _ := deltaEditor(t)
	coord := model.Coord{X: 1, Y: 1, Z: 1}
	e.pendingChanges[coord] = model.CloneTileState(e.authoritativeTiles[coord])
	acceptDelta(t, network, document, coord, "/area/remote")
	e.ProcessCollaborationUpdates()
	if e.authoritative.Revision != 0 {
		t.Fatal("publication crossed the gesture fence")
	}
	clear(e.pendingChanges)
	e.ProcessCollaborationUpdates()
	if e.authoritative.Revision != 1 || e.authoritativeTiles[coord].Prefabs[0].Path != "/area/remote" {
		t.Fatal("queued publication was lost")
	}
}

type snapshotForbiddenNetwork struct {
	*client.NetworkExecutor
	reads int
}

func (network *snapshotForbiddenNetwork) Snapshot(context.Context) (model.Snapshot, error) {
	network.reads++
	return model.Snapshot{}, fmt.Errorf("unexpected whole-map snapshot")
}

func TestNetworkCommitReadsOnlyOperationMetadata(t *testing.T) {
	e, network, _, transport := deltaEditor(t)
	e.app.(*editorTestApp).runLater = make(chan func(), 8)
	wrapped := &snapshotForbiddenNetwork{NetworkExecutor: network}
	e.executor = wrapped
	t.Cleanup(func() { network.Suspend(fmt.Errorf("test finished")) })
	instance := e.dmm.Tiles[0].Instances()[2]
	e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "4")))
	e.CommitOperation("Sparse Network Change")
	transport.next(t)
	if wrapped.reads != 0 || !network.HasUnacknowledgedOperations() {
		t.Fatalf("commit snapshot reads=%d pending=%t", wrapped.reads, network.HasUnacknowledgedOperations())
	}
}

func TestNetworkSpeculationDoesNotMaterializeMissingAuthority(t *testing.T) {
	e, old, _, _ := deltaEditor(t)
	capture, err := old.CaptureProjection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := capture.AcceptedSnapshot()
	missing := snapshot.Tiles[len(snapshot.Tiles)-1].Coord
	snapshot.Tiles = snapshot.Tiles[:len(snapshot.Tiles)-1]
	transport := newEditorNetworkTransport()
	network, err := client.NewNetworkExecutor(transport, snapshot, e.actorID, "delta-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	e.ProcessCollaborationUpdates()
	after := model.CloneTileState(snapshot.Tiles[0].State)
	for i := range after.Prefabs {
		after.Prefabs[i].StableID, _ = model.NewStableID()
	}
	op, err := placementWireOperation(snapshot, e.actorID, []model.TileChange{{Coord: missing, After: after}})
	if err != nil {
		t.Fatal(err)
	}
	if err := network.ExecuteAsync(context.Background(), op, func(model.AcceptedOperation, error) {}); err != nil {
		t.Fatal(err)
	}
	transport.next(t)
	e.ProcessCollaborationUpdates()
	point := util.Point{X: missing.X, Y: missing.Y, Z: missing.Z}
	if len(e.dmm.GetTile(point).Instances()) == 0 {
		t.Fatal("insertion was not speculatively displayed")
	}
	if _, exists := e.authoritativeTiles[missing]; exists {
		t.Fatal("speculation materialized missing authority")
	}
	expectedHash, _ := snapshot.Hash()
	actualHash, _ := e.authoritative.Hash()
	if actualHash != expectedHash {
		t.Fatal("speculative display altered acknowledged hash")
	}
	network.Suspend(fmt.Errorf("reject speculative insertion"))
	e.ProcessCollaborationUpdates()
	if len(e.dmm.GetTile(point).Instances()) != 0 {
		t.Fatal("rejected insertion remained displayed")
	}
	if _, exists := e.authoritativeTiles[missing]; exists {
		t.Fatal("rejection materialized missing authority")
	}
}

func TestNetworkGrabCommitUsesSelectedTileBudget(t *testing.T) {
	e, network, document, transport := deltaEditor(t)
	application := e.app.(*editorTestApp)
	application.runLater = make(chan func(), 8)
	e.workBudget = resources.NewFixedBudget(5 << 20)
	area, turf := dmmap.BaseArea, dmmap.BaseTurf
	dmmap.BaseArea, dmmap.BaseTurf = e.dmm.Tiles[0].Instances()[0].Prefab(), e.dmm.Tiles[0].Instances()[1].Prefab()
	t.Cleanup(func() {
		network.Suspend(fmt.Errorf("test finished"))
		e.CancelSelectionMovePreview()
		dmmap.BaseArea, dmmap.BaseTurf = area, turf
	})
	move, err := e.BeginSelectionMovePreview(editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for e.selectionMovePreview.preparing && time.Now().Before(deadline) {
		e.ProcessPasteWork()
		time.Sleep(time.Millisecond)
	}
	if e.selectionMovePreview.preparing || e.selectionMovePreview.err != nil {
		t.Fatalf("prepare move: %v", e.selectionMovePreview.err)
	}
	if _, err := e.PreviewSelectionMovePreview(move, util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	if err := e.FinishSelectionMovePreview(move, false); err != nil {
		t.Fatal(err)
	}
	application.runScheduled(t)
	envelope := transport.next(t)
	decoded, err := protocol.DecodeClient(mustEditorJSON(t, envelope))
	if err != nil {
		t.Fatal(err)
	}
	operation := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	accepted, err := document.Apply(operation, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := document.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "grab-accepted", SessionID: "delta-session", Type: protocol.ServerOperationAccepted, Payload: mustEditorJSON(t, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: metadata.MapHash})}); err != nil {
		t.Fatal(err)
	}
	application.runScheduled(t)
	e.ProcessCollaborationUpdates()
	if e.workBudget.Used() != 0 || e.authoritative.Revision != 1 {
		t.Fatalf("accepted move retained work or missed display: bytes=%d revision=%d", e.workBudget.Used(), e.authoritative.Revision)
	}
}
