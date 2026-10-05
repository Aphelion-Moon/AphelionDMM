package pquickedit

import (
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func unknownQuickEditInstance() *dmminstance.Instance {
	vars := &dmvars.MutableVariables{}
	vars.Put("custom_value", "7")
	vars.Put("pixel_x", "0")
	vars.Put("dir", "4")
	return dmminstance.New(util.Point{X: 1, Y: 1, Z: 1}, dmmprefab.New(dmmprefab.IdNone, "/obj/audit_unknown", vars.ToImmutable()))
}

func assertUnknownQuickEditPreserved(t *testing.T, instance *dmminstance.Instance, original *dmmprefab.Prefab) {
	t.Helper()
	vars := instance.Prefab().Vars()
	if instance.Prefab() != original || instance.Prefab().Path() != "/obj/audit_unknown" ||
		vars.ValueV("custom_value", "") != "7" || vars.ValueV("pixel_x", "") != "0" || vars.ValueV("dir", "") != "4" {
		t.Fatal("quick edit changed or discarded an unknown instance")
	}
}

func TestQuickEditRefusesUnknownType(t *testing.T) {
	ctx := imgui.CreateContext(nil)
	t.Cleanup(ctx.Destroy)
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 300, Y: 200})
	io.SetDeltaTime(1.0 / 60)
	io.Fonts().TextureDataAlpha8()
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	instance := unknownQuickEditInstance()
	original := instance.Prefab()
	ed := &capturePanelEditor{allow: true}
	panel := New(&capturePanelApp{environment: &dmenv.Dme{Objects: map[string]*dmenv.Object{}}}, ed)
	if panel.unavailableReason(instance) == "" {
		t.Fatal("unknown type has no disabled reason")
	}
	frame := func() imgui.Vec2 {
		imgui.NewFrame()
		imgui.SetNextWindowPos(imgui.Vec2{})
		imgui.SetNextWindowSize(imgui.Vec2{X: 300, Y: 200})
		imgui.BeginV("quick edit unknown", nil, imgui.WindowFlagsNoTitleBar|imgui.WindowFlagsNoResize)
		// Exercise the control directly, bypassing ProcessV's disabled scope,
		// so the change and release guards are tested on their own.
		panel.showNudgeOption("Nudge X", true, instance)
		lo, hi := imgui.ItemRectMin(), imgui.ItemRectMax()
		imgui.End()
		imgui.Render()
		return imgui.Vec2{X: lo.X + 5, Y: (lo.Y + hi.Y) / 2}
	}

	func() {
		defer func() {
			if failure := recover(); failure != nil {
				t.Fatalf("quick edit of a preserved unknown instance panicked: %v", failure)
			}
		}()
		frame()
		point := frame()
		io.SetMousePosition(point)
		io.AddMouseWheelDelta(0, 1)
		frame()
		panel.lastScrollEdit = time.Now().Add(-time.Second).UnixMilli()
		frame()
		panel.sanitizeInstanceVar(instance, "dir", "0")
		panel.sanitizeInstanceVar(instance, "pixel_x", "0")
	}()

	assertUnknownQuickEditPreserved(t, instance, original)
	if ed.updates != 0 || ed.commits != 0 || ed.selections != 0 {
		t.Fatalf("refused quick edit touched the editor: updates=%d commits=%d selections=%d", ed.updates, ed.commits, ed.selections)
	}
}

func TestQuickEditKnownTypeHasNoDisabledReason(t *testing.T) {
	instance := unknownQuickEditInstance()
	app := &capturePanelApp{environment: &dmenv.Dme{Objects: map[string]*dmenv.Object{"/obj/audit_unknown": {Path: "/obj/audit_unknown", Vars: &dmvars.Variables{}}}}}
	if reason := New(app, &capturePanelEditor{}).unavailableReason(instance); reason != "" {
		t.Fatalf("known type disabled: %s", reason)
	}
	if reason := New(&capturePanelApp{}, &capturePanelEditor{}).unavailableReason(instance); reason == "" {
		t.Fatal("missing environment has no disabled reason")
	}
}
