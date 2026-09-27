package client

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

type pausedRebuildContext struct {
	context.Context
	calls   int
	entered chan struct{}
	release chan struct{}
}

func (ctx *pausedRebuildContext) Err() error {
	ctx.calls++
	if ctx.calls == 2 {
		close(ctx.entered)
		<-ctx.release
	}
	return ctx.Context.Err()
}

func TestConflictRebuildReleasesAuthorityWhileMaterializing(t *testing.T) {
	network, id := recoveryCaptureFixture(t, 256)
	original, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	draft := model.Operation{Changes: []model.TileChange{{Coord: original.Tiles[0].Coord, After: model.CloneTileState(original.Tiles[0].State)}}}
	draft.Changes[0].After.Prefabs[0].Vars["dir"] = "8"
	network.conflicts[0].Draft = draft
	ctx := &pausedRebuildContext{Context: context.Background(), entered: make(chan struct{}), release: make(chan struct{})}
	defer close(ctx.release)
	result := make(chan model.Operation, 1)
	errors := make(chan error, 1)
	go func() {
		op, err := network.BuildConflictRebuild(ctx, id)
		result <- op
		errors <- err
	}()
	select {
	case <-ctx.entered:
	case <-time.After(time.Second):
		t.Fatal("rebuild did not reach materialization")
	}
	if !network.mutex.TryLock() {
		t.Fatal("rebuild holds the authority lock while materializing tile values")
	}
	network.mutex.Unlock()
	replacement := model.CloneSnapshot(original)
	replacement.Revision++
	replacement.Tiles[0].State.Prefabs[0].Vars["dir"] = "4"
	if err := network.ReplaceAcknowledgedSnapshot(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	ctx.release <- struct{}{}
	op := <-result
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	hash, _ := original.Hash()
	if op.BaseRevision != original.Revision || op.BaseMapHash != hash || len(op.Changes) != 1 || !op.Changes[0].Before.Equal(original.Tiles[0].State) || !op.Changes[0].After.Equal(draft.Changes[0].After) {
		t.Fatal("rebuild mixed authority revisions or lost draft intent")
	}
	op.Changes[0].Before.Prefabs[0].Vars["dir"] = "caller mutation"
	op.Changes[0].After.Prefabs[0].Vars["dir"] = "caller mutation"
	if network.conflicts[0].Draft.Changes[0].After.Prefabs[0].Vars["dir"] != "8" || network.authorityTiles[replacement.Tiles[0].Coord].Prefabs[0].Vars["dir"] != "4" {
		t.Fatal("rebuild shares mutable values with executor")
	}
}

func BenchmarkWholeLevelConflictRebuild(b *testing.B) {
	network, id := wholeLevelNetwork(b)
	draft := model.CloneOperation(network.accepted[id].Operation)
	for i := range draft.Changes {
		draft.Changes[i].After.Prefabs[0].Vars["raw"] = "recovered"
	}
	network.retainConflictLocked(Conflict{OperationID: id, Draft: draft})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := network.BuildConflictRebuild(context.Background(), id); err != nil {
			b.Fatal(err)
		}
	}
}

func TestConflictRebuildClaimsDraftUntilPreparationCompletes(t *testing.T) {
	network, id := recoveryCaptureFixture(t, 256)
	state := model.CloneTileState(network.projection.Acknowledged.Tiles[0].State)
	state.Prefabs[0].Vars["dir"] = "8"
	network.conflicts[0].Draft.Changes = []model.TileChange{{Coord: network.projection.Acknowledged.Tiles[0].Coord, After: state}}
	canceled, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &pausedRebuildContext{Context: canceled, entered: make(chan struct{}), release: make(chan struct{})}
	defer close(ctx.release)
	completed := make(chan error, 1)
	if err := network.RebuildConflictAsync(ctx, id, func(_ model.AcceptedOperation, err error) { completed <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.entered:
	case <-time.After(time.Second):
		t.Fatal("preparation did not begin")
	}
	if _, err := network.DiscardConflict(context.Background(), id); err == nil {
		t.Fatal("discard succeeded during rebuild preparation")
	}
	if err := network.RebuildConflictAsync(ctx, id, func(model.AcceptedOperation, error) {}); err == nil {
		t.Fatal("duplicate recovery was admitted")
	}
	cancel()
	ctx.release <- struct{}{}
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("completion error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not complete")
	}
	if len(network.conflicts) != 1 || network.HasUnacknowledgedOperations() {
		t.Fatal("canceled preparation lost or submitted the draft")
	}
	if _, err := network.DiscardConflict(context.Background(), id); err != nil {
		t.Fatalf("failed rebuild retained its claim: %v", err)
	}
}

func TestConflictRecoveryCapturesPinAuthority(t *testing.T) {
	network, id := recoveryCaptureFixture(t, 256)
	ctx := context.Background()
	refreshed, err := network.RefreshConflict(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := network.DiscardConflict(canceled, id); !errors.Is(err, context.Canceled) || len(network.conflicts) != 1 {
		t.Fatal("canceled discard changed the retained draft", err)
	}
	discarded, err := network.DiscardConflict(ctx, id)
	if err != nil || len(network.conflicts) != 0 {
		t.Fatal("discard did not remove the draft", err)
	}
	if _, err := network.RefreshConflict(ctx, id); err == nil {
		t.Fatal("refresh accepted an absent draft")
	}
	replacement := refreshed.AcceptedSnapshot()
	replacement.Revision++
	replacement.Tiles[0].State.Prefabs[0].Vars["dir"] = "4"
	if err := network.ReplaceAcknowledgedSnapshot(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	for _, capture := range []ProjectionCapture{refreshed, discarded} {
		snapshot := capture.AcceptedSnapshot()
		if snapshot.Revision != 0 || snapshot.Tiles[0].State.Prefabs[0].Vars["dir"] != "2" {
			t.Fatal("recovery capture changed after authority replacement")
		}
		snapshot.Tiles[0].State.Prefabs[0].Vars["dir"] = "8"
		if capture.AcceptedSnapshot().Tiles[0].State.Prefabs[0].Vars["dir"] != "2" {
			t.Fatal("materialized recovery snapshot shares mutable values")
		}
	}
}

func recoveryCaptureFixture(t testing.TB, tiles int) (*NetworkExecutor, model.OperationID) {
	t.Helper()
	snapshot := indexedFixture(tiles)
	snapshot.MaxX, snapshot.MaxY = 256, (tiles+255)/256
	for i := range snapshot.Tiles {
		snapshot.Tiles[i].Coord = model.Coord{X: i%256 + 1, Y: i/256 + 1, Z: 1}
	}
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, "01890f3e-7b5c-7abc-8def-0123456789ac", "session")
	if err != nil {
		t.Fatal(err)
	}
	id := model.OperationID("01890f3e-7b5c-7abc-8def-0123456789ad")
	network.conflicts = []Conflict{{OperationID: id}}
	return network, id
}

func TestConflictRecoveryDoesNotCopyWholeMap(t *testing.T) {
	for _, discard := range []bool{false, true} {
		t.Run(fmt.Sprint(discard), func(t *testing.T) {
			network, id := recoveryCaptureFixture(t, 4096)
			allocations := testing.AllocsPerRun(10, func() {
				var err error
				if discard {
					network.conflicts = []Conflict{{OperationID: id}}
					_, err = network.DiscardConflict(context.Background(), id)
				} else {
					_, err = network.RefreshConflict(context.Background(), id)
				}
				if err != nil {
					t.Fatal(err)
				}
			})
			// Constant publication bookkeeping is allowed; per-tile copying is not.
			if allocations > 16 {
				t.Fatalf("recovery copied map-sized data: %.0f allocations", allocations)
			}
		})
	}
}

func BenchmarkConflictRecovery(b *testing.B) {
	for _, discard := range []bool{false, true} {
		b.Run(fmt.Sprintf("discard=%t", discard), func(b *testing.B) {
			network, id := recoveryCaptureFixture(b, 65536)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				if discard {
					network.conflicts = []Conflict{{OperationID: id}}
					_, err = network.DiscardConflict(context.Background(), id)
				} else {
					_, err = network.RefreshConflict(context.Background(), id)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
