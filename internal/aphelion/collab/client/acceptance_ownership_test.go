package client

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestAcceptedProjectionKeepsPublicValuesDetached(t *testing.T) {
	snapshot := indexedFixture(3)
	projection := NewProjection(snapshot)
	pending := projectionOperation(t, snapshot, 2)
	projection.Pending = []model.Operation{pending}
	operation := projectionOperation(t, snapshot, 1)
	accepted := model.AcceptedOperation{Operation: operation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	after := snapshotWithOperation(t, snapshot, accepted)
	hash, err := after.Hash()
	if err != nil {
		t.Fatal(err)
	}
	next, err := projection.Accept(accepted, hash)
	if err != nil {
		t.Fatal(err)
	}
	projection.Acknowledged.Tiles[2].State.Prefabs[0].Vars["dir"] = "caller"
	projection.Pending[0].Changes[0].After.Prefabs[0].Vars["dir"] = "caller"
	accepted.Changes[0].After.Prefabs[0].Vars["dir"] = "caller"
	if !reflect.DeepEqual(next.Acknowledged, after) || next.Pending[0].Changes[0].After.Prefabs[0].Vars["dir"] == "caller" {
		t.Fatal("public accepted projection aliases input")
	}
	next.Acknowledged.Tiles[1].State.Prefabs[0].Vars["dir"] = "returned"
	if snapshot.Tiles[1].State.Prefabs[0].Vars["dir"] == "returned" {
		t.Fatal("returned snapshot aliases original")
	}
}

func TestNetworkCapturesStayPinnedAcrossSuccessiveAcceptedEdits(t *testing.T) {
	snapshot := indexedFixture(3)
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, mustActorID(t), "session")
	if err != nil {
		t.Fatal(err)
	}
	var captures []ProjectionCapture
	var expected []model.Snapshot
	for step := 0; step < 6; step++ {
		capture, err := network.CaptureProjection(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		captures = append(captures, capture)
		expected = append(expected, model.CloneSnapshot(snapshot))
		operation := projectionOperation(t, snapshot, step%3+1)
		operation.Changes[0].After.Prefabs[0].Vars["dir"] = fmt.Sprint(step + 10)
		accepted := model.AcceptedOperation{Operation: operation, Revision: snapshot.Revision + 1, AcceptedAt: time.Unix(int64(step+1), 0)}
		snapshot = snapshotWithOperation(t, snapshot, accepted)
		hash, err := snapshot.Hash()
		if err != nil {
			t.Fatal(err)
		}
		if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})); err != nil {
			t.Fatal(err)
		}
	}
	for i, capture := range captures {
		got := capture.AcceptedSnapshot()
		if !reflect.DeepEqual(got, expected[i]) {
			t.Fatalf("capture %d changed after later edits", i)
		}
		for _, tile := range got.Tiles {
			tile.State.Prefabs[0].Vars["dir"] = "external"
		}
	}
	latest, err := network.Snapshot(context.Background())
	if err != nil || !reflect.DeepEqual(latest, snapshot) {
		t.Fatal("external capture mutation changed current authority", err)
	}
}

func BenchmarkAcceptedReconciliation(b *testing.B) {
	network, _ := wholeLevelNetwork(b)
	base := network.projection
	tile := base.Acknowledged.Tiles[len(base.Acknowledged.Tiles)-1]
	after := model.CloneTileState(tile.State)
	after.Prefabs[0].Vars["raw"] = "remote"
	operation := model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: base.Acknowledged.DocumentID, EnvironmentHash: base.Acknowledged.EnvironmentHash,
		OperationID: "01890f3e-7b5c-7abc-8def-0123456789bd", ActorID: network.actor, BaseRevision: base.Acknowledged.Revision, BaseMapHash: base.acknowledgedHash, Kind: model.OperationKindTileChange,
		Changes: []model.TileChange{{Coord: tile.Coord, Before: tile.State, After: after}}}
	accepted := model.AcceptedOperation{Operation: operation, Revision: base.Acknowledged.Revision + 1, AcceptedAt: time.Unix(2, 0)}
	next, err := applyAcceptedSnapshot(base.Acknowledged, accepted)
	if err != nil {
		b.Fatal(err)
	}
	hash, err := next.Hash()
	if err != nil {
		b.Fatal(err)
	}
	for _, count := range []int{0, 9216} {
		b.Run(fmt.Sprintf("pending_tiles_%d", count), func(b *testing.B) {
			projection := base
			if count != 0 {
				pending := operation
				pending.OperationID = "01890f3e-7b5c-7abc-8def-0123456789be"
				pending.Changes = nil
				for _, tile := range base.Acknowledged.Tiles[:count] {
					after := model.CloneTileState(tile.State)
					after.Prefabs[0].Vars["raw"] = "pending"
					pending.Changes = append(pending.Changes, model.TileChange{Coord: tile.Coord, Before: tile.State, After: after})
				}
				projection.Pending = []model.Operation{pending}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := projection.acceptWithVerifiedCurrent(accepted, hash, nil)
				if err != nil || got.Acknowledged.Revision != accepted.Revision || len(got.Pending) != len(projection.Pending) {
					b.Fatal("invalid acceptance", err)
				}
			}
		})
	}
}
