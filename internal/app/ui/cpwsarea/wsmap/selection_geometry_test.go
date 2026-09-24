package wsmap

import (
	"context"
	"errors"
	"testing"

	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/util"
)

type rejectedSelectionExecutor struct{ executor.Executor }

func (*rejectedSelectionExecutor) Execute(context.Context, model.Operation) (model.AcceptedOperation, error) {
	return model.AcceptedOperation{}, errors.New("controlled immediate rejection")
}

func TestSelectionImmediateRejectionRestoresBoundsAndMap(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	grab.Reset()
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	_, _, document := selectionNetwork(t, ws)
	e := ws.Map().Editor()
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	local, err := executor.NewLocal(document, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AttachCollaborationExecutor(&rejectedSelectionExecutor{Executor: local}); err != nil {
		t.Fatal(err)
	}
	before := grab.Bounds()
	want, err := document.Snapshot().Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := grab.Rotate(true, e.RotateSelection); err != nil {
		t.Fatal(err)
	}
	if grab.Bounds() != before || len(app.errors) != 1 || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("synchronous rejection left transformed selection/history or missed its error")
	}
	snapshot, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := snapshot.Hash()
	if err != nil || got != want {
		t.Fatal("synchronous rejection changed map authority")
	}
}

func TestSelectionNetworkRejectedTransformRestoresBounds(t *testing.T) {
	for _, rotate := range []bool{true, false} {
		t.Run(map[bool]string{true: "rectangular rotation", false: "nudge"}[rotate], func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			grab := activateSelectionWorkspace(t, ws)
			grab.Reset()
			grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
			network, transport, document := selectionNetwork(t, ws)
			before := grab.Bounds()
			e := ws.Map().Editor()
			var err error
			if rotate {
				err = grab.Rotate(true, e.RotateSelection)
			} else {
				err = grab.Nudge(util.Point{Y: 1})
			}
			if err != nil {
				t.Fatal(err)
			}
			if grab.Bounds() == before {
				t.Fatal("fixture did not change speculative bounds")
			}
			operation := transport.next(t)
			hash, err := document.Snapshot().Hash()
			if err != nil {
				t.Fatal(err)
			}
			receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: operation.OperationID, Code: "precondition_failed", Message: "controlled conflict", Revision: 0, MapHash: hash})
			runSelectionJob(t, app)
			e.ProcessCollaborationUpdates()
			if got := grab.Bounds(); got != before {
				t.Fatalf("rejected transform left bounds %v; want %v", got, before)
			}
			if app.commands.HasUndoV(e.Dmm().Path.Absolute) {
				t.Fatal("rejected transform created undo")
			}
		})
	}
}

func TestSelectionNetworkConsecutiveRejectedTransformsRestoreOrigin(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "in order", true: "reversed callbacks"}[reverse], func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			grab := activateSelectionWorkspace(t, ws)
			grab.Reset()
			grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
			network, transport, document := selectionNetwork(t, ws)
			origin := grab.Bounds()
			e := ws.Map().Editor()
			if err := grab.Rotate(true, e.RotateSelection); err != nil {
				t.Fatal(err)
			}
			first := transport.next(t)
			if err := grab.Nudge(util.Point{X: 1}); err != nil {
				t.Fatal(err)
			}
			second := transport.next(t)
			latest := grab.Bounds()
			hash, err := document.Snapshot().Hash()
			if err != nil {
				t.Fatal(err)
			}
			order := []model.Operation{first, second}
			if reverse {
				order[0], order[1] = order[1], order[0]
			}
			for index, operation := range order {
				receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: operation.OperationID, Code: "precondition_failed", Message: "controlled conflict", Revision: 0, MapHash: hash})
				runSelectionJob(t, app)
				e.ProcessCollaborationUpdates()
				if index == 0 && !reverse && grab.Bounds() != latest {
					t.Fatal("older rejection replaced the newer pending transform geometry")
				}
			}
			if grab.Bounds() != origin {
				t.Fatalf("rejections restored %v, want original %v", grab.Bounds(), origin)
			}
		})
	}
}

func TestSelectionNetworkOutcomeCannotReplaceNewSelection(t *testing.T) {
	for _, reset := range []string{"new selection", "deselected", "inactive tab"} {
		t.Run(reset, func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			grab := activateSelectionWorkspace(t, ws)
			network, transport, document := selectionNetwork(t, ws)
			e := ws.Map().Editor()
			if err := grab.Nudge(util.Point{X: 1}); err != nil {
				t.Fatal(err)
			}
			operation := transport.next(t)
			grab.Reset()
			switch reset {
			case "new selection":
				grab.SelectArea([]util.Point{{X: 3, Y: 3, Z: 1}})
			case "inactive tab":
				ws.Map().OnDeactivate()
			}
			want, selected := grab.Bounds(), grab.HasSelectedArea()
			acceptSelection(t, network, document, operation)
			runSelectionJob(t, app)
			e.ProcessCollaborationUpdates()
			app.commands.UndoV(e.Dmm().Path.Absolute)
			acceptSelection(t, network, document, transport.next(t))
			runSelectionJob(t, app)
			e.ProcessCollaborationUpdates()
			if grab.Bounds() != want || grab.HasSelectedArea() != selected {
				t.Fatal("old acceptance or undo replaced the newer selection context")
			}
		})
	}
}

func TestSelectionTransformUndoRedoRestoresBounds(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	grab.Reset()
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	e := ws.Map().Editor()
	before := grab.Bounds()
	if err := grab.Rotate(true, e.RotateSelection); err != nil {
		t.Fatal(err)
	}
	rotated := grab.Bounds()
	if err := grab.Nudge(util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	nudged := grab.Bounds()
	settleSelectionMove(t, ws, app)
	for _, want := range []util.Bounds{rotated, before} {
		app.commands.UndoV(e.Dmm().Path.Absolute)
		if got := grab.Bounds(); got != want {
			t.Fatalf("undo bounds %v; want %v", got, want)
		}
	}
	for _, want := range []util.Bounds{rotated, nudged} {
		app.commands.RedoV(e.Dmm().Path.Absolute)
		if got := grab.Bounds(); got != want {
			t.Fatalf("redo bounds %v; want %v", got, want)
		}
	}
}
