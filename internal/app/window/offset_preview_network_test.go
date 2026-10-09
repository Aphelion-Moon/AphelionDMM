package window_test

import (
	"encoding/json"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/app/ui/cpvareditor"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// A session-attached (presentation) executor publishes accepted operations back
// through installPresentationUpdate. Restoring a pixel offset to the value the
// map was opened with must still move the rendered unit, both when the revert
// is acknowledged before the next edit and when acknowledgements lag behind.
func TestVariableEditorOffsetRevertRefreshesRenderedUnitsWithNetworkExecutor(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		acknowledge func(step int) bool
	}{
		{"acknowledged each edit", func(int) bool { return true }},
		{"acknowledgements lag", func(step int) bool { return step == 2 }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ws, app := newMouseNetworkWorkspace(t)
			e := ws.Map().Editor()
			initial, err := e.CollaborationSnapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			actor, err := model.NewActorID()
			if err != nil {
				t.Fatal(err)
			}
			transport := &mouseNetworkTransport{sent: make(chan protocol.ClientEnvelope, 16)}
			network, err := client.NewNetworkExecutor(transport, initial, actor, "offset-revert")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { network.Terminate(nil) })
			if err := e.AttachCollaborationExecutor(network); err != nil {
				t.Fatal(err)
			}
			document, err := engine.NewDocument(initial)
			if err != nil {
				t.Fatal(err)
			}
			r := ws.Map().Canvas().Render()
			frame := mouseWorkspaceFrame(t, ws, app.mouse)
			vars := cpvareditor.NewForVerification(&offsetVarApp{mouseNetworkApp: app, current: e})
			origin := util.Point{X: 1, Y: 1, Z: 1}
			instance := func() *dmminstance.Instance { return e.Dmm().GetTile(origin).Instances()[2] }
			settle := func() {
				t.Helper()
				for i := 0; i < 4; i++ {
					window.DrainFrameJobsForTest()
					e.ProcessCollaborationUpdates()
					frame(false, 1, 1)
				}
				window.DrainFrameJobsForTest()
			}
			acknowledge := func() {
				t.Helper()
				operation := transport.next(t)
				accepted, err := document.Apply(operation, time.Unix(0, int64(document.Snapshot().Revision+1)))
				if err != nil {
					t.Fatal(err)
				}
				hash, err := document.Snapshot().Hash()
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})
				if err != nil {
					t.Fatal(err)
				}
				if err := network.Receive(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "offset-revert", SessionID: "offset-revert", Type: protocol.ServerOperationAccepted, Payload: data}); err != nil {
					t.Fatal(err)
				}
			}
			assertAt := func(step string, wantX1 float32) {
				t.Helper()
				units := r.UnitBounds(1, instance().Id())
				if len(units) != 1 || units[0].X1 != wantX1 || units[0].Y1 != 0 {
					t.Fatalf("%s: units=%v, want one unit at (%v,0); pixel_x=%q", step, units, wantX1, instance().Prefab().Vars().ValueV("pixel_x", ""))
				}
			}

			for step, edit := range []struct {
				value string
				x1    float32
			}{{"40", 40}, {"", 0}, {"41", 41}, {"", 0}} {
				vars.SetInstanceVariableForVerification(instance(), "pixel_x", edit.value)
				settle()
				assertAt("local pixel_x="+edit.value, edit.x1)
				if scenario.acknowledge(step) {
					// Acknowledge every operation sent so far, in order.
					for len(transport.sent) != 0 {
						acknowledge()
					}
					settle()
					assertAt("acknowledged pixel_x="+edit.value, edit.x1)
				}
			}
			for len(transport.sent) != 0 {
				acknowledge()
			}
			settle()
			assertAt("all acknowledged, restored to opened value", 0)
			if got := instance().Prefab().Vars().ValueV("pixel_x", ""); got != "" {
				t.Fatalf("restored pixel_x = %q, want the opened (absent) value", got)
			}
			stats := e.CollaborationPublicationStats()
			t.Logf("publication stats: %+v", stats)
			if stats.Publications == 0 {
				t.Fatal("acknowledgements never reached the presentation publication path")
			}
			if len(app.errors) != 0 {
				t.Fatalf("unexpected errors: %v", app.errors)
			}
		})
	}
}
