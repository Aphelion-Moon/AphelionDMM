package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func wholeLevelNetwork(t testing.TB) (*NetworkExecutor, model.OperationID) {
	t.Helper()
	snapshot := indexedFixture(1)
	snapshot.MaxX = 256
	snapshot.MaxY = 256
	snapshot.Tiles = nil
	snapshot.Revision = 1
	id := model.OperationID("01890f3e-7b5c-7abc-8def-0123456789bb")
	actor := model.ActorID("01890f3e-7b5c-7abc-8def-0123456789bc")
	accepted := model.AcceptedOperation{Operation: model.Operation{OperationID: id, ActorID: actor, Kind: model.OperationKindTileChange}, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	for i := 0; i < 65536; i++ {
		coord := model.Coord{X: i%256 + 1, Y: i/256 + 1, Z: 1}
		state := model.TileState{Prefabs: []model.PrefabState{{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", i+1)), Path: "/obj/unknown", Vars: map[string]string{"raw": "list(1, /obj/missing)"}}}}
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: coord, State: state})
		accepted.Changes = append(accepted.Changes, model.TileChange{Coord: coord, After: state})
	}
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, actor, "session")
	if err != nil {
		t.Fatal(err)
	}
	network.accepted[id] = accepted
	return network, id
}

func TestWholeLevelInverseOwnershipAndLateConflict(t *testing.T) {
	network, id := wholeLevelNetwork(t)
	inverse, err := network.BuildInverse(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(inverse.Changes) != 65536 {
		t.Fatal("inverse lost tiles")
	}
	inverse.Changes[0].Before.Prefabs[0].Vars["raw"] = "changed caller"
	if network.accepted[id].Changes[0].After.Prefabs[0].Vars["raw"] == "changed caller" {
		t.Fatal("inverse aliases history")
	}
	// Advance authority through the same boundary as a remote edit, so all
	// maintained indexes and the verified hash describe the new revision.
	snapshot, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	last := snapshot.Tiles[len(snapshot.Tiles)-1]
	after := model.CloneTileState(last.State)
	after.Prefabs[0].Vars["raw"] = "conflict"
	operation := projectionOperation(t, snapshot, 1)
	operation.Changes = []model.TileChange{{Coord: last.Coord, Before: last.State, After: after}}
	accepted := model.AcceptedOperation{Operation: operation, Revision: snapshot.Revision + 1, AcceptedAt: time.Unix(2, 0)}
	updated := snapshotWithOperation(t, snapshot, accepted)
	hash, err := updated.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})); err != nil {
		t.Fatal(err)
	}
	if _, err := network.BuildInverse(context.Background(), id); err == nil {
		t.Fatal("late inverse conflict accepted")
	}
	// Snapshot recovery can restore the original values at a later revision.
	snapshot.Revision = updated.Revision + 1
	if err := network.ReplaceAcknowledgedSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Tiles[len(snapshot.Tiles)-1].State.Prefabs[0].Vars["raw"] = "caller mutation"
	if _, err := network.BuildInverse(context.Background(), id); err != nil {
		t.Fatalf("recovered authority remained conflicted or aliases caller: %v", err)
	}
}

func TestWholeLevelInversePreparationAllowsNewerAuthority(t *testing.T) {
	network, id := wholeLevelNetwork(t)
	ctx := &pausedRebuildContext{Context: context.Background(), entered: make(chan struct{}), release: make(chan struct{})}
	defer close(ctx.release)
	prepared := make(chan model.Operation, 1)
	failed := make(chan error, 1)
	go func() {
		inverse, err := network.BuildInverse(ctx, id)
		prepared <- inverse
		failed <- err
	}()
	select {
	case <-ctx.entered:
	case <-time.After(time.Second):
		t.Fatal("inverse did not reach materialization")
	}
	if !network.mutex.TryLock() {
		t.Fatal("inverse holds the authority lock while materializing tile values")
	}
	network.mutex.Unlock()
	snapshot, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	last := snapshot.Tiles[len(snapshot.Tiles)-1]
	after := model.CloneTileState(last.State)
	after.Prefabs[0].Vars["raw"] = "newer edit"
	operation := projectionOperation(t, snapshot, 1)
	operation.Changes = []model.TileChange{{Coord: last.Coord, Before: last.State, After: after}}
	accepted := model.AcceptedOperation{Operation: operation, Revision: snapshot.Revision + 1, AcceptedAt: time.Unix(2, 0)}
	updated := snapshotWithOperation(t, snapshot, accepted)
	hash, err := updated.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})); err != nil {
		t.Fatal(err)
	}
	ctx.release <- struct{}{}
	inverse := <-prepared
	if err := <-failed; err != nil {
		t.Fatal(err)
	}
	baseHash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if inverse.BaseRevision != snapshot.Revision || inverse.BaseMapHash != baseHash || len(inverse.Changes) != len(snapshot.Tiles) || !inverse.Changes[len(inverse.Changes)-1].Before.Equal(last.State) {
		t.Fatal("inverse mixed revisions during preparation")
	}
	executeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := network.Execute(executeContext, inverse); err == nil || !strings.Contains(err.Error(), "precondition") {
		t.Fatalf("stale inverse was not rejected by value preconditions: %v", err)
	}
	if len(network.transport.(*fakeTransport).sent) != 0 {
		t.Fatal("stale inverse reached transport")
	}
	current, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	currentHash, err := current.Hash()
	if err != nil || currentHash != hash || current.Revision != updated.Revision {
		t.Fatal("failed inverse changed newer authority", err)
	}
	inverse.Changes[0].Before.Prefabs[0].Vars["raw"] = "caller mutation"
	if network.accepted[id].Changes[0].After.Prefabs[0].Vars["raw"] == "caller mutation" {
		t.Fatal("prepared inverse aliases retained history")
	}
}

// Keep the operation small while the surrounding document is large. This
// isolates preparation cost from the number of tiles actually being undone.
func BenchmarkSingleTileNetworkPreparationOnLargeMap(b *testing.B) {
	network, id := wholeLevelNetwork(b)
	target := network.accepted[id]
	target.Changes = target.Changes[len(target.Changes)-1:]
	network.accepted[id] = target
	draft := model.CloneOperation(target.Operation)
	draft.Changes[0].After = model.TileState{}
	network.retainConflictLocked(Conflict{OperationID: id, Draft: draft})
	b.Run("inverse", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := network.BuildInverse(context.Background(), id); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("conflict_rebuild", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := network.BuildConflictRebuild(context.Background(), id); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkWholeLevelNetworkInverse(b *testing.B) {
	network, id := wholeLevelNetwork(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := network.BuildInverse(context.Background(), id); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLargeMapSuspension(b *testing.B) {
	network, _ := wholeLevelNetwork(b)
	transport := network.transport
	cause := errors.New("benchmark disconnect")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		network.Suspend(cause)
		if err := network.Resume(transport); err != nil {
			b.Fatal(err)
		}
	}
}
