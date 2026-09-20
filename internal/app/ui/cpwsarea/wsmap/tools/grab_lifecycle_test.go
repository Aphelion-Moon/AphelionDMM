package tools

import (
	"fmt"
	"reflect"
	"runtime"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/shortcut"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type lifecycleEditor struct {
	editor
	m       *dmmap.Dmm
	commits int
}

func (e *lifecycleEditor) Dmm() *dmmap.Dmm         { return e.m }
func (e *lifecycleEditor) TileDelete(p util.Point) { e.m.GetTile(p).Set(nil) }
func (e *lifecycleEditor) TileReplace(p util.Point, prefabs dmmdata.Prefabs) {
	e.m.GetTile(p).InstancesSet(prefabs)
}
func (*lifecycleEditor) UpdateCanvasByCoords([]util.Point)                   {}
func (*lifecycleEditor) OverlayPushArea(util.Bounds, util.Color, util.Color) {}
func (e *lifecycleEditor) CommitOperation(string)                            { e.commits++ }
func (e *lifecycleEditor) BeginSelectionMove(area util.Bounds, z int) (*editing.Move, error) {
	return editing.NewMove(e.m, area, z, func(string) bool { return true }, func(util.Point) error { return nil }, nil, nil)
}
func (e *lifecycleEditor) PreviewSelectionMove(move *editing.Move, shift util.Point) (util.Bounds, error) {
	_, err := move.Preview(shift)
	return move.Bounds(), err
}
func (e *lifecycleEditor) FinishSelectionMove(move *editing.Move, cancel bool) {
	move.Finish(cancel)
	if !cancel {
		e.commits++
	}
}

func lifecycleFixture(t *testing.T) (*ToolGrab, *lifecycleEditor) {
	t.Helper()
	previous := ed
	t.Cleanup(func() { ed = previous })
	e := &lifecycleEditor{m: &dmmap.Dmm{MaxX: 4, MaxY: 1, MaxZ: 1}}
	for x := 1; x <= 4; x++ {
		tile := &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: 1}}
		vars := &dmvars.MutableVariables{}
		vars.Put("marker", fmt.Sprint(x))
		tile.InstancesAdd(dmmprefab.New(0, "/obj/test", vars.ToImmutable()))
		tile.Instances()[0].SetStableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", x))
		e.m.Tiles = append(e.m.Tiles, tile)
	}
	ed = e
	g := newGrab()
	g.onStart(util.Point{X: 1, Y: 1, Z: 1})
	g.onStop(util.Point{X: 1, Y: 1, Z: 1})
	return g, e
}

func TestGrabMovePreservesIdentityAndPassedTile(t *testing.T) {
	g, e := lifecycleFixture(t)
	sourceID := e.m.Tiles[0].Instances()[0].StableID()
	passedID := e.m.Tiles[1].Instances()[0].StableID()
	g.onStart(util.Point{X: 1, Y: 1, Z: 1})
	g.onMove(util.Point{X: 2, Y: 1, Z: 1})
	g.onMove(util.Point{X: 3, Y: 1, Z: 1})
	if e.m.Tiles[2].Instances()[0].StableID() != sourceID {
		t.Fatal("drag replaced the moved instance identity")
	}
	if e.m.Tiles[1].Instances()[0].StableID() != passedID {
		t.Fatal("passing over a tile replaced its identity")
	}
	g.onStop(util.Point{X: 3, Y: 1, Z: 1})
}

func TestGrabNewGestureDoesNotRestoreOldBackground(t *testing.T) {
	g, e := lifecycleFixture(t)
	g.onStart(util.Point{X: 1, Y: 1, Z: 1})
	g.onMove(util.Point{X: 2, Y: 1, Z: 1})
	g.onMove(util.Point{X: 3, Y: 1, Z: 1})
	g.onStop(util.Point{X: 3, Y: 1, Z: 1})
	i := e.m.Tiles[1].Instances()[0]
	i.SetPrefab(dmmprefab.New(0, i.Prefab().Path(), dmvars.Set(i.Prefab().Vars(), "marker", "remote")))
	g.onStart(util.Point{X: 3, Y: 1, Z: 1})
	g.onMove(util.Point{X: 4, Y: 1, Z: 1})
	g.onStop(util.Point{X: 4, Y: 1, Z: 1})
	if got := e.m.Tiles[1].Instances()[0].Prefab().Vars().ValueV("marker", ""); got != "remote" {
		t.Fatalf("new gesture restored stale background %s", got)
	}
}

func TestGrabCancelRestoresPreview(t *testing.T) {
	g, e := lifecycleFixture(t)
	before := e.m.Copy()
	g.onStart(util.Point{X: 1, Y: 1, Z: 1})
	g.onMove(util.Point{X: 2, Y: 1, Z: 1})
	g.OnDeselect()
	if !reflect.DeepEqual(e.m, &before) {
		t.Fatal("deselect left a speculative move in the map")
	}
	if g.HasSelectedArea() || !g.Stale() {
		t.Fatal("cancel retained an active gesture")
	}
}

func TestGrabEscapeCancelsDuringDrag(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	g, e := lifecycleFixture(t)
	previous, previousName := tools[TNGrab], selectedToolName
	defer func() { tools[TNGrab] = previous; selectedToolName = previousName }()
	tools[TNGrab] = g
	selectedToolName = TNGrab
	before := e.m.Copy()
	g.onStart(util.Point{X: 1, Y: 1, Z: 1})
	g.onMove(util.Point{X: 2, Y: 1, Z: 1})
	shortcut.BeginFrame()
	imgui.NewFrame()
	imgui.OpenPopup("Cancel input popup")
	if !imgui.BeginPopup("Cancel input popup") {
		t.Fatal("popup fixture did not open")
	}
	imgui.Text("Popup owns Escape")
	imgui.EndPopup()
	imgui.EndFrame()
	io.KeyPress(int(glfw.KeyEscape))
	shortcut.BeginFrame()
	imgui.NewFrame()
	if imgui.BeginPopup("Cancel input popup") {
		imgui.CloseCurrentPopup()
		imgui.EndPopup()
	}
	process(false)
	imgui.EndFrame()
	if g.Stale() {
		t.Fatal("popup dismissal cancelled the map gesture")
	}
	io.KeyRelease(int(glfw.KeyEscape))
	shortcut.BeginFrame()
	imgui.NewFrame()
	imgui.EndFrame()
	io.KeyPress(int(glfw.KeyEscape))
	shortcut.BeginFrame()
	imgui.NewFrame()
	process(false)
	imgui.EndFrame()
	if !reflect.DeepEqual(e.m, &before) || !g.Stale() {
		t.Fatal("Escape did not cancel the active drag through the tool frame handler")
	}
}
