package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

func TestWholeLevelAlteredRetryRejected(t *testing.T) {
	snapshot, op := largeEditFixture(t, 4097)
	doc, err := NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Apply(op, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	op.Changes[4096].After.Prefabs[0].Vars["raw"] = "changed"
	if _, err := doc.Apply(op, time.Unix(2, 0)); CodeOf(err) != CodeInvalidOperation {
		t.Fatalf("altered retry: %v", err)
	}
}

type cancelDuringValidation struct {
	context.Context
	remaining int
}

func (c *cancelDuringValidation) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestWholeLevelCancellationBeforeCommitIsAtomic(t *testing.T) {
	snapshot, op := largeEditFixture(t, 65536)
	doc, err := NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &cancelDuringValidation{Context: context.Background(), remaining: 70000}
	if _, err := doc.ApplyContext(ctx, op, time.Unix(1, 0)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	got := doc.Snapshot()
	hash, _ := got.Hash()
	if got.Revision != 0 || hash != op.BaseMapHash {
		t.Fatal("cancelled edit changed authority")
	}
}

func TestWholeLevelInversePreparationIsCancellable(t *testing.T) {
	snapshot, op := largeEditFixture(t, 65536)
	doc, err := NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Apply(op, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelDuringValidation{Context: context.Background(), remaining: 100}
	if _, err := doc.BuildInverseContext(ctx, op.ActorID, op.OperationID, "01890f3e-7b5c-7abc-8def-0123456789bc"); !errors.Is(err, context.Canceled) {
		t.Fatalf("inverse preparation did not cancel: %v", err)
	}
	if doc.Snapshot().Revision != 1 {
		t.Fatal("cancelled inverse changed authority")
	}
}

func largeEditFixture(t testing.TB, count int) (model.Snapshot, model.Operation) {
	t.Helper()
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion,
		DocumentID: testDocumentID, EnvironmentHash: testEnvironmentHash, MaxX: 256, MaxY: 256, MaxZ: 2}
	hash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	op := model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: testDocumentID, ActorID: testActorID,
		OperationID: "01890f3e-7b5c-7abc-8def-0123456789bb", EnvironmentHash: testEnvironmentHash, BaseMapHash: hash, Kind: model.OperationKindTileChange}
	for index := 0; index < count; index++ {
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: index%256 + 1, Y: index/256 + 1, Z: 1}})
		op.Changes = append(op.Changes, model.TileChange{Coord: model.Coord{X: index%256 + 1, Y: index/256 + 1, Z: 1},
			After: model.TileState{Prefabs: []model.PrefabState{{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", index+1)), Path: "/obj/unknown", Vars: map[string]string{"raw": `list("a" = 12)`}}}}})
	}
	hash, err = snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	op.BaseMapHash = hash
	return snapshot, op
}

func TestWholeLevelEditIsOneAtomicUndoableOperation(t *testing.T) {
	for _, count := range []int{4097, 256 * 256} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			snapshot, op := largeEditFixture(t, count)
			doc, err := NewDocument(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := doc.Apply(op, time.Unix(1, 0))
			if err != nil {
				t.Fatal(err)
			}
			if accepted.Revision != 1 || len(accepted.Changes) != count {
				t.Fatal("large edit split or truncated")
			}
			for _, tile := range doc.Snapshot().Tiles {
				if tile.Coord.Z != 1 || tile.State.Prefabs[0].Vars["raw"] != `list("a" = 12)` {
					t.Fatal("out-of-scope edit or lost raw value")
				}
			}
			inverse, err := doc.BuildInverse(testActorID, op.OperationID, "01890f3e-7b5c-7abc-8def-0123456789bc")
			if err != nil {
				t.Fatal(err)
			}
			if len(inverse.Changes) != count {
				t.Fatal("inverse truncated")
			}
			undone, err := doc.Apply(inverse, time.Unix(2, 0))
			if err != nil {
				t.Fatal(err)
			}
			if undone.Revision != 2 {
				t.Fatal("undo split into multiple revisions")
			}
			for _, tile := range doc.Snapshot().Tiles {
				if len(tile.State.Prefabs) != 0 {
					t.Fatal("undo did not restore empty level")
				}
			}
			hash, err := doc.Snapshot().Hash()
			if err != nil || hash != op.BaseMapHash {
				t.Fatal("undo changed canonical identity")
			}
		})
	}
}

func TestWholeLevelLateConflictIsAtomic(t *testing.T) {
	snapshot, op := largeEditFixture(t, 256*256)
	op.Changes[len(op.Changes)-1].Before = op.Changes[0].After
	doc, err := NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Apply(op, time.Unix(1, 0)); CodeOf(err) != CodePreconditionFailed {
		t.Fatalf("want late precondition rejection: %v", err)
	}
	got := doc.Snapshot()
	hash, err := got.Hash()
	if err != nil || got.Revision != 0 || hash != op.BaseMapHash {
		t.Fatal("late conflict partially mutated authority")
	}
}

func BenchmarkWholeLevelApply(b *testing.B) {
	for _, count := range []int{4096, 4097, 65536} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			snapshot, op := largeEditFixture(b, count)
			doc, err := NewDocument(snapshot)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := doc.Clone().Apply(op, time.Unix(1, 0)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
