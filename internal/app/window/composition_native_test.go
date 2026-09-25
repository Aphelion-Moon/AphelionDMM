package window_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/mapping"
	mappingui "sdmm/internal/aphelion/mapping/ui"
	"sdmm/internal/app/render"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

type inspectorNativeApp struct {
	e           *editor.Editor
	environment *dmenv.Dme
	opened      string
}

func (a *inspectorNativeApp) LoadedEnvironment() *dmenv.Dme { return a.environment }
func (a *inspectorNativeApp) ActiveMappingPath() string     { return a.e.Dmm().Path.Absolute }
func (a *inspectorNativeApp) DoLoadResource(path string)    { a.opened = path }
func (a *inspectorNativeApp) MappingRevisionKey([]string) string {
	g, r := a.e.SaveVersion()
	return fmt.Sprintf("%d:%d", g, r)
}
func (a *inspectorNativeApp) CaptureMappingSources() map[string]mapping.AcceptedSource {
	handle, version, err := a.e.CaptureSaveSnapshot(context.Background())
	return map[string]mapping.AcceptedSource{a.ActiveMappingPath(): {Snapshot: handle, Generation: version.Generation, Err: err, Current: func() bool { return a.e.SaveCaptureReady(version) }}}
}

func TestNativeInspectorAcceptedSourceConflictUndoAndSave(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared=%t", shared), func(t *testing.T) {
			ws, app := newMouseNetworkWorkspace(t)
			e := ws.Map().Editor()
			input, err := os.ReadFile(e.Dmm().Path.Absolute)
			if err != nil {
				t.Fatal(err)
			}
			parent := filepath.Join(t.TempDir(), "locked-parent.dmm")
			if err = os.WriteFile(parent, input, 0600); err != nil {
				t.Fatal(err)
			}
			initial, err := e.SaveSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var network *client.NetworkExecutor
			var transport *mouseNetworkTransport
			var authority *engine.Document
			if shared {
				actor, err := model.NewActorID()
				if err != nil {
					t.Fatal(err)
				}
				transport = &mouseNetworkTransport{sent: make(chan protocol.ClientEnvelope, 8)}
				network, err = client.NewNetworkExecutor(transport, initial, actor, "source-context")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { network.Terminate(nil) })
				if err = e.AttachCollaborationExecutor(network); err != nil {
					t.Fatal(err)
				}
				authority, err = engine.NewDocument(initial)
				if err != nil {
					t.Fatal(err)
				}
			}
			bridge := &inspectorNativeApp{e: e, environment: app.environment}
			panel := mappingui.New(bridge)
			panel.OpenSources(parent, e.Dmm().Path.Absolute, nil)
			panel.OpenReferenceForEditing()
			t.Cleanup(func() { panel.Invalidate(); window.DrainFrameJobsForTest() })
			if bridge.opened != e.Dmm().Path.Absolute {
				t.Fatal("wrong source opened for editing")
			}
			frame := func() {
				window.DrainFrameJobsForTest()
				e.ProcessCollaborationUpdates()
				for len(app.queued) > 0 {
					<-app.queued
				}
				imgui.CurrentIO().SetDeltaTime(1.0 / 60)
				imgui.NewFrame()
				panel.Process()
				panel.ProcessLevelBuildBudget(render.NewLevelBuildBudget())
				imgui.Render()
			}
			settle := func() {
				deadline := time.Now().Add(5 * time.Second)
				for {
					frame()
					texture, visible := panel.Backdrop(e.Dmm().Path.Absolute, render.Camera{Scale: 1, Level: 1}, imgui.Vec2{X: 128, Y: 128})
					if visible && texture != 0 {
						return
					}
					if time.Now().After(deadline) {
						t.Fatal("accepted context did not become usable")
					}
					time.Sleep(time.Millisecond)
				}
			}
			settle()
			outcome := func(reject bool) {
				if !shared {
					return
				}
				operation := transport.next(t)
				kind := protocol.ServerOperationAccepted
				var payload any
				if reject {
					kind = protocol.ServerOperationRejected
					hash, _ := authority.Snapshot().Hash()
					payload = protocol.OperationRejectedPayload{OperationID: operation.OperationID, Code: "precondition_failed", Message: "source context fixture conflict", Revision: authority.Snapshot().Revision, MapHash: hash}
				} else {
					accepted, err := authority.Apply(operation, time.Now())
					if err != nil {
						t.Fatal(err)
					}
					hash, _ := authority.Snapshot().Hash()
					payload = protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash}
				}
				data, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				if err = network.Receive(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "context-outcome", SessionID: "source-context", Type: kind, Payload: data}); err != nil {
					t.Fatal(err)
				}
				frame()
			}
			edit := func() {
				instance := e.Dmm().Tiles[0].Instances()[2]
				e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "4")))
				e.CommitOperation("Context source direction")
			}
			if shared {
				edit()
				if _, _, err = e.CaptureSaveSnapshot(context.Background()); err == nil {
					t.Fatal("pending shared source published as accepted")
				}
				outcome(true)
			}
			edit()
			outcome(false)
			panel.OpenSources(parent, e.Dmm().Path.Absolute, nil)
			settle()
			if !saveWorkspaceAsync(t, ws) {
				t.Fatal("source Save failed with inspector open")
			}
			app.commands.UndoV(e.Dmm().Path.Absolute)
			outcome(false)
			panel.OpenSources(parent, e.Dmm().Path.Absolute, nil)
			settle()
			if !saveWorkspaceAsync(t, ws) {
				t.Fatal("source Undo/Save failed with inspector open")
			}
			if got, err := os.ReadFile(parent); err != nil || string(got) != string(input) {
				t.Fatal("locked parent was modified", err)
			}
			if value, _ := e.Dmm().Tiles[0].Instances()[2].Prefab().Vars().Value("dir"); value != "2" {
				t.Fatal("source undo failed", value)
			}
		})
	}
}
