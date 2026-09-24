package engine

import (
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

const (
	testDocumentID      = model.DocumentID("01890f3e-7b5c-7abc-8def-0123456789ab")
	testActorID         = model.ActorID("01890f3e-7b5c-7abc-8def-0123456789ba")
	testEnvironmentHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestDocumentApply(t *testing.T) {
	t.Parallel()

	snapshot := initialSnapshot()
	document, err := NewDocument(snapshot)
	if err != nil {
		t.Fatalf("NewDocument() error = %v", err)
	}
	operation := operationFor(t, snapshot, "01890f3e-7b5c-7abc-8def-0123456789bb", []model.TileChange{{
		Coord:  model.Coord{X: 1, Y: 1, Z: 1},
		Before: tileOneBefore(),
		After: model.TileState{Prefabs: []model.PrefabState{{
			StableID: "01890f3e-7b5c-7abc-8def-0123456789ac",
			Path:     "/obj/foo1",
			Vars:     map[string]string{"dir": "4"},
		}}},
	}})
	acceptedAt := time.Date(2026, time.August, 24, 20, 0, 0, 0, time.UTC)

	accepted, err := document.Apply(operation, acceptedAt)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if accepted.Revision != snapshot.Revision+1 {
		t.Fatalf("accepted revision = %d, want %d", accepted.Revision, snapshot.Revision+1)
	}
	if !accepted.AcceptedAt.Equal(acceptedAt) {
		t.Fatalf("accepted time = %s, want %s", accepted.AcceptedAt, acceptedAt)
	}
	got := document.Snapshot()
	if got.Revision != accepted.Revision {
		t.Fatalf("snapshot revision = %d, want %d", got.Revision, accepted.Revision)
	}
	if !got.Tiles[0].State.Equal(operation.Changes[0].After) {
		t.Fatalf("snapshot tile = %#v, want %#v", got.Tiles[0].State, operation.Changes[0].After)
	}
	if !snapshot.Tiles[0].State.Equal(tileOneBefore()) {
		t.Fatalf("input snapshot was mutated: %#v", snapshot.Tiles[0].State)
	}
}

func TestDocumentApplyRejectsInvalidOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*model.Operation)
		wantCode Code
	}{
		{
			name: "wrong document",
			mutate: func(operation *model.Operation) {
				operation.DocumentID = "01890f3e-7b5c-7abc-8def-0123456789bc"
			},
			wantCode: CodeWrongDocument,
		},
		{
			name: "wrong environment",
			mutate: func(operation *model.Operation) {
				operation.EnvironmentHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			},
			wantCode: CodeWrongEnvironment,
		},
		{
			name: "unknown base revision",
			mutate: func(operation *model.Operation) {
				operation.BaseRevision = 999
			},
			wantCode: CodeUnknownBaseRevision,
		},
		{
			name: "wrong base hash",
			mutate: func(operation *model.Operation) {
				operation.BaseMapHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			},
			wantCode: CodeBaseHashMismatch,
		},
		{
			name: "out of bounds",
			mutate: func(operation *model.Operation) {
				operation.Changes[0].Coord.X = 3
			},
			wantCode: CodeOutOfBounds,
		},
		{
			name: "stale value precondition",
			mutate: func(operation *model.Operation) {
				operation.Changes[0].Before.Prefabs[0].Vars["dir"] = "8"
			},
			wantCode: CodePreconditionFailed,
		},
		{
			name: "duplicate change coordinate",
			mutate: func(operation *model.Operation) {
				operation.Changes = append(operation.Changes, operation.Changes[0])
			},
			wantCode: CodeInvalidOperation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := initialSnapshot()
			document, err := NewDocument(snapshot)
			if err != nil {
				t.Fatalf("NewDocument() error = %v", err)
			}
			operation := operationFor(t, snapshot, "01890f3e-7b5c-7abc-8def-0123456789bb", []model.TileChange{{
				Coord:  model.Coord{X: 1, Y: 1, Z: 1},
				Before: tileOneBefore(),
				After:  model.TileState{},
			}})
			test.mutate(&operation)

			_, err = document.Apply(operation, time.Now())
			if got := CodeOf(err); got != test.wantCode {
				t.Fatalf("Apply() code = %q, want %q (error: %v)", got, test.wantCode, err)
			}
			if got := document.Snapshot(); !reflect.DeepEqual(got, snapshot) {
				t.Fatalf("rejected operation mutated document:\ngot  %#v\nwant %#v", got, snapshot)
			}
		})
	}
}

func TestDocumentApplyIsAllOrNothing(t *testing.T) {
	t.Parallel()

	snapshot := initialSnapshot()
	document, err := NewDocument(snapshot)
	if err != nil {
		t.Fatalf("NewDocument() error = %v", err)
	}
	operation := operationFor(t, snapshot, "01890f3e-7b5c-7abc-8def-0123456789bb", []model.TileChange{
		{
			Coord:  model.Coord{X: 1, Y: 1, Z: 1},
			Before: tileOneBefore(),
			After:  model.TileState{},
		},
		{
			Coord:  model.Coord{X: 2, Y: 1, Z: 1},
			Before: model.TileState{Prefabs: []model.PrefabState{{StableID: "01890f3e-7b5c-7abc-8def-0123456789ae", Path: "/obj/foo3"}}},
			After:  model.TileState{},
		},
	})

	_, err = document.Apply(operation, time.Now())
	if got := CodeOf(err); got != CodePreconditionFailed {
		t.Fatalf("Apply() code = %q, want %q (error: %v)", got, CodePreconditionFailed, err)
	}
	if got := document.Snapshot(); !reflect.DeepEqual(got, snapshot) {
		t.Fatalf("partially rejected operation mutated document:\ngot  %#v\nwant %#v", got, snapshot)
	}
}

func TestDocumentApplyReturnsOriginalResultForDuplicate(t *testing.T) {
	t.Parallel()

	snapshot := initialSnapshot()
	document, err := NewDocument(snapshot)
	if err != nil {
		t.Fatalf("NewDocument() error = %v", err)
	}
	operation := operationFor(t, snapshot, "01890f3e-7b5c-7abc-8def-0123456789bb", []model.TileChange{{
		Coord:  model.Coord{X: 1, Y: 1, Z: 1},
		Before: tileOneBefore(),
		After:  model.TileState{},
	}})
	first, err := document.Apply(operation, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}
	// Only an exact retry is idempotent. Altered content with the same ID is
	// covered by TestWholeLevelAlteredRetryRejected.
	second, err := document.Apply(operation, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("duplicate Apply() error = %v", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("duplicate result = %#v, want original %#v", second, first)
	}
	if got := document.Snapshot().Revision; got != first.Revision {
		t.Fatalf("duplicate advanced revision to %d, want %d", got, first.Revision)
	}
}

func TestDocumentDoesNotAliasInputsOrResults(t *testing.T) {
	t.Parallel()

	snapshot := initialSnapshot()
	document, err := NewDocument(snapshot)
	if err != nil {
		t.Fatalf("NewDocument() error = %v", err)
	}
	snapshot.Tiles[0].State.Prefabs[0].Vars["dir"] = "99"
	if !document.Snapshot().Tiles[0].State.Equal(tileOneBefore()) {
		t.Fatal("NewDocument() retained an alias to its input")
	}

	base := document.Snapshot()
	operation := operationFor(t, base, "01890f3e-7b5c-7abc-8def-0123456789bb", []model.TileChange{{
		Coord:  model.Coord{X: 1, Y: 1, Z: 1},
		Before: tileOneBefore(),
		After: model.TileState{Prefabs: []model.PrefabState{{
			StableID: "01890f3e-7b5c-7abc-8def-0123456789ac",
			Path:     "/obj/foo1",
			Vars:     map[string]string{"dir": "4"},
		}}},
	}})
	accepted, err := document.Apply(operation, time.Now())
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	operation.Changes[0].After.Prefabs[0].Vars["dir"] = "98"
	accepted.Changes[0].After.Prefabs[0].Vars["dir"] = "97"
	returned := document.Snapshot()
	returned.Tiles[0].State.Prefabs[0].Vars["dir"] = "96"

	got := document.Snapshot().Tiles[0].State.Prefabs[0].Vars["dir"]
	if got != "4" {
		t.Fatalf("engine state changed through alias: got dir %q, want %q", got, "4")
	}
}

func initialSnapshot() model.Snapshot {
	return model.Snapshot{
		ProtocolVersion: model.ProtocolVersion,
		SchemaVersion:   model.SchemaVersion,
		DocumentID:      testDocumentID,
		Revision:        4,
		EnvironmentHash: testEnvironmentHash,
		MaxX:            2,
		MaxY:            1,
		MaxZ:            1,
		Tiles: []model.Tile{
			{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: tileOneBefore()},
			{Coord: model.Coord{X: 2, Y: 1, Z: 1}, State: model.TileState{}},
		},
	}
}

func tileOneBefore() model.TileState {
	return model.TileState{Prefabs: []model.PrefabState{{
		StableID: "01890f3e-7b5c-7abc-8def-0123456789ac",
		Path:     "/obj/foo1",
		Vars:     map[string]string{"dir": "2"},
	}}}
}

func operationFor(t *testing.T, snapshot model.Snapshot, id model.OperationID, changes []model.TileChange) model.Operation {
	t.Helper()
	baseHash, err := snapshot.Hash()
	if err != nil {
		t.Fatalf("hash base snapshot: %v", err)
	}
	return model.Operation{
		ProtocolVersion: model.ProtocolVersion,
		DocumentID:      snapshot.DocumentID,
		ActorID:         testActorID,
		OperationID:     id,
		BaseRevision:    snapshot.Revision,
		EnvironmentHash: snapshot.EnvironmentHash,
		BaseMapHash:     baseHash,
		Kind:            model.OperationKindTileChange,
		Changes:         changes,
	}
}
