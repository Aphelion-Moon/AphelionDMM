package server

import (
	"context"
	"errors"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func TestDocumentOwnerAuthorityReadIsCoherentAndDetached(t *testing.T) {
	ctx := context.Background()
	initial := testSnapshot(t, 2)
	owner, err := StartDocument(ctx, initial, NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(ctx) })
	operation := testOperation(t, initial, 1)
	accepted, err := owner.Submit(ctx, operation)
	if err != nil {
		t.Fatal(err)
	}
	coord := operation.Changes[0].Coord
	missing := model.Coord{X: 2, Y: 1, Z: 1}
	metadata, values, err := owner.ReadAuthority(ctx, []model.Coord{coord, missing, coord})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := owner.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Revision != accepted.Revision || metadata.MapHash != hash || metadata.DocumentID != initial.DocumentID {
		t.Fatalf("metadata does not match authority: %+v", metadata)
	}
	if len(values) != 2 || values[0].Coord != coord || !values[0].State.Equal(operation.Changes[0].After) || values[1].Coord != missing || len(values[1].State.Prefabs) != 0 {
		t.Fatalf("sparse conflict values do not match authority: %+v", values)
	}
	values[0].State.Prefabs[0].Path = "/mutated"
	_, again, err := owner.ReadAuthority(ctx, []model.Coord{coord})
	if err != nil {
		t.Fatal(err)
	}
	if !again[0].State.Equal(operation.Changes[0].After) {
		t.Fatal("reader mutated authority")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := owner.ReadAuthority(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read: %v", err)
	}
}
