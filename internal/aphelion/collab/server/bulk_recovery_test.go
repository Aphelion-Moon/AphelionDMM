package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/store/sqlite"
)

type committedBulkStore struct {
	*sqlite.Store
	loseAcknowledgement bool
}

func (s *committedBulkStore) Append(ctx context.Context, op model.AcceptedOperation) error {
	if err := s.Store.Append(ctx, op); err != nil {
		return err
	}
	if s.loseAcknowledgement {
		s.loseAcknowledgement = false
		return errors.New("injected lost acknowledgement after durable commit")
	}
	return nil
}

func TestSparse512LevelCommitErrorRestartRetryAndUndo(t *testing.T) {
	ctx := context.Background()
	docID, _ := model.NewDocumentID()
	actor, _ := model.NewActorID()
	opID, _ := model.NewOperationID()
	const width = 512
	snapshot := model.Snapshot{ProtocolVersion: 1, SchemaVersion: 1, DocumentID: docID, EnvironmentHash: strings.Repeat("a", 64), MaxX: width, MaxY: width, MaxZ: 2}
	op := model.Operation{ProtocolVersion: 1, DocumentID: docID, ActorID: actor, OperationID: opID, EnvironmentHash: snapshot.EnvironmentHash, Kind: model.OperationKindTileChange}
	for i := 0; i < width*width; i++ {
		coord := model.Coord{X: i%width + 1, Y: i/width + 1, Z: 1}
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: coord})
		change := model.TileChange{Coord: coord}
		if i%97 == 0 {
			change.After.Prefabs = []model.PrefabState{{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", i+1)), Path: "/obj/unknown", Vars: map[string]string{"raw": `list("世界" = 12)`}}}
		}
		op.Changes = append(op.Changes, change)
	}
	sentinel := model.Tile{Coord: model.Coord{X: 1, Y: 1, Z: 2}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: "01890f3e-7b5c-7abc-8def-ffffffffffff", Path: "/obj/untouched", Vars: map[string]string{"raw": "TRUE"}}}}}
	snapshot.Tiles = append(snapshot.Tiles, sentinel)
	op.BaseMapHash, _ = snapshot.Hash()
	path := filepath.Join(t.TempDir(), "bulk.db")
	database, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func(database *sqlite.Store) { _ = database.Close() }(database)
	wrapped := &committedBulkStore{Store: database, loseAcknowledgement: true}
	owner, err := StartDocumentWithConfig(ctx, snapshot, wrapped, DocumentConfig{BulkEdits: true})
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	defer func(owner *DocumentOwner) { _ = owner.Close(context.Background()) }(owner)
	accepted, err := owner.Submit(ctx, op)
	if err != nil || accepted.Revision != 1 {
		t.Fatalf("reconcile durable commit: revision=%d err=%v", accepted.Revision, err)
	}
	current, err := owner.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantHash, _ := current.Hash()
	if err := owner.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	owner, err = RecoverDocument(ctx, docID, database)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close(ctx) }()
	current, err = owner.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gotHash, _ := current.Hash()
	if current.Revision != 1 || gotHash != wantHash || !owner.bulkEdits {
		t.Fatal("restart changed committed authority or capability")
	}
	retry, duplicate, err := owner.SubmitWithStatus(ctx, op)
	if err != nil || !duplicate || retry.Revision != 1 {
		t.Fatalf("retry appended twice: %v", err)
	}
	conflict := model.CloneOperation(op)
	conflict.OperationID, _ = model.NewOperationID()
	if _, err := owner.Submit(ctx, conflict); engine.CodeOf(err) != engine.CodePreconditionFailed {
		t.Fatalf("stale overlap accepted: %v", err)
	}
	inverseID, _ := model.NewOperationID()
	inverse, err := owner.BuildInverse(ctx, actor, opID, inverseID)
	if err != nil {
		t.Fatal(err)
	}
	if accepted, err := owner.Submit(ctx, inverse); err != nil || accepted.Revision != 2 {
		t.Fatalf("undo: %v", err)
	}
	current, err = owner.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gotHash, _ = current.Hash()
	if gotHash != op.BaseMapHash {
		t.Fatal("undo changed original map or untouched second Z")
	}
}
