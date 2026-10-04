package wsmap

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

func TestHeldRotationRegistryTargetsPasteAndRespectsText(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()

	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}})
	ws.Map().CanvasState().SetMousePosition(32, 0, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	input := "value"
	io := imgui.CurrentIO()
	for frame := 0; frame < 3; frame++ {
		if frame == 2 {
			io.KeyPress(int(glfw.KeyE))
		}
		imgui.NewFrame()
		imgui.Begin("Held rotation text owner")
		if frame == 0 {
			imgui.SetKeyboardFocusHere()
		}
		imgui.InputText("Value", &input)
		if frame == 2 {
			if !imgui.IsAnyItemActive() {
				t.Fatal("text fixture inactive")
			}
			shortcut.Process()
		}
		imgui.End()
		imgui.EndFrame()
	}
	io.KeyRelease(int(glfw.KeyE))
	imgui.NewFrame()
	imgui.EndFrame()
	pressSelectionShortcut(glfw.KeyQ)
	settlePastePreview(t, ws, app)
	pressSelectionShortcut(glfw.KeyE)
	settlePastePreview(t, ws, app)
	pressSelectionShortcut(glfw.KeyE)
	settlePastePreview(t, ws, app)
	if resizeSnapshot(t, e).Revision != 0 {
		t.Fatal("held rotation committed before placement")
	}
	grab = tools.Selected().(*tools.ToolGrab)
	if !grab.ConfirmPlacement() {
		t.Fatal("rotated paste not confirmed")
	}
	settlePastePreview(t, ws, app)
	tile := e.Dmm().GetTile(util.Point{X: 2, Y: 1, Z: 1})
	for _, instance := range tile.Instances() {
		if instance.Prefab().Path() == "/obj/foo" && instance.Prefab().Vars().ValueV("dir", "") != "8" {
			t.Fatal("paste did not use Q/E rotated data")
		}
	}
	if resizeSnapshot(t, e).Revision != 1 {
		t.Fatal("rotation and paste were not one operation")
	}
	for _, instance := range app.Clipboard().Buffer().Buffer[0].Instances() {
		if instance.Prefab().Path() == "/obj/foo" && instance.Prefab().Vars().ValueV("dir", "") != "2" {
			t.Fatal("rotation mutated clipboard")
		}
	}
}

func TestRotationDefaultsUseQEForSelectionWithoutBracketAliases(t *testing.T) {
	ws, _ := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	for _, key := range []glfw.Key{glfw.KeyLeftBracket, glfw.KeyRightBracket} {
		pressSelectionShortcut(key)
	}
	if resizeSnapshot(t, e).Revision != 0 {
		t.Fatal("old bracket bindings still rotate the selection")
	}
	for index, key := range []glfw.Key{glfw.KeyE, glfw.KeyQ} {
		pressSelectionShortcut(key)
		if int(resizeSnapshot(t, e).Revision) != index+1 {
			t.Fatal("Q/E did not produce exactly one selection rotation")
		}
		want := "8"
		if key == glfw.KeyQ {
			want = "2"
		}
		if got := e.Dmm().GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[2].Prefab().Vars().ValueV("dir", ""); got != want {
			t.Fatalf("Q/E selection direction=%s, want %s", got, want)
		}
	}
}

func TestDirectionalRotationShortcutSaveAndUndo(t *testing.T) {
	for _, base := range []string{"/obj/machinery/power/apc/auto_name", "/obj/machinery/airalarm", "/obj/machinery/firealarm"} {
		t.Run(base, func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			activateSelectionWorkspace(t, ws)
			e := ws.Map().Editor()
			family := base + "/directional"
			parent := &dmvars.MutableVariables{}
			parent.Put("abstract_type", family)
			app.environment.Objects[family] = &dmenv.Object{Path: family, Vars: parent.ToImmutable()}
			for _, direction := range []struct{ name, dir, x, y string }{{"north", "1", "0", "26"}, {"east", "4", "26", "0"}} {
				path := family + "/" + direction.name
				vars := &dmvars.MutableVariables{}
				vars.Put("dir", direction.dir)
				vars.Put("pixel_x", direction.x)
				vars.Put("pixel_y", direction.y)
				defaults := vars.ToImmutable()
				defaults.LinkParent(app.environment.Objects[family].Vars)
				app.environment.Objects[path] = &dmenv.Object{Path: path, Vars: defaults}
			}
			path := family + "/north"
			vars := dmvars.Set(dmvars.FromParent(app.environment.Objects[path].Vars), "name", "\"custom fixture\"")
			source := dmmprefab.New(dmmprefab.IdNone, path, vars)
			instance := e.Dmm().GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[2]
			id := instance.StableID()
			e.InstanceReplace(instance, source)
			e.CommitOperation("Prepare directional fixture")
			pressSelectionShortcut(glfw.KeyE)
			got := e.Dmm().GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[2]
			if got.Prefab().Path() != family+"/east" || got.StableID() != id {
				t.Fatal("shortcut lost directional path or identity")
			}
			if !saveForTest(t, ws, app.jobs) {
				t.Fatal("rotated variant did not save")
			}
			saved, err := dmmdata.New(e.Dmm().Path.Absolute)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, prefabs := range saved.Dictionary {
				for _, prefab := range prefabs {
					if prefab.Path() == family+"/east" {
						found = true
						if prefab.Vars().ValueV("name", "") != "\"custom fixture\"" {
							t.Fatal("save lost explicit override")
						}
						for _, name := range []string{"dir", "pixel_x", "pixel_y"} {
							if _, ok := prefab.Vars().ExplicitValue(name); ok {
								t.Fatal("saved redundant old orientation", name)
							}
						}
					}
				}
			}
			if !found {
				t.Fatal("save did not retain target variant path")
			}
			app.commands.UndoV(e.Dmm().Path.Absolute)
			got = e.Dmm().GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[2]
			if got.Prefab().Path() != path || got.StableID() != id {
				t.Fatal("undo lost source type or identity")
			}
		})
	}
}
