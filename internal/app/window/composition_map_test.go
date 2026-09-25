package window_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/mapping"
	mappingui "sdmm/internal/aphelion/mapping/ui"
	"sdmm/internal/app/render"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type nativeMapComposition struct {
	hub      *mappingui.Hub
	ws       *wsmap.WsMap
	captures int
}

func (a *mouseNetworkApp) ActiveMappingPath() string {
	if a.composition == nil {
		return ""
	}
	return a.composition.ws.Map().Dmm().Path.Absolute
}
func (*mouseNetworkApp) DoLoadResource(string)        {}
func (a *mouseNetworkApp) MappingDocuments() []string { return []string{a.ActiveMappingPath()} }
func (a *mouseNetworkApp) MappingRevisionKey([]string) string {
	g, r := a.composition.ws.Map().Editor().SaveVersion()
	return fmt.Sprintf("%d:%d", g, r)
}
func (a *mouseNetworkApp) CaptureMappingSources() map[string]mapping.AcceptedSource {
	a.composition.captures++
	e := a.composition.ws.Map().Editor()
	snapshot, version, err := e.CaptureSaveSnapshot(context.Background())
	return map[string]mapping.AcceptedSource{a.ActiveMappingPath(): {Snapshot: snapshot, Generation: version.Generation, Err: err, Current: func() bool { return e.SaveCaptureReady(version) }}}
}
func (*mouseNetworkApp) FrameMappingSource(string, util.Point) {}
func (a *mouseNetworkApp) MoveMappingRoot(root mapping.Root, to util.Point, check bool) error {
	e := a.composition.ws.Map().Editor()
	if check {
		_, err := e.CompositionRoot(root, to)
		return err
	}
	return e.MoveCompositionRoot(root, to)
}
func (*mouseNetworkApp) OpenMappingContext(string, string, mapping.Transform) {}
func (*mouseNetworkApp) OpenMappingComparison(*mappingui.Panel)               {}
func (a *mouseNetworkApp) CompositionInput(path string, point util.Point, active, pressed, released, cancel, focused bool, tool string) bool {
	if a.composition == nil {
		return false
	}
	return a.composition.hub.Handle(path, point, active, pressed, released, cancel, focused, tool)
}
func (a *mouseNetworkApp) CompositionTileLocked(path string, point util.Point) bool {
	return a.composition != nil && a.composition.hub.TileLocked(path, point)
}
func (a *mouseNetworkApp) CompositionEditFence(path string) func(util.Point) bool {
	if a.composition == nil {
		return nil
	}
	return a.composition.hub.EditFence(path)
}
func (a *mouseNetworkApp) CancelCompositionDraft(path string) {
	if a.composition != nil {
		a.composition.hub.CancelDraft(path)
	}
}

func TestNativeMapCompositionAnchorDraftCommitUndo(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	app.composition = &nativeMapComposition{ws: ws}
	hub := mappingui.NewHub(app)
	app.composition.hub = hub
	t.Cleanup(hub.Invalidate)
	for name, data := range map[string]string{"modules.toml": "directory = \"\"\n[rooms.room]\nmodules = [\"module.dmm\"]\n", "module.dmm": "\"a\" = (/obj/modular_map_connector,/obj/foo)\n(1,1,1) = {\"\na\n\"}\n"} {
		if err := os.WriteFile(filepath.Join(app.environment.RootDir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	instance := e.Dmm().Tiles[0].Instances()[2]
	vars := dmvars.Set(dmvars.Set(instance.Prefab().Vars(), "key", "\"room\""), "config_file", "\"modules.toml\"")
	e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, "/obj/modular_map_root", vars))
	e.CommitOperation("Native root fixture")
	before, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hub.Open()
	tools.SetSelected(tools.TNPick)
	baseFrame := mouseWorkspaceFrame(t, ws, app.mouse, func() {
		camera := *ws.Map().Canvas().Render().Camera
		camera.Level = ws.Map().ActiveLevel()
		hub.Draw(app.ActiveMappingPath(), camera, imgui.Vec2{X: 128, Y: 128}, imgui.Vec2{})
	})
	frame := func(down bool, x, y int) {
		hub.Advance()
		baseFrame(down, x, y)
		hub.ProcessLevelBuildBudget(render.NewLevelBuildBudget())
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		frame(false, 1, 1)
		frame(true, 1, 1)
		frame(false, 1, 1)
		if _, ok := hub.SelectedRoot(app.ActiveMappingPath()); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("root was not selectable on ordinary map input")
		}
		time.Sleep(time.Millisecond)
	}
	root, _ := hub.SelectedRoot(app.ActiveMappingPath())
	if root.StableID != instance.StableID() {
		t.Fatal("wrong source selected")
	}
	for !hub.ArmAnchorMove(app.ActiveMappingPath()) {
		frame(false, 1, 1)
		if time.Now().After(deadline) {
			t.Fatal("anchor action unavailable")
		}
		time.Sleep(time.Millisecond)
	}
	frame(true, 1, 1)
	frame(true, 3, 2)
	captures := app.composition.captures
	var samples []time.Duration
	for n := 0; n < 120; n++ {
		started := time.Now()
		frame(true, 2+n%2, 2)
		samples = append(samples, time.Since(started))
	}
	if app.composition.captures != captures {
		t.Fatal("draft movement queued source snapshots")
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("hidden native draft: samples=%d p50=%s p95=%s p99=%s source_snapshot_requests=%d", len(samples), samples[60], samples[114], samples[118], app.composition.captures-captures)
	during, _ := e.SaveSnapshot(context.Background())
	if !reflect.DeepEqual(before, during) {
		t.Fatal("draft motion changed accepted source")
	}
	imgui.CurrentIO().KeyPress(int(glfw.KeyEscape))
	frame(true, 3, 2)
	imgui.CurrentIO().KeyRelease(int(glfw.KeyEscape))
	frame(false, 3, 2)
	cancelled, _ := e.SaveSnapshot(context.Background())
	if !reflect.DeepEqual(before, cancelled) {
		t.Fatal("Escape changed source")
	}
	hub.ArmAnchorMove(app.ActiveMappingPath())
	frame(true, 1, 1)
	frame(true, 3, 2)
	frame(false, 3, 2)
	after, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision+1 || instance.Coord() != (util.Point{X: 3, Y: 2, Z: 1}) {
		t.Fatal("release did not move exactly one root", after.Revision, instance.Coord())
	}
	app.commands.UndoV(ws.CommandStackId())
	frame(false, 3, 2)
	undone, _ := e.SaveSnapshot(context.Background())
	if !reflect.DeepEqual(before.Tiles, undone.Tiles) {
		t.Fatal("Undo did not restore exact source")
	}
	app.commands.RedoV(ws.CommandStackId())
	frame(false, 3, 2)
	redone, _ := e.SaveSnapshot(context.Background())
	if !reflect.DeepEqual(after.Tiles, redone.Tiles) {
		t.Fatal("Redo changed source contents")
	}
	hub.SetVisible(false)
	hub.Advance()
	if selected, ok := hub.SelectedRoot(app.ActiveMappingPath()); !ok || selected.ID != root.ID {
		t.Fatal("sidebar hid root selection")
	}
	window.DrainFrameJobsForTest()
}
