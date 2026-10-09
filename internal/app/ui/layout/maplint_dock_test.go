package layout

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/ui/layout/lnode"
)

// A saved imgui.ini predating the Map Lint panel never docks it, so it used to
// open as a narrow floating window. It must join Environment once, and later
// deliberate placement must survive.
func TestMapLintDockMigrationJoinsEnvironmentOnce(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetConfigFlags(imgui.ConfigFlagsDockingEnable)
	io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 700})
	io.Fonts().TextureDataRGBA32()
	a := &compositionDockApp{}
	a.cfg.CompositionDocked = true
	l := &Layout{app: a, initialized: true, compositionClosed: true}
	env, lint := &observedDock{}, &observedDock{}
	frame := func(setup func()) {
		imgui.NewFrame()
		if setup != nil {
			setup()
		}
		l.wrapNode(lnode.NameEnvironment, 101, env)
		l.wrapNode(lnode.NameMapLint, 202, lint)
		imgui.EndFrame()
	}
	frame(func() {
		for _, id := range []int{101, 202} {
			imgui.DockBuilderAddNodeV(id, imgui.DockNodeFlagsNone)
			imgui.DockBuilderSetNodeSize(id, imgui.Vec2{X: 400, Y: 500})
		}
		imgui.DockBuilderDockWindow(lnode.NameEnvironment, 101)
		imgui.DockBuilderFinish(101)
		imgui.DockBuilderFinish(202)
	})
	frame(nil)
	if !a.cfg.MapLintDocked || lint.dock != 101 {
		t.Fatalf("Map Lint did not join Environment: lint %d env %d", lint.dock, env.dock)
	}
	frame(func() { imgui.DockBuilderDockWindow(lnode.NameMapLint, 202); imgui.DockBuilderFinish(202) })
	frame(nil)
	if lint.dock != 202 {
		t.Fatal("migration overwrote deliberate docking")
	}
}
