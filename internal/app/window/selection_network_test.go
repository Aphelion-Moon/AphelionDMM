package window_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"
	"github.com/coder/websocket"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/app/command"
	"sdmm/internal/app/config"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type mouseNetworkApp struct {
	wsmap.App
	environment *dmenv.Dme
	commands    *command.Storage
	queued      chan struct{}
	mouse       func(uint, uint)
	errors      []error
}

func (a *mouseNetworkApp) LoadedEnvironment() *dmenv.Dme    { return a.environment }
func (a *mouseNetworkApp) CommandStorage() *command.Storage { return a.commands }
func (*mouseNetworkApp) Prefs() prefs.Prefs {
	return prefs.Prefs{Editor: prefs.Editor{SaveFormat: prefs.SaveFormatDMM}}
}
func (*mouseNetworkApp) PathsFilter() *dm.PathsFilter                                          { return dm.NewPathsFilterEmpty() }
func (a *mouseNetworkApp) RunLater(job func())                                                 { window.RunLater(job); a.queued <- struct{}{} }
func (*mouseNetworkApp) ConfigRegister(config.Config)                                          {}
func (a *mouseNetworkApp) AddMouseChangeCallback(cb func(uint, uint)) int                      { a.mouse = cb; return 0 }
func (a *mouseNetworkApp) RemoveMouseChangeCallback(int)                                       { a.mouse = nil }
func (*mouseNetworkApp) SyncPrefabs()                                                          {}
func (*mouseNetworkApp) SyncVarEditor()                                                        {}
func (*mouseNetworkApp) PublishCollaborationPresence(model.Coord, *protocol.PresenceSelection) {}
func (a *mouseNetworkApp) ReportCollaborationError(_ string, err error) {
	a.errors = append(a.errors, err)
}

func newMouseNetworkWorkspace(t *testing.T) (*wsmap.WsMap, *mouseNetworkApp) {
	t.Helper()
	if lifecycleWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for native mouse/network checks")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	lifecycleWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)
	ctx := imgui.CreateContext(nil)
	t.Cleanup(ctx.Destroy)
	// Keep a final queue drain inside the native context even on setup failure.
	t.Cleanup(window.DrainFrameJobsForTest)
	dir := t.TempDir()
	path := filepath.Join(dir, "map.dmm")
	if err := os.WriteFile(path, []byte("\"a\" = (/area/foo,/turf/foo,/obj/foo{dir = 2})\n(1,1,1) = {\"\naaaa\naaaa\naaaa\naaaa\n\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	objects := make(map[string]*dmenv.Object)
	for _, p := range []string{"/world", "/area/foo", "/turf/foo", "/obj/foo"} {
		vars := &dmvars.MutableVariables{}
		vars.Put("dir", "2")
		if p == "/world" {
			vars.Put("area", "/area/foo")
			vars.Put("turf", "/turf/foo")
			vars.Put("icon_size", "32")
		}
		objects[p] = &dmenv.Object{Path: p, Vars: vars.ToImmutable()}
	}
	environment := &dmenv.Dme{RootDir: dir, Objects: objects}
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	dmmap.Init(environment)
	t.Cleanup(dmmap.Free)
	data, err := dmmdata.New(path)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := dmmap.New(environment, data, path)
	a := &mouseNetworkApp{environment: environment, commands: command.NewStorage(), queued: make(chan struct{}, 8)}
	a.commands.SetStack(path)
	ws := wsmap.New(a, m)
	t.Cleanup(func() { ws.Map().OnDeactivate(); ws.Dispose(); window.DrainFrameJobsForTest() })
	ws.Map().OnActivate()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	return ws, a
}

type mouseNetworkTransport struct{ sent chan protocol.ClientEnvelope }

func (*mouseNetworkTransport) Connect(context.Context, protocol.JoinRequest, func(protocol.ServerEnvelope)) error {
	return nil
}
func (tr *mouseNetworkTransport) Send(_ context.Context, message protocol.ClientEnvelope) error {
	tr.sent <- message
	return nil
}
func (*mouseNetworkTransport) Close(websocket.StatusCode, string) error { return nil }
func (tr *mouseNetworkTransport) next(t *testing.T) model.Operation {
	t.Helper()
	select {
	case message := <-tr.sent:
		var payload protocol.OperationSubmitPayload
		if err := json.Unmarshal(message.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Operation
	case <-time.After(3 * time.Second):
		t.Fatal("mouse operation was not sent")
	}
	return model.Operation{}
}

func TestMouseDragWithDelayedSelectionOutcome(t *testing.T) {
	for _, scenario := range []struct {
		name                       string
		rejectRotation, rejectMove bool
	}{
		{name: "accepted"},
		{name: "rotation_rejected_during_drag", rejectRotation: true},
		{name: "move_rejected_after_release", rejectMove: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ws, app := newMouseNetworkWorkspace(t)
			e := ws.Map().Editor()
			initial, err := e.CollaborationSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			hash := func(snapshot model.Snapshot) string {
				t.Helper()
				value, err := snapshot.Hash()
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			displaySnapshot := func() model.Snapshot {
				t.Helper()
				snapshot, err := mapadapter.Import(e.Dmm(), initial.DocumentID, initial.EnvironmentHash)
				if err != nil {
					t.Fatal(err)
				}
				return snapshot
			}
			displayHash := func() string { return hash(displaySnapshot()) }
			actor, err := model.NewActorID()
			if err != nil {
				t.Fatal(err)
			}
			transport := &mouseNetworkTransport{sent: make(chan protocol.ClientEnvelope, 8)}
			network, err := client.NewNetworkExecutor(transport, initial, actor, "mouse-verification")
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
			grab := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
			grab.Reset()
			grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 2, Z: 1}})
			origin := grab.Bounds()

			frame := mouseWorkspaceFrame(t, ws, app.mouse)
			outcome := func(operation model.Operation, reject bool) {
				t.Helper()
				var payload any
				kind := protocol.ServerOperationAccepted
				if reject {
					kind = protocol.ServerOperationRejected
					payload = protocol.OperationRejectedPayload{OperationID: operation.OperationID, Code: "precondition_failed", Message: "verification conflict", Revision: document.Snapshot().Revision, MapHash: hash(document.Snapshot())}
				} else {
					accepted, err := document.Apply(operation, time.Unix(0, int64(document.Snapshot().Revision+1)))
					if err != nil {
						t.Fatal(err)
					}
					payload = protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash(document.Snapshot())}
				}
				data, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				if err := network.Receive(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "mouse-verification", SessionID: "mouse-verification", Type: kind, Payload: data}); err != nil {
					t.Fatal(err)
				}
				select {
				case <-app.queued:
				case <-time.After(3 * time.Second):
					t.Fatal("completion not queued")
				}
			}
			frame(false, 1, 1)
			frame(false, 1, 1)
			if err := grab.Rotate(true, e.RotateSelection); err != nil {
				t.Fatal(err)
			}
			rotation := transport.next(t)
			rotated := grab.Bounds()
			rotatedSnapshot := displaySnapshot()
			rotatedHash := hash(rotatedSnapshot)
			frame(true, 1, 1)
			frame(true, 1, 1)
			if grab.Stale() {
				t.Fatal("mouse press did not start Grab")
			}
			frame(true, 2, 2)
			previewBounds, previewHash := grab.Bounds(), displayHash()
			if previewBounds != rotated.Plus(1, 1) || previewHash == rotatedHash {
				t.Fatal("mouse movement did not move the rectangular selection")
			}
			outcome(rotation, scenario.rejectRotation)
			frame(true, 2, 2)
			e.ProcessCollaborationUpdates()
			if grab.Stale() || grab.Bounds() != previewBounds || displayHash() != previewHash {
				t.Fatal("older outcome overwrote an open mouse preview")
			}
			if _, err := e.SaveSnapshot(context.Background()); err == nil {
				t.Fatal("open mouse preview became saveable")
			}
			frame(false, 2, 2)
			frame(false, 2, 2)
			if !grab.Stale() {
				t.Fatal("mouse release did not finish Grab")
			}
			if scenario.rejectRotation {
				// Its rotated before-state is now stale. The executor must retain
				// this dependent drag as an unsent draft, without sending it.
				select {
				case <-app.queued:
				case <-time.After(3 * time.Second):
					t.Fatal("unsent drag completion not queued")
				}
				if len(transport.sent) != 0 {
					t.Fatal("stale dependent drag was sent")
				}
			} else {
				outcome(transport.next(t), scenario.rejectMove)
			}
			frame(false, 2, 2)
			e.ProcessCollaborationUpdates()
			assertState := func(wantHash string, wantBounds util.Bounds) {
				t.Helper()
				snapshot, err := e.SaveSnapshot(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if hash(snapshot) != wantHash || displayHash() != wantHash || grab.Bounds() != wantBounds {
					t.Fatalf("map/authority/selection mismatch: bounds %v want %v", grab.Bounds(), wantBounds)
				}
			}
			path := e.Dmm().Path.Absolute
			if scenario.rejectRotation {
				assertState(hash(initial), origin)
				if app.commands.HasUndoV(path) || len(network.Conflicts()) != 2 || len(app.errors) != 2 {
					t.Fatal("rejected mouse chain lost drafts or entered undo history")
				}
				if !errors.Is(app.errors[0], client.ErrOperationRejected) || !strings.Contains(app.errors[1].Error(), "precondition failed") {
					t.Fatalf("unexpected rejection errors: %v", app.errors)
				}
				conflicts := network.Conflicts()
				if conflicts[0].OperationID != rotation.OperationID || conflicts[1].Code != "submission_failed" {
					t.Fatal("rejection lost operation provenance")
				}
				// Reconstruct the user's exact preview from the retained drag,
				// proving that stable IDs and every changed tile survived rollback.
				recovered := model.CloneSnapshot(rotatedSnapshot)
				for _, change := range conflicts[1].Draft.Changes {
					found := false
					for i, tile := range recovered.Tiles {
						if tile.Coord == change.Coord {
							if !tile.State.Equal(change.Before) {
								t.Fatal("draft lost rotated before-state")
							}
							recovered.Tiles[i].State = model.CloneTileState(change.After)
							found = true
							break
						}
					}
					if !found {
						t.Fatal("draft targets an unknown tile")
					}
				}
				if hash(recovered) != previewHash {
					t.Fatal("retained draft lost mouse preview contents")
				}
				return
			}
			if scenario.rejectMove {
				assertState(rotatedHash, rotated)
				if len(network.Conflicts()) != 1 || len(app.errors) != 1 || !errors.Is(app.errors[0], client.ErrOperationRejected) {
					t.Fatal("released drag rejection lost its conflict or error")
				}
				app.commands.UndoV(path)
				outcome(transport.next(t), false)
				frame(false, 2, 2)
				e.ProcessCollaborationUpdates()
				assertState(hash(initial), origin)
				if app.commands.HasUndoV(path) {
					t.Fatal("rejected released drag entered undo history")
				}
				app.commands.RedoV(path)
				outcome(transport.next(t), false)
				frame(false, 2, 2)
				e.ProcessCollaborationUpdates()
				assertState(rotatedHash, rotated)
				return
			}
			assertState(previewHash, previewBounds)
			for _, expected := range []struct {
				hash   string
				bounds util.Bounds
			}{{rotatedHash, rotated}, {hash(initial), origin}} {
				app.commands.UndoV(path)
				outcome(transport.next(t), false)
				frame(false, 2, 2)
				e.ProcessCollaborationUpdates()
				assertState(expected.hash, expected.bounds)
			}
			for _, expected := range []struct {
				hash   string
				bounds util.Bounds
			}{{rotatedHash, rotated}, {previewHash, previewBounds}} {
				app.commands.RedoV(path)
				outcome(transport.next(t), false)
				frame(false, 2, 2)
				e.ProcessCollaborationUpdates()
				assertState(expected.hash, expected.bounds)
			}
			if len(app.errors) != 0 {
				t.Fatalf("unexpected errors: %v", app.errors)
			}
		})
	}
}

// Mirror startFrame ordering and GLFW frame-end callback delivery.
func mouseWorkspaceFrame(t *testing.T, ws *wsmap.WsMap, mouse func(uint, uint)) func(bool, int, int) {
	pane := ws.Map()
	primed := false
	return func(down bool, x, y int) {
		t.Helper()
		io := imgui.CurrentIO()
		pos := imgui.Vec2{X: float32((x-1)*32 + 16), Y: float32(128 - ((y-1)*32 + 16))}
		io.SetMousePosition(pos)
		io.SetMouseButtonDown(0, down)
		shortcut.BeginFrame()
		imgui.NewFrame()
		window.DrainFrameJobsForTest()
		window.RunRepeatJobsForTest()
		imgui.SetNextWindowPos(imgui.Vec2{})
		imgui.SetNextWindowSize(imgui.Vec2{X: 128, Y: 128})
		imgui.BeginV("Mouse network canvas", nil, imgui.WindowFlagsNoTitleBar|imgui.WindowFlagsNoResize|imgui.WindowFlagsNoMove|imgui.WindowFlagsNoScrollbar)
		pane.CanvasControl().Process(imgui.Vec2{X: 128, Y: 128})
		pane.Canvas().Process(imgui.Vec2{X: 128, Y: 128})
		imgui.End()
		imgui.Render()
		mouse(uint(pos.X), uint(pos.Y))
		if primed && pane.CanvasState().HoveredTile() != (util.Point{X: x, Y: y, Z: 1}) {
			t.Fatalf("screen input did not reach tile %d,%d: %v", x, y, pane.CanvasState().HoveredTile())
		}
		primed = true // ImGui hover uses the previous frame's window bounds.
	}
}
