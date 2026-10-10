package engine

import (
	"fmt"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

type copyWorkload struct{ cells, history, levels, changes int }

var copyWorkloads = []copyWorkload{
	{100, 0, 1, 1}, {1000, 0, 1, 1}, {10000, 0, 1, 1},
	{100, 100, 1, 1}, {100, 1000, 1, 1},
	{1000, 0, 5, 100}, {10000, 0, 5, 100},
	{10000, 1000, 5, 100},
}

func (work copyWorkload) name() string {
	return fmt.Sprintf("cells=%d/history=%d/z=%d/changes=%d", work.cells, work.history, work.levels, work.changes)
}

func copyFixture(cells, levels int) model.Snapshot {
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion,
		DocumentID: testDocumentID, EnvironmentHash: testEnvironmentHash, MaxX: 10, MaxY: cells / (10 * levels), MaxZ: levels}
	for index := range cells {
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{
			Coord: model.Coord{X: index%10 + 1, Y: (index/10)%snapshot.MaxY + 1, Z: index/(10*snapshot.MaxY) + 1},
			State: model.TileState{Prefabs: []model.PrefabState{{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", index+1)),
				Path: "/obj/foo", Vars: map[string]string{"dir": "2"}}}},
		})
	}
	return snapshot
}

func copyOperation(tb testing.TB, snapshot model.Snapshot, serial, count int) model.Operation {
	tb.Helper()
	hash, err := snapshot.Hash()
	if err != nil {
		tb.Fatal(err)
	}
	operation := model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: snapshot.DocumentID,
		ActorID: testActorID, OperationID: model.OperationID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", serial)),
		BaseRevision: snapshot.Revision, BaseMapHash: hash, EnvironmentHash: snapshot.EnvironmentHash, Kind: model.OperationKindTileChange}
	for index := range count {
		tile := snapshot.Tiles[index]
		after := model.CloneTileState(tile.State)
		value := "4"
		if after.Prefabs[0].Vars["dir"] == value {
			value = "2"
		}
		after.Prefabs[0].Vars["dir"] = value
		operation.Changes = append(operation.Changes, model.TileChange{Coord: tile.Coord, Before: model.CloneTileState(tile.State), After: after})
	}
	return operation
}

func prepareCopyWorkload(tb testing.TB, work copyWorkload) (*Document, model.Operation, string) {
	tb.Helper()
	document, err := NewDocument(copyFixture(work.cells, work.levels))
	if err != nil {
		tb.Fatal(err)
	}
	for index := range work.history {
		if _, err := document.Apply(copyOperation(tb, document.snapshot, index+1, 1), time.Unix(int64(index), 0)); err != nil {
			tb.Fatal(err)
		}
	}
	op := copyOperation(tb, document.Snapshot(), work.history+1, work.changes)
	// Independent expected state: the fixed workload changes existing cells in
	// their fixture order. It does not call engine validation/application.
	want := document.Snapshot()
	for index, change := range op.Changes {
		want.Tiles[index].State = model.CloneTileState(change.After)
	}
	want.Revision++
	wantHash, err := want.Hash()
	if err != nil {
		tb.Fatal(err)
	}
	return document, op, wantHash
}

var copyDocumentSink *Document
var copySnapshotSink model.Snapshot

func BenchmarkDocumentCopyStages(b *testing.B) {
	for _, work := range copyWorkloads {
		b.Run(work.name(), func(b *testing.B) {
			document, operation, wantHash := prepareCopyWorkload(b, work)
			b.Run("Clone", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					copyDocumentSink = document.Clone()
					if copyDocumentSink.mapHash != document.mapHash {
						b.Fatal("clone hash differs")
					}
				}
			})
			b.Run("Validate", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					_, candidate, hash, _, err := document.validate(operation)
					if err != nil || hash != wantHash {
						b.Fatalf("validation differs: %v", err)
					}
					copySnapshotSink = candidate
				}
			})
			b.Run("CloneApply", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					candidate := document.Clone()
					accepted, err := candidate.Apply(operation, time.Unix(1, 0))
					if err != nil || accepted.Revision != document.snapshot.Revision+1 || candidate.mapHash != wantHash {
						b.Fatalf("application differs: %v", err)
					}
					copyDocumentSink = candidate
				}
			})
		})
	}
}

func TestDocumentCopyWorkloadHashes(t *testing.T) {
	for _, work := range copyWorkloads {
		document, operation, wantHash := prepareCopyWorkload(t, work)
		beforeHash := document.mapHash
		candidate := document.Clone()
		accepted, err := candidate.Apply(operation, time.Unix(1, 0))
		if err != nil || accepted.Revision != document.snapshot.Revision+1 || candidate.mapHash != wantHash || document.mapHash != beforeHash {
			t.Fatalf("%s changed the workload: %v", work.name(), err)
		}
		t.Logf("%s base=%s result=%s revision=%d", work.name(), beforeHash, wantHash, accepted.Revision)
	}
}

func TestDocumentCloneAllocationDoesNotScaleWithMapPayload(t *testing.T) {
	counts := make([]float64, 0, 2)
	for _, cells := range []int{100, 10000} {
		document, err := NewDocument(copyFixture(cells, 1))
		if err != nil {
			t.Fatal(err)
		}
		allocations := testing.AllocsPerRun(10, func() { copyDocumentSink = document.Clone() })
		t.Logf("cells=%d clone allocations=%g", cells, allocations)
		counts = append(counts, allocations)
	}
	if counts[1] > counts[0]+16 {
		t.Fatalf("clone allocation still scales with unchanged map payload: %v", counts)
	}
}
