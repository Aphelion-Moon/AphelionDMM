package client

import (
	"context"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestProjectionUpdatesCoalesceAuthorityAndRemovedSpeculation(t *testing.T) {
	network := indexedNetwork(t, 10)
	updates := network.ProjectionChanges()
	if initial := <-updates; !initial.Full() {
		t.Fatal("initial publication must install a full baseline")
	}
	for x := 1; x <= 2; x++ {
		operation := indexedNetworkOperation(network, x)
		accepted := model.AcceptedOperation{Operation: operation, Revision: model.Revision(x), AcceptedAt: time.Unix(int64(x), 0)}
		next, err := applyAcceptedSnapshot(network.projection.Acknowledged, accepted)
		if err != nil {
			t.Fatal(err)
		}
		hash, _ := next.Hash()
		if err := network.acceptProjectionLocked(accepted, hash); err != nil {
			t.Fatal(err)
		}
		network.publishLocked(operation.Changes...)
	}
	coalesced := <-updates
	if coalesced.Full() || len(coalesced.ChangedCoords()) != 2 || coalesced.Capture().BaseRevision() != 2 {
		t.Fatal("coalescing lost a disjoint accepted edit")
	}
	operation := indexedNetworkOperation(network, 3)
	if err := network.submitProjectionLocked(operation); err != nil {
		t.Fatal(err)
	}
	network.publishLocked()
	if _, err := network.rejectProjectionLocked(protocol.OperationRejectedPayload{OperationID: operation.OperationID, Revision: operation.BaseRevision, MapHash: operation.BaseMapHash}); err != nil {
		t.Fatal(err)
	}
	network.publishLocked()
	rollback := <-updates
	if rollback.Full() || len(rollback.ChangedCoords()) != 1 || rollback.ChangedCoords()[0] != operation.Changes[0].Coord {
		t.Fatal("removed speculation did not remain dirty")
	}
	visible, _ := rollback.Capture().VisibleTile(operation.Changes[0].Coord)
	if !visible.Equal(operation.Changes[0].Before) {
		t.Fatal("rejection publication retained speculative content")
	}
	coords := rollback.ChangedCoords()
	coords[0] = model.Coord{}
	if rollback.ChangedCoords()[0] != operation.Changes[0].Coord {
		t.Fatal("caller mutated dirty publication")
	}
	legacy := network.ProjectionUpdates()
	public := <-legacy
	public.Acknowledged.Tiles[0].State.Prefabs[0].Vars["dir"] = "99"
	current, _ := network.Snapshot(context.Background())
	if current.Tiles[0].State.Prefabs[0].Vars["dir"] != "4" {
		t.Fatal("legacy publication aliases authority")
	}
}

func TestImmutableProjectionPublicationDoesNotCopyUntouchedPayloads(t *testing.T) {
	var small float64
	for _, cells := range []int{100, 10000} {
		network := indexedNetwork(t, cells)
		<-network.ProjectionChanges()
		allocs := testing.AllocsPerRun(5, func() { network.publishLocked(); <-network.ProjectionChanges() })
		if cells == 100 {
			small = allocs
		} else if allocs > small+4 {
			t.Fatalf("publication allocations grew with map payload: %.0f -> %.0f", small, allocs)
		}
	}
}

func TestLegacyProjectionGetterDoesNotRefillDrainedChannel(t *testing.T) {
	network := indexedNetwork(t, 10)
	updates := network.ProjectionUpdates()
	<-updates
	if network.ProjectionUpdates() != updates {
		t.Fatal("getter replaced the legacy channel")
	}
	select {
	case <-updates:
		t.Fatal("reading the legacy channel generated another whole-map publication")
	default:
	}
}

func TestSparseCaptureSurvivesQueuedAdmission(t *testing.T) {
	network := indexedNetwork(t, 10)
	defer network.Terminate(nil)
	completed := make(chan error, 2)
	complete := func(_ model.AcceptedOperation, err error) { completed <- err }
	first := indexedNetworkOperation(network, 1)
	if err := network.ExecuteAsync(context.Background(), first, complete); err != nil {
		t.Fatal(err)
	}
	network.transport.(*fakeTransport).next(t)
	func() {
		network.mutex.Lock()
		defer network.mutex.Unlock()
		second := indexedNetworkOperation(network, 2)
		second.OperationID = "01890f3e-7b5c-7abc-8def-0123456789ae"
		if err := network.ExecuteAsync(context.Background(), second, complete); err != nil {
			t.Fatal(err)
		}
		capture, err := network.CaptureProjection(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		coord := first.Changes[0].Coord
		if !capture.HasPending() {
			t.Fatal("queued admission lost pending metadata")
		}
		accepted, found := capture.AcceptedTile(coord)
		if !found || !accepted.Equal(first.Changes[0].Before) {
			t.Fatal("admission lost acknowledged index")
		}
		visible, found := capture.VisibleTile(coord)
		if !found || !visible.Equal(first.Changes[0].After) {
			t.Fatal("admission lost speculative overlay")
		}
		visible.Prefabs[0].Vars["dir"] = "99"
		again, _ := capture.VisibleTile(coord)
		if !again.Equal(first.Changes[0].After) {
			t.Fatal("caller mutated admitted capture")
		}
	}()
	network.transport.(*fakeTransport).next(t)
	network.Terminate(nil)
	for range 2 {
		select {
		case err := <-completed:
			if err == nil {
				t.Fatal("terminated operation unexpectedly succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("termination did not finish queued admission")
		}
	}
}
