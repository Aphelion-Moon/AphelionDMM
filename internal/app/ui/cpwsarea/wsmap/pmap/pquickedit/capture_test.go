package pquickedit

import (
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/prefs"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type capturePanelEditor struct {
	allow                        bool
	commits, updates, selections int
}

func (*capturePanelEditor) Dmm() *dmmap.Dmm                         { return &dmmap.Dmm{Name: "capture"} }
func (e *capturePanelEditor) TryBeginTileChange(...util.Point) bool { return e.allow }
func (e *capturePanelEditor) CommitOperation(string)                { e.commits++ }
func (e *capturePanelEditor) UpdateCanvasByCoords([]util.Point)     { e.updates++ }
func (e *capturePanelEditor) InstanceSelect(*dmminstance.Instance)  { e.selections++ }

type capturePanelApp struct {
	App
	environment *dmenv.Dme
}

func (*capturePanelApp) Prefs() prefs.Prefs {
	return prefs.Prefs{Editor: prefs.Editor{NudgeMode: prefs.SaveNudgeModePixel}}
}
func (a *capturePanelApp) LoadedEnvironment() *dmenv.Dme { return a.environment }

func TestQuickNudgeCaptureAndRelease(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		change, release bool
	}{
		{"failed change", false, false},
		{"failed release", true, false},
		{"valid", true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := imgui.CreateContext(nil)
			t.Cleanup(ctx.Destroy)
			io := imgui.CurrentIO()
			io.SetIniFilename("")
			io.SetDisplaySize(imgui.Vec2{X: 300, Y: 200})
			io.SetDeltaTime(1.0 / 60)
			io.Fonts().TextureDataAlpha8()
			dmmap.PrefabStorage.Free()
			t.Cleanup(dmmap.PrefabStorage.Free)
			base := dmvars.Set(dmvars.FromParent(nil), "pixel_x", "1")
			vars := dmvars.Set(dmvars.FromParent(base), "pixel_x", "0")
			instance := dmminstance.New(util.Point{X: 1, Y: 1, Z: 1}, dmmprefab.New(dmmprefab.IdNone, "/obj/test", vars))
			ed := &capturePanelEditor{allow: scenario.change}
			app := &capturePanelApp{environment: &dmenv.Dme{Objects: map[string]*dmenv.Object{"/obj/test": {Path: "/obj/test", Vars: base}}}}
			panel := New(app, ed)
			frame := func() imgui.Vec2 {
				imgui.NewFrame()
				imgui.SetNextWindowPos(imgui.Vec2{})
				imgui.SetNextWindowSize(imgui.Vec2{X: 300, Y: 200})
				imgui.BeginV("quick edit capture", nil, imgui.WindowFlagsNoTitleBar|imgui.WindowFlagsNoResize)
				panel.showNudgeOption("Nudge X", true, instance)
				lo, hi := imgui.ItemRectMin(), imgui.ItemRectMax()
				imgui.End()
				imgui.Render()
				return imgui.Vec2{X: lo.X + 5, Y: (lo.Y + hi.Y) / 2}
			}
			frame()
			point := frame()
			original := instance.Prefab()
			io.SetMousePosition(point)
			io.AddMouseWheelDelta(0, 1)
			frame()
			if panel.lastScrollEdit == 0 {
				t.Fatal("wheel did not enter the real nudge control")
			}
			if scenario.change {
				if instance.Prefab().Vars().IntV("pixel_x", -1) != 1 || ed.updates != 1 {
					t.Fatal("valid capture did not apply the nudge")
				}
			} else if instance.Prefab() != original || ed.updates != 0 {
				t.Fatal("failed capture changed the instance")
			}
			beforeRelease := instance.Prefab()
			if scenario.release {
				// Force the sanitized prefab's hash slot to belong to other content.
				id := dmmprefab.Id("/obj/test", dmvars.FromParent(base))
				dmmap.PrefabStorage.Put(dmmprefab.New(id, "/obj/other", &dmvars.Variables{}))
			}
			ed.allow = scenario.release
			panel.lastScrollEdit = time.Now().Add(-time.Second).UnixMilli()
			frame()
			if ed.commits != 1 || panel.lastScrollEdit != 0 {
				t.Fatal("release did not finish/report exactly once")
			}
			if !scenario.release {
				if instance.Prefab() != beforeRelease || ed.selections != 0 {
					t.Fatal("failed release sanitized or selected the instance")
				}
			} else if instance.Prefab() == beforeRelease || ed.selections != 1 {
				t.Fatal("valid release did not sanitize the inherited default")
			}
			if scenario.release {
				cached, ok := dmmap.PrefabStorage.GetById(instance.Prefab().Id())
				if !ok || cached != instance.Prefab() {
					t.Fatal("quick edit selected another prefab's colliding ID")
				}
			}
		})
	}
}
