package editor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type recoverySnapshotExecutor struct {
	executor.Executor
	snapshot model.Snapshot
}

func (e *recoverySnapshotExecutor) Snapshot(context.Context) (model.Snapshot, error) {
	return e.snapshot, nil
}

type recoveryProjectionExecutor struct {
	executor.Executor
	updates chan client.Projection
	pending bool
}

func (e *recoveryProjectionExecutor) ProjectionUpdates() <-chan client.Projection { return e.updates }
func (e *recoveryProjectionExecutor) HasUnacknowledgedOperations() bool           { return e.pending }

func TestLocalRecoveryFaultSurvivesProjection(t *testing.T) {
	e := captureEditor(t)
	snapshot, err := e.executor.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	projection := &recoveryProjectionExecutor{Executor: e.executor, updates: make(chan client.Projection, 1)}
	e.executor = projection
	e.dmm.Tiles[0].Instances()[2].SetStableID("retain-until-explicit-discard")
	if e.TryBeginTileChange(util.Point{X: 1, Y: 1, Z: 1}) {
		t.Fatal("fixture captured")
	}
	before := e.dmm.Copy()
	projection.updates <- client.Projection{Acknowledged: snapshot}
	e.ProcessCollaborationUpdates()
	if !reflect.DeepEqual(e.dmm, &before) {
		t.Fatal("network projection replaced damaged display before explicit discard")
	}
	e.syncFromExecutor(projection, true, 1, nil)
	if !reflect.DeepEqual(e.dmm, &before) {
		t.Fatal("completion replaced damaged display before explicit discard")
	}
	if err := e.RefreshCollaborationSnapshot(context.Background()); err == nil {
		t.Fatal("ordinary refresh discarded faulted display")
	}
	draft, err := e.InspectLocalRecovery()
	if err != nil {
		t.Fatal(err)
	}
	projection.pending = true
	if err := e.DiscardLocalRecovery(draft); err == nil {
		t.Fatal("discard ignored pending acknowledgement")
	}
	projection.pending = false
	if err := e.DiscardLocalRecovery(draft); err != nil {
		t.Fatal(err)
	}
	e.ProcessCollaborationUpdates()
	if e.HasLocalRecovery() {
		t.Fatal("recovery did not resume projections")
	}
}

func TestLocalRecoveryRetainsExportsAndDiscards(t *testing.T) {
	e := captureEditor(t)
	ctx := context.Background()
	initial, _ := e.SaveSnapshot(ctx)
	i := e.dmm.Tiles[0].Instances()[2]
	e.InstanceReplace(i, dmmprefab.New(dmmprefab.IdNone, i.Prefab().Path(), dmvars.Set(i.Prefab().Vars(), "dir", "4")))
	e.CommitOperation("accepted edit")
	committed, _ := e.SaveSnapshot(ctx)
	// Retain a valid earlier edit as well as a later damaged tile.
	e.InstanceReplace(i, dmmprefab.New(dmmprefab.IdNone, "/obj/unknown/recovery", dmvars.Set(i.Prefab().Vars(), "custom", "\"preserve me\"")))
	fault := util.Point{X: 4, Y: 2, Z: 1}
	e.dmm.GetTile(fault).Instances()[2].SetStableID("invalid-retained-id")
	if e.TryBeginTileChange(fault) {
		t.Fatal("fixture did not fail capture")
	}
	before := e.dmm.Copy()
	generation, history := e.attachmentGeneration, e.historyGeneration
	draft, err := e.InspectLocalRecovery()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "recovery.json")
	if err := draft.Export(path); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil || !json.Valid(encoded) || !strings.Contains(string(encoded), "invalid-retained-id") || !strings.Contains(string(encoded), "/obj/unknown/recovery") {
		t.Fatal("export lost damaged or unsubmitted contents", err)
	}
	if !reflect.DeepEqual(e.dmm, &before) || len(e.pendingChanges) != 1 {
		t.Fatal("inspection/export modified retained work")
	}
	if _, err := e.SaveSnapshot(ctx); err == nil {
		t.Fatal("export removed Save guard")
	}
	if err := draft.Export(filepath.Join(path, "impossible.json")); err == nil {
		t.Fatal("bad export succeeded")
	}
	if err := e.DiscardLocalRecovery(draft); err != nil {
		t.Fatal(err)
	}
	recovered, err := e.SaveSnapshot(ctx)
	if err != nil || !reflect.DeepEqual(recovered, committed) {
		t.Fatal("discard did not restore committed authority", err)
	}
	if generation != e.attachmentGeneration || history != e.historyGeneration {
		t.Fatal("discard reset attachment/history")
	}
	app := e.app.(*noopReportingApp)
	app.commands.UndoV("test")
	undone, err := e.SaveSnapshot(ctx)
	if err != nil || !reflect.DeepEqual(undone.Tiles, initial.Tiles) {
		t.Fatal("discard invalidated accepted undo", err)
	}
	app.commands.RedoV("test")
	redone, err := e.SaveSnapshot(ctx)
	if err != nil || !reflect.DeepEqual(redone.Tiles, committed.Tiles) {
		t.Fatal("discard invalidated accepted redo", err)
	}
	if e.HasLocalRecovery() {
		t.Fatal("successful recovery remains faulted")
	}
}

func TestLocalRecoveryRefusesChangedOrBusyEditor(t *testing.T) {
	for _, change := range []string{"display", "journal", "attachment", "submission", "preview", "closed", "invalid authority", "other editor"} {
		t.Run(change, func(t *testing.T) {
			e := captureEditor(t)
			e.BeginTileChange(util.Point{X: 1, Y: 1, Z: 1})
			draft, err := e.InspectLocalRecovery()
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "display":
				e.dmm.Tiles[0].Instances()[2].SetStableID("changed-after-inspection")
			case "journal":
				e.BeginTileChange(util.Point{X: 2, Y: 1, Z: 1})
			case "attachment":
				e.attachmentGeneration++
			case "submission":
				e.unresolvedSubmissions[model.OperationID("pending")] = struct{}{}
			case "preview":
				e.pendingChanges = make(map[model.Coord]model.TileState)
				if _, err := e.BeginSelectionMove(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1); err != nil {
					t.Fatal(err)
				}
			case "closed":
				e.Close()
			case "invalid authority":
				e.executor = &recoverySnapshotExecutor{Executor: e.executor, snapshot: model.Snapshot{}}
			case "other editor":
				e = captureEditor(t)
				e.BeginTileChange(util.Point{X: 1, Y: 1, Z: 1})
			}
			before := e.dmm.Copy()
			pending := len(e.pendingChanges)
			if err := e.DiscardLocalRecovery(draft); err == nil {
				t.Fatal("discard accepted changed or unsafe state")
			}
			if !reflect.DeepEqual(e.dmm, &before) || len(e.pendingChanges) != pending {
				t.Fatal("refused discard modified retained work")
			}
		})
	}
}
