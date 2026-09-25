package layout

import (
	"github.com/SpaiR/imgui-go"
	"runtime"
	"sdmm/internal/app/config"
	"sdmm/internal/app/ui/component"
	"sdmm/internal/app/ui/layout/lnode"
	"testing"
)

type compositionDockApp struct {
	app
	cfg layoutConfig
}

func (a *compositionDockApp) ConfigFind(string) config.Config { return &a.cfg }
func (*compositionDockApp) IsLayoutReset() bool               { return false }

type observedDock struct {
	component.Component
	dock int
}

func (n *observedDock) Process(int32) { n.dock = imgui.GetWindowDockID() }
func (n *observedDock) SetVisible(visible bool) {
	n.Component.SetVisible(visible)
	n.dock = imgui.GetWindowDockID()
}

func TestCompositionDockMigrationPreservesLaterPlacementAndOtherDocks(t *testing.T) {
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
	l := &Layout{app: a, initialized: true}
	env, composition, other := &observedDock{}, &observedDock{}, &observedDock{}
	frame := func(setup func()) {
		imgui.NewFrame()
		if setup != nil {
			setup()
		}
		l.wrapNode(lnode.NameEnvironment, 101, env)
		l.wrapNode(lnode.NameComposition, 101, composition)
		l.wrapNode("Other dock", 202, other)
		imgui.EndFrame()
	}
	frame(func() {
		for _, id := range []int{101, 202} {
			imgui.DockBuilderAddNodeV(id, imgui.DockNodeFlagsNone)
			imgui.DockBuilderSetNodeSize(id, imgui.Vec2{X: 400, Y: 500})
		}
		imgui.DockBuilderDockWindow(lnode.NameEnvironment, 101)
		imgui.DockBuilderDockWindow("Other dock", 202)
		imgui.DockBuilderFinish(101)
		imgui.DockBuilderFinish(202)
	})
	frame(nil)
	if !a.cfg.CompositionDocked || composition.dock != env.dock || env.dock != 101 {
		t.Fatalf("composition did not join Environment: %d/%d", composition.dock, env.dock)
	}
	frame(func() { imgui.DockBuilderDockWindow(lnode.NameComposition, 202); imgui.DockBuilderFinish(202) })
	frame(nil)
	if composition.dock != 202 || other.dock != 202 {
		t.Fatal("migration overwrote deliberate docking")
	}
	l.RestoreCompositionDock()
	frame(nil)
	frame(nil)
	if composition.dock != 101 || other.dock != 202 {
		t.Fatal("restore changed unrelated placement or failed to restore Composition")
	}
}
