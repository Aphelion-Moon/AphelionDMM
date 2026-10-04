package client

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func indexedNetwork(t testing.TB, cells int) *NetworkExecutor {
	t.Helper()
	snapshot := indexedFixture(cells)
	snapshot.MaxX, snapshot.MaxY = 100, cells/100+1
	for index := range snapshot.Tiles {
		snapshot.Tiles[index].Coord = model.Coord{X: index%100 + 1, Y: index/100 + 1, Z: 1}
	}
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, "01890f3e-7b5c-7abc-8def-0123456789ac", "session")
	if err != nil {
		t.Fatal(err)
	}
	return network
}

func TestIndexedNetworkProjectionMixedTransitionsMatchReference(t *testing.T) {
	network := indexedNetwork(t, 20)
	reference := cloneProjection(network.projection)
	random := rand.New(rand.NewSource(71004))
	for step := 0; step < 80; step++ {
		operation := indexedNetworkOperation(network, random.Intn(20)+1)
		operation.OperationID = model.OperationID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", 100+step))
		operation.Changes[0].After.Prefabs[0].Vars["dir"] = fmt.Sprint(step)
		if step%3 != 0 {
			visible, _ := reference.Visible()
			operation.Changes[0].Before = tileAt(visible, operation.Changes[0].Coord.X)
			if step%5 == 0 {
				operation.Changes[0].After.Prefabs[0].StableID = reference.Acknowledged.Tiles[19].State.Prefabs[0].StableID
			}
			next, wantErr := reference.Submit(operation)
			gotErr := network.submitProjectionLocked(operation)
			if (wantErr == nil) != (gotErr == nil) {
				t.Fatalf("step %d submit: got %v want %v", step, gotErr, wantErr)
			}
			if wantErr == nil {
				reference = next
			}
		} else {
			accepted := model.AcceptedOperation{Operation: operation, Revision: reference.Acknowledged.Revision + 1, AcceptedAt: time.Unix(int64(step), 0)}
			next, err := applyAcceptedSnapshot(reference.Acknowledged, accepted)
			if err != nil {
				t.Fatal(err)
			}
			hash, _ := next.Hash()
			reference, err = reference.Accept(accepted, hash)
			if err != nil {
				t.Fatal(err)
			}
			if err := network.acceptProjectionLocked(accepted, hash); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(network.projection, reference) {
			t.Fatalf("projection diverged at step %d", step)
		}
		network.publishLocked()
		capture, _ := network.CaptureProjection(context.Background())
		gotVisible, _ := capture.VisibleSnapshot()
		wantVisible, _ := reference.Visible()
		if !reflect.DeepEqual(gotVisible, wantVisible) {
			t.Fatalf("cached visible projection diverged at step %d", step)
		}
	}
}

func indexedNetworkOperation(network *NetworkExecutor, x int) model.Operation {
	before := network.projection.Acknowledged.Tiles[x-1].State
	after := model.CloneTileState(before)
	after.Prefabs[0].Vars["dir"] = "4"
	hash, _ := network.projection.Acknowledged.Hash()
	return model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: network.projection.Acknowledged.DocumentID, ActorID: network.actor,
		OperationID: "01890f3e-7b5c-7abc-8def-0123456789ad", BaseRevision: network.projection.Acknowledged.Revision,
		EnvironmentHash: network.projection.Acknowledged.EnvironmentHash, BaseMapHash: hash, Kind: model.OperationKindTileChange,
		Changes: []model.TileChange{{Coord: model.Coord{X: x, Y: 1, Z: 1}, Before: model.CloneTileState(before), After: after}}}
}

func TestIndexedNetworkProjectionMatchesReconciliationAndPinsCaptures(t *testing.T) {
	network := indexedNetwork(t, 10)
	reference := cloneProjection(network.projection)
	pending := indexedNetworkOperation(network, 1)
	if err := network.submitProjectionLocked(pending); err != nil {
		t.Fatal(err)
	}
	var err error
	reference, err = reference.Submit(pending)
	if err != nil {
		t.Fatal(err)
	}
	network.publishLocked()
	capture, err := network.CaptureProjection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pinned, _ := capture.VisibleSnapshot()
	remote := indexedNetworkOperation(network, 2)
	remote.OperationID = "01890f3e-7b5c-7abc-8def-0123456789ae"
	accepted := model.AcceptedOperation{Operation: remote, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	after, err := applyAcceptedSnapshot(reference.Acknowledged, accepted)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := after.Hash()
	if err := network.acceptProjectionLocked(accepted, hash); err != nil {
		t.Fatal(err)
	}
	reference, err = reference.Accept(accepted, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(network.projection, reference) {
		t.Fatal("remote acceptance changed pending intent or authority")
	}
	stillPinned, _ := capture.VisibleSnapshot()
	if !reflect.DeepEqual(stillPinned, pinned) {
		t.Fatal("acceptance mutated an older capture")
	}
	rejection := protocol.OperationRejectedPayload{OperationID: pending.OperationID, Revision: 1, MapHash: hash}
	gotConflict, err := network.rejectProjectionLocked(rejection)
	if err != nil {
		t.Fatal(err)
	}
	reference, wantConflict, err := reference.Reject(rejection)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(network.projection, reference) || !reflect.DeepEqual(gotConflict, wantConflict) {
		t.Fatal("rejection changed conflict or acknowledged semantics")
	}
	// A moved identity cannot be duplicated in an unrelated tile, including
	// while another operation supplies a speculative visible state.
	bad := indexedNetworkOperation(network, 1)
	bad.Changes[0].After.Prefabs[0].StableID = network.projection.Acknowledged.Tiles[1].State.Prefabs[0].StableID
	if err := network.submitProjectionLocked(bad); err == nil {
		t.Fatal("speculation accepted an identity owned by an unchanged tile")
	}
}

func TestNetworkProjectionSubmitRejectAllocationsStaySparse(t *testing.T) {
	var small float64
	for _, cells := range []int{100, 10000} {
		network := indexedNetwork(t, cells)
		operation := indexedNetworkOperation(network, 1)
		rejection := protocol.OperationRejectedPayload{OperationID: operation.OperationID, Revision: operation.BaseRevision, MapHash: operation.BaseMapHash}
		allocs := testing.AllocsPerRun(5, func() {
			if err := network.submitProjectionLocked(operation); err != nil {
				t.Fatal(err)
			}
			if _, err := network.rejectProjectionLocked(rejection); err != nil {
				t.Fatal(err)
			}
		})
		if cells == 100 {
			small = allocs
		} else if allocs > small+32 {
			t.Fatalf("one-tile speculation allocations grew with untouched map: %.0f -> %.0f", small, allocs)
		}
		t.Logf("cells=%d allocations=%.0f", cells, allocs)
	}
}

func TestAcceptedTileCaptureSurvivesSparseAppendAndReturnedMutation(t *testing.T) {
	network := indexedNetwork(t, 10)
	network.publishLocked()
	capture, err := network.CaptureProjection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	operation := indexedNetworkOperation(network, 1)
	operation.Changes[0].Coord.X = 11
	operation.Changes[0].Before = model.TileState{}
	operation.Changes[0].After.Prefabs[0].StableID = "01890f3e-7b5c-7abc-8def-0123456789af"
	accepted := model.AcceptedOperation{Operation: operation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	result, err := applyAcceptedSnapshot(network.projection.Acknowledged, accepted)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := result.Hash()
	readDone := make(chan error, 1)
	go func() {
		for range 50 {
			state, found := capture.AcceptedTile(model.Coord{X: 1, Y: 1, Z: 1})
			if !found || state.Prefabs[0].Vars["dir"] != "2" {
				readDone <- fmt.Errorf("old capture lost its tile")
				return
			}
			state.Prefabs[0].Vars["dir"] = "99"
			if _, found := capture.AcceptedTile(operation.Changes[0].Coord); found {
				readDone <- fmt.Errorf("old capture acquired appended coordinate")
				return
			}
		}
		readDone <- nil
	}()
	if err := network.acceptProjectionLocked(accepted, hash); err != nil {
		t.Fatal(err)
	}
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	network.publishLocked()
	next, _ := network.CaptureProjection(context.Background())
	if state, found := next.AcceptedTile(operation.Changes[0].Coord); !found || !state.Equal(operation.Changes[0].After) {
		t.Fatal("new capture lost appended state")
	}
	if bytes, found := next.EstimatedTileBytes(operation.Changes[0].Coord); !found || bytes == 0 {
		t.Fatal("new capture lost allocation estimate")
	}
}

func BenchmarkNetworkProjectionSubmitReject(b *testing.B) {
	for _, cells := range []int{100, 10000} {
		b.Run(fmt.Sprint(cells), func(b *testing.B) {
			network := indexedNetwork(b, cells)
			operation := indexedNetworkOperation(network, 1)
			rejection := protocol.OperationRejectedPayload{OperationID: operation.OperationID, Revision: operation.BaseRevision, MapHash: operation.BaseMapHash}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := network.submitProjectionLocked(operation); err != nil {
					b.Fatal(err)
				}
				if _, err := network.rejectProjectionLocked(rejection); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkReferenceProjectionSubmitReject(b *testing.B) {
	network := indexedNetwork(b, 10000)
	operation := indexedNetworkOperation(network, 1)
	rejection := protocol.OperationRejectedPayload{OperationID: operation.OperationID, Revision: operation.BaseRevision, MapHash: operation.BaseMapHash}
	projection := network.projection
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pending, err := projection.Submit(operation)
		if err != nil {
			b.Fatal(err)
		}
		projection, _, err = pending.Reject(rejection)
		if err != nil {
			b.Fatal(err)
		}
	}
}
