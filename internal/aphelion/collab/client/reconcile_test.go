package client

import (
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestProjectionSpeculatesAcceptsAndRejects(t *testing.T) {
	t.Parallel()

	snapshot := projectionSnapshot(t)
	projection := NewProjection(snapshot)
	operation := projectionOperation(t, snapshot, 1)
	speculative, err := projection.Submit(operation)
	if err != nil {
		t.Fatal(err)
	}
	if speculative.Acknowledged.Revision != 0 || len(speculative.Pending) != 1 {
		t.Fatalf("speculative projection = %#v", speculative)
	}
	visible, err := speculative.Visible()
	if err != nil {
		t.Fatal(err)
	}
	if len(tileAt(visible, 1).Prefabs) != 1 {
		t.Fatal("speculative operation is absent from visible state")
	}

	accepted := model.AcceptedOperation{Operation: operation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	acceptedSnapshot := snapshotWithOperation(t, snapshot, accepted)
	acceptedHash, err := acceptedSnapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	reconciled, err := speculative.Accept(accepted, acceptedHash)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Acknowledged.Revision != 1 || len(reconciled.Pending) != 0 {
		t.Fatalf("reconciled projection = %#v", reconciled)
	}

	second := projectionOperation(t, reconciled.Acknowledged, 2)
	pending, err := reconciled.Submit(second)
	if err != nil {
		t.Fatal(err)
	}
	authoritative := []model.Tile{{Coord: second.Changes[0].Coord, State: model.CloneTileState(second.Changes[0].After)}}
	rolledBack, conflict, err := pending.Reject(protocol.OperationRejectedPayload{OperationID: second.OperationID, Code: "precondition_failed", Message: "conflict", Revision: 1, MapHash: acceptedHash, AuthoritativeValues: authoritative})
	if err != nil {
		t.Fatal(err)
	}
	if conflict.OperationID != second.OperationID || len(conflict.AuthoritativeValues) != 1 || len(rolledBack.Pending) != 0 {
		t.Fatalf("rejection result = %#v, conflict = %#v", rolledBack, conflict)
	}
	if conflict.Draft.OperationID != second.OperationID || !conflict.Draft.Changes[0].After.Equal(second.Changes[0].After) {
		t.Fatalf("conflict draft = %#v, want rejected operation %#v", conflict.Draft, second)
	}
	authoritative[0].State = model.TileState{}
	if len(conflict.AuthoritativeValues[0].State.Prefabs) == 0 {
		t.Fatal("conflict authoritative values alias protocol payload")
	}
	second.Changes[0].After = model.TileState{}
	if len(conflict.Draft.Changes[0].After.Prefabs) == 0 {
		t.Fatal("conflict draft aliases rejected operation")
	}
}

func TestProjectionRebasesCompatiblePendingOperation(t *testing.T) {
	t.Parallel()

	snapshot := projectionSnapshot(t)
	projection := NewProjection(snapshot)
	pendingOperation := projectionOperation(t, snapshot, 2)
	projection, err := projection.Submit(pendingOperation)
	if err != nil {
		t.Fatal(err)
	}
	remoteOperation := projectionOperation(t, snapshot, 1)
	accepted := model.AcceptedOperation{Operation: remoteOperation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	acceptedSnapshot := snapshotWithOperation(t, snapshot, accepted)
	acceptedHash, err := acceptedSnapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	projection, err = projection.Accept(accepted, acceptedHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Pending) != 1 || projection.Pending[0].BaseRevision != pendingOperation.BaseRevision || projection.Pending[0].BaseMapHash != pendingOperation.BaseMapHash {
		t.Fatalf("rebased pending operation = %#v", projection.Pending)
	}
	visible, err := projection.Visible()
	if err != nil {
		t.Fatal(err)
	}
	if len(tileAt(visible, 1).Prefabs) != 1 || len(tileAt(visible, 2).Prefabs) != 1 {
		t.Fatal("compatible remote and pending edits did not compose")
	}
}

func TestProjectionRefusesOutOfOrderAndHashMismatch(t *testing.T) {
	t.Parallel()

	snapshot := projectionSnapshot(t)
	projection := NewProjection(snapshot)
	operation := projectionOperation(t, snapshot, 1)
	outOfOrder := model.AcceptedOperation{Operation: operation, Revision: 2, AcceptedAt: time.Unix(1, 0)}
	if _, err := projection.Accept(outOfOrder, strings.Repeat("a", 64)); err == nil {
		t.Fatal("out-of-order acceptance succeeded")
	}
	accepted := model.AcceptedOperation{Operation: operation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	if _, err := projection.Accept(accepted, strings.Repeat("f", 64)); err == nil {
		t.Fatal("hash-mismatched acceptance succeeded")
	}
	if projection.Acknowledged.Revision != 0 || len(projection.Pending) != 0 {
		t.Fatal("failed acceptance mutated original projection")
	}
}

func TestPublicProjectionMutationCannotReuseCachedAcknowledgedHash(t *testing.T) {
	snapshot := projectionSnapshot(t)
	projection := NewProjection(snapshot)
	baseHash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	projection.Acknowledged.MaxX++
	operation := projectionOperation(t, snapshot, 1)
	operation.BaseMapHash = baseHash
	if _, err := projection.Submit(operation); err == nil {
		t.Fatal("public projection accepted a stale cached acknowledged hash")
	}
}

func projectionSnapshot(t *testing.T) model.Snapshot {
	t.Helper()
	documentID, err := model.NewDocumentID()
	if err != nil {
		t.Fatal(err)
	}
	return model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: documentID, EnvironmentHash: strings.Repeat("a", 64), MaxX: 2, MaxY: 1, MaxZ: 1}
}

func projectionOperation(t *testing.T, snapshot model.Snapshot, x int) model.Operation {
	t.Helper()
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	operationID, err := model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	stableID, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: snapshot.DocumentID, ActorID: actorID, OperationID: operationID, BaseRevision: snapshot.Revision, EnvironmentHash: snapshot.EnvironmentHash, BaseMapHash: hash, Kind: model.OperationKindTileChange, Changes: []model.TileChange{{Coord: model.Coord{X: x, Y: 1, Z: 1}, Before: tileAt(snapshot, x), After: model.TileState{Prefabs: []model.PrefabState{{StableID: stableID, Path: "/turf/open/floor", Vars: map[string]string{}}}}}}}
}

func snapshotWithOperation(t *testing.T, snapshot model.Snapshot, accepted model.AcceptedOperation) model.Snapshot {
	t.Helper()
	result, err := applyAcceptedSnapshot(snapshot, accepted)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
