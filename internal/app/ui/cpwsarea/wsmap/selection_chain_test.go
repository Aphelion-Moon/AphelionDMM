package wsmap

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/util"
)

func TestSelectionNetworkRectangularChainDuringPreview(t *testing.T) {
	for _, test := range []struct {
		name     string
		accepted int
	}{{"both accepted", 2}, {"rotation accepted nudge rejected", 1}, {"both rejected", 0}} {
		t.Run(test.name, func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			grab := activateSelectionWorkspace(t, ws)
			grab.Reset()
			grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 2, Z: 1}})
			network, transport, document := selectionNetwork(t, ws)
			e := ws.Map().Editor()
			initial := document.Snapshot()
			initialHash := resizeHash(t, initial)
			origin := grab.Bounds()
			if err := grab.Rotate(true, e.RotateSelection); err != nil {
				t.Fatal(err)
			}
			rotation := transport.next(t)
			rotated := grab.Bounds()
			if rotated != (util.Bounds{X1: 1, Y1: 1, X2: 2, Y2: 3}) {
				t.Fatal("fixture did not rotate rectangular bounds")
			}
			if err := grab.Nudge(util.Point{X: 1}); err != nil {
				t.Fatal(err)
			}
			nudge := transport.next(t)
			nudged := grab.Bounds()
			move, err := e.BeginSelectionMove(nudged, 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.PreviewSelectionMove(move, util.Point{Y: 1}); err != nil {
				t.Fatal(err)
			}
			preview := e.Dmm().Copy()
			for index, operation := range []model.Operation{rotation, nudge} {
				if index < test.accepted {
					acceptSelection(t, network, document, operation)
				} else {
					current := document.Snapshot()
					receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: operation.OperationID, Code: "precondition_failed", Message: "controlled chain conflict", Revision: current.Revision, MapHash: resizeHash(t, current)})
				}
				runSelectionJob(t, app)
				e.ProcessCollaborationUpdates()
				if !reflect.DeepEqual(e.Dmm().Copy(), preview) {
					t.Fatal("older transform outcome replaced the open preview")
				}
				if _, err := e.SaveSnapshot(context.Background()); err == nil {
					t.Fatal("open preview became saveable")
				}
			}
			e.FinishSelectionMove(move, true)
			e.ProcessCollaborationUpdates()
			assertState := func(want model.Snapshot, bounds util.Bounds) {
				t.Helper()
				actual := resizeSnapshot(t, e)
				display, err := mapadapter.Import(e.Dmm(), actual.DocumentID, actual.EnvironmentHash)
				if err != nil || resizeHash(t, actual) != resizeHash(t, want) || !reflect.DeepEqual(display.Tiles, actual.Tiles) || grab.Bounds() != bounds {
					t.Fatalf("map/display/selection diverged: bounds=%v want=%v err=%v", grab.Bounds(), bounds, err)
				}
			}
			if len(network.Conflicts()) != 2-test.accepted || len(app.errors) != 2-test.accepted {
				t.Fatal("chain outcomes lost rejected intent or error reports")
			}
			if test.accepted == 0 {
				assertState(initial, origin)
				if len(network.Conflicts()) != 2 || len(app.errors) != 2 || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
					t.Fatal("rejected chain lost drafts or entered history")
				}
				return
			}
			final := document.Snapshot()
			undoBounds, redoBounds := []util.Bounds{origin}, []util.Bounds{rotated}
			if test.accepted == 2 {
				undoBounds, redoBounds = []util.Bounds{rotated, origin}, []util.Bounds{rotated, nudged}
			}
			assertState(final, redoBounds[len(redoBounds)-1])
			for _, bounds := range undoBounds {
				app.commands.UndoV(e.Dmm().Path.Absolute)
				acceptSelection(t, network, document, transport.next(t))
				runSelectionJob(t, app)
				e.ProcessCollaborationUpdates()
				assertState(document.Snapshot(), bounds)
			}
			if resizeHash(t, document.Snapshot()) != initialHash {
				t.Fatal("chain undo lost original map contents or stable IDs")
			}
			for _, bounds := range redoBounds {
				app.commands.RedoV(e.Dmm().Path.Absolute)
				acceptSelection(t, network, document, transport.next(t))
				runSelectionJob(t, app)
				e.ProcessCollaborationUpdates()
				assertState(document.Snapshot(), bounds)
			}
			if resizeHash(t, document.Snapshot()) != resizeHash(t, final) || len(app.errors) != 2-test.accepted {
				t.Fatal("chain redo differs from accepted state")
			}
		})
	}
}
