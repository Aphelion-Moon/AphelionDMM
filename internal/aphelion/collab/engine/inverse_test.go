package engine

import (
	"fmt"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

func inverseCopyFixture(tb testing.TB, cells int) (*Document, model.AcceptedOperation) {
	tb.Helper()
	document, operation, _ := prepareCopyWorkload(tb, copyWorkload{cells: cells, levels: 1, changes: 1})
	accepted, err := document.Apply(operation, time.Unix(1, 0))
	if err != nil {
		tb.Fatal(err)
	}
	return document, accepted
}

func checkSparseInverse(tb testing.TB, document *Document, target model.AcceptedOperation) {
	tb.Helper()
	inverse, err := document.BuildInverse(testActorID, target.OperationID, "01890f3e-7b5c-7abc-8def-0123456789bc")
	if err != nil || len(inverse.Changes) != 1 || !inverse.Changes[0].Before.Equal(target.Changes[0].After) || !inverse.Changes[0].After.Equal(target.Changes[0].Before) {
		tb.Fatalf("sparse inverse changed accepted values: %v", err)
	}
}

func TestBuildInverseAllocationsDoNotScaleWithUntouchedTiles(t *testing.T) {
	var small float64
	for _, cells := range []int{100, 10000} {
		document, target := inverseCopyFixture(t, cells)
		allocs := testing.AllocsPerRun(10, func() { checkSparseInverse(t, document, target) })
		t.Logf("cells=%d sparse inverse allocations=%g", cells, allocs)
		if small == 0 {
			small = allocs
		} else if allocs > small+16 {
			t.Fatalf("sparse inverse rebuilds the full coordinate index: small=%g large=%g", small, allocs)
		}
	}
}

func BenchmarkBuildSparseInverse(b *testing.B) {
	for _, cells := range []int{100, 10000} {
		b.Run(fmt.Sprint(cells), func(b *testing.B) {
			document, target := inverseCopyFixture(b, cells)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				checkSparseInverse(b, document, target)
			}
		})
	}
}

func TestBuildInverseAndApply(t *testing.T) {
	t.Parallel()

	document, target := documentWithAcceptedTarget(t)
	inverse, err := document.BuildInverse(testActorID, target.OperationID, "01890f3e-7b5c-7abc-8def-0123456789bc")
	if err != nil {
		t.Fatalf("BuildInverse() error = %v", err)
	}
	if inverse.Kind != model.OperationKindInverse {
		t.Fatalf("inverse kind = %q, want %q", inverse.Kind, model.OperationKindInverse)
	}
	if inverse.InverseOf == nil || *inverse.InverseOf != target.OperationID {
		t.Fatalf("inverse target = %v, want %q", inverse.InverseOf, target.OperationID)
	}
	if len(inverse.Changes) != 1 || !inverse.Changes[0].Before.Equal(target.Changes[0].After) || !inverse.Changes[0].After.Equal(target.Changes[0].Before) {
		t.Fatalf("inverse did not swap accepted values: %#v", inverse.Changes)
	}

	accepted, err := document.Apply(inverse, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("Apply(inverse) error = %v", err)
	}
	if accepted.Revision != target.Revision+1 {
		t.Fatalf("inverse revision = %d, want %d", accepted.Revision, target.Revision+1)
	}
	if !document.Snapshot().Tiles[0].State.Equal(target.Changes[0].Before) {
		t.Fatalf("inverse state = %#v, want %#v", document.Snapshot().Tiles[0].State, target.Changes[0].Before)
	}
}

func TestBuildInverseRejectsDifferentActor(t *testing.T) {
	t.Parallel()

	document, target := documentWithAcceptedTarget(t)
	_, err := document.BuildInverse("01890f3e-7b5c-7abc-8def-0123456789bd", target.OperationID, "01890f3e-7b5c-7abc-8def-0123456789bc")
	if got := CodeOf(err); got != CodeActorMismatch {
		t.Fatalf("BuildInverse() code = %q, want %q (error: %v)", got, CodeActorMismatch, err)
	}
}

func TestBuildInverseRejectsAlreadyInvertedTarget(t *testing.T) {
	t.Parallel()

	document, target := documentWithAcceptedTarget(t)
	inverse, err := document.BuildInverse(testActorID, target.OperationID, "01890f3e-7b5c-7abc-8def-0123456789bc")
	if err != nil {
		t.Fatalf("BuildInverse() error = %v", err)
	}
	if _, err := document.Apply(inverse, time.Unix(2, 0)); err != nil {
		t.Fatalf("Apply(inverse) error = %v", err)
	}

	_, err = document.BuildInverse(testActorID, target.OperationID, "01890f3e-7b5c-7abc-8def-0123456789be")
	if got := CodeOf(err); got != CodeAlreadyInverted {
		t.Fatalf("second BuildInverse() code = %q, want %q (error: %v)", got, CodeAlreadyInverted, err)
	}
}

func TestBuildInverseRejectsChangedAfterValue(t *testing.T) {
	t.Parallel()

	document, target := documentWithAcceptedTarget(t)
	current := document.Snapshot()
	later := operationFor(t, current, "01890f3e-7b5c-7abc-8def-0123456789bc", []model.TileChange{{
		Coord:  model.Coord{X: 1, Y: 1, Z: 1},
		Before: target.Changes[0].After,
		After: model.TileState{Prefabs: []model.PrefabState{{
			StableID: "01890f3e-7b5c-7abc-8def-0123456789ac",
			Path:     "/obj/foo1",
			Vars:     map[string]string{"dir": "8"},
		}}},
	}})
	if _, err := document.Apply(later, time.Unix(2, 0)); err != nil {
		t.Fatalf("Apply(later) error = %v", err)
	}

	_, err := document.BuildInverse(testActorID, target.OperationID, "01890f3e-7b5c-7abc-8def-0123456789be")
	if got := CodeOf(err); got != CodePreconditionFailed {
		t.Fatalf("BuildInverse() code = %q, want %q (error: %v)", got, CodePreconditionFailed, err)
	}
}

func TestApplyRejectsForgedInverse(t *testing.T) {
	t.Parallel()

	document, target := documentWithAcceptedTarget(t)
	inverse, err := document.BuildInverse(testActorID, target.OperationID, "01890f3e-7b5c-7abc-8def-0123456789bc")
	if err != nil {
		t.Fatalf("BuildInverse() error = %v", err)
	}
	inverse.Changes[0].After = inverse.Changes[0].Before

	_, err = document.Apply(inverse, time.Unix(2, 0))
	if got := CodeOf(err); got != CodeInvalidOperation {
		t.Fatalf("Apply(forged inverse) code = %q, want %q (error: %v)", got, CodeInvalidOperation, err)
	}
}

func TestRedoIsNewForwardOperation(t *testing.T) {
	t.Parallel()

	document, target := documentWithAcceptedTarget(t)
	inverse, err := document.BuildInverse(testActorID, target.OperationID, "01890f3e-7b5c-7abc-8def-0123456789bc")
	if err != nil {
		t.Fatalf("BuildInverse() error = %v", err)
	}
	if _, err := document.Apply(inverse, time.Unix(2, 0)); err != nil {
		t.Fatalf("Apply(inverse) error = %v", err)
	}

	current := document.Snapshot()
	redo := operationFor(t, current, "01890f3e-7b5c-7abc-8def-0123456789be", []model.TileChange{{
		Coord:  target.Changes[0].Coord,
		Before: target.Changes[0].Before,
		After:  target.Changes[0].After,
	}})
	accepted, err := document.Apply(redo, time.Unix(3, 0))
	if err != nil {
		t.Fatalf("Apply(redo) error = %v", err)
	}
	if accepted.Kind != model.OperationKindTileChange || accepted.InverseOf != nil {
		t.Fatalf("redo was not a new forward operation: %#v", accepted.Operation)
	}
}

func TestBuildInverseRejectsInvertingAnInverse(t *testing.T) {
	t.Parallel()

	document, target := documentWithAcceptedTarget(t)
	inverse, err := document.BuildInverse(testActorID, target.OperationID, "01890f3e-7b5c-7abc-8def-0123456789bc")
	if err != nil {
		t.Fatalf("BuildInverse() error = %v", err)
	}
	acceptedInverse, err := document.Apply(inverse, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("Apply(inverse) error = %v", err)
	}
	_, err = document.BuildInverse(testActorID, acceptedInverse.OperationID, "01890f3e-7b5c-7abc-8def-0123456789be")
	if got := CodeOf(err); got != CodeInvalidOperation {
		t.Fatalf("BuildInverse(inverse) code = %q, want %q (error: %v)", got, CodeInvalidOperation, err)
	}
}

func documentWithAcceptedTarget(t *testing.T) (*Document, model.AcceptedOperation) {
	t.Helper()
	snapshot := initialSnapshot()
	document, err := NewDocument(snapshot)
	if err != nil {
		t.Fatalf("NewDocument() error = %v", err)
	}
	target := operationFor(t, snapshot, "01890f3e-7b5c-7abc-8def-0123456789bb", []model.TileChange{{
		Coord:  model.Coord{X: 1, Y: 1, Z: 1},
		Before: tileOneBefore(),
		After: model.TileState{Prefabs: []model.PrefabState{{
			StableID: "01890f3e-7b5c-7abc-8def-0123456789ac",
			Path:     "/obj/foo1",
			Vars:     map[string]string{"dir": "4"},
		}}},
	}})
	accepted, err := document.Apply(target, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("Apply(target) error = %v", err)
	}
	return document, accepted
}
