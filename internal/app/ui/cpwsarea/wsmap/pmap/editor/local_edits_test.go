package editor

import (
	"context"
	"testing"

	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

type countedLocalEdits struct {
	*executor.Local
	snapshots, wireEdits int
}

func (e *countedLocalEdits) Snapshot(ctx context.Context) (model.Snapshot, error) {
	e.snapshots++
	return e.Local.Snapshot(ctx)
}
func (e *countedLocalEdits) Execute(ctx context.Context, op model.Operation) (model.AcceptedOperation, error) {
	e.wireEdits++
	return e.Local.Execute(ctx, op)
}

func TestLocalEditAndHistoryUseDeltasWithoutWireOrSnapshots(t *testing.T) {
	e := selectionEditor(t)
	counted := &countedLocalEdits{Local: e.executor.(*executor.Local)}
	e.executor = counted
	compatibility := e.pMap.Snapshot().Initial()
	untouched := e.dmm.Tiles[len(e.dmm.Tiles)-1]
	instance := e.dmm.Tiles[0].Instances()[2]
	id := instance.StableID()
	e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "4")))
	e.CommitOperation("Local direction")
	commands := e.app.CommandStorage()
	commands.UndoV("test")
	assertEditorDirection(t, e.dmm, "2")
	commands.RedoV("test")
	assertEditorDirection(t, e.dmm, "4")
	if counted.snapshots != 0 || counted.wireEdits != 0 {
		t.Fatalf("ordinary local edit/history: snapshots=%d wire edits=%d", counted.snapshots, counted.wireEdits)
	}
	if e.pMap.Snapshot().Initial() != compatibility || e.dmm.Tiles[len(e.dmm.Tiles)-1] != untouched {
		t.Fatal("ordinary edit/history replaced whole display or compatibility map")
	}
	if e.dmm.Tiles[0].Instances()[2].StableID() != id {
		t.Fatal("history changed stable identity")
	}
	saved, err := e.SaveSnapshot(context.Background())
	if err != nil || saved.Revision != 3 {
		t.Fatal("save did not capture accepted revision", saved.Revision, err)
	}
	if _, err := saved.Hash(); err != nil {
		t.Fatal("local authority cannot be canonically exported", err)
	}
}

// APHELION EDIT ADDITION START - RESPONSIVE_SAVE
func TestSaveCaptureDefersLocalSnapshotMaterialization(t *testing.T) {
	e := selectionEditor(t)
	counted := &countedLocalEdits{Local: e.executor.(*executor.Local)}
	e.executor = counted
	initialHash, err := e.authoritative.Hash()
	if err != nil {
		t.Fatal(err)
	}
	captured, version, err := e.CaptureSaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counted.snapshots != 0 || version.Revision != e.authoritative.Revision {
		t.Fatalf("save capture materialized synchronously or selected the wrong revision: snapshots=%d capture=%d authority=%d", counted.snapshots, version.Revision, e.authoritative.Revision)
	}

	instance := e.dmm.Tiles[0].Instances()[2]
	e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "4")))
	e.CommitOperation("Edit while save materializes")
	materialized := captured.Snapshot()
	materializedHash, err := materialized.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if materialized.Revision != version.Revision || materializedHash != initialHash || counted.snapshots != 0 {
		t.Fatal("deferred save materialization did not preserve its captured local revision")
	}
}
// APHELION EDIT ADDITION END

func TestAttachedLocalCapabilityStillUsesSessionContract(t *testing.T) {
	e := selectionEditor(t)
	counted := &countedLocalEdits{Local: e.executor.(*executor.Local)}
	if err := e.AttachCollaborationExecutor(counted); err != nil {
		t.Fatal(err)
	}
	counted.snapshots = 0
	instance := e.dmm.Tiles[0].Instances()[2]
	e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "4")))
	e.CommitOperation("Session direction")
	if counted.wireEdits != 1 {
		t.Fatal("attached session silently became solo")
	}
}
