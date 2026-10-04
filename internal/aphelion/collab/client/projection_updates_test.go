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
