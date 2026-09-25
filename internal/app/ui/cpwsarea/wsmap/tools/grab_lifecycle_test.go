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
	m             *dmmap.Dmm
	commits       int
	sourceDirs    []string
	previewStates map[util.Point]dmmap.Instances
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
func (e *lifecycleEditor) BeginSelectionMovePreview(selection editing.Selection) (*editing.SelectionMove, error) {
	e.sourceDirs = nil
	e.previewStates = make(map[util.Point]dmmap.Instances, selection.Len())
	for _, coord := range selection.Coordinates() {
		var source dmmap.Instances
		for _, instance := range e.m.GetTile(coord).Instances() {
			e.sourceDirs = append(e.sourceDirs, instance.Prefab().Vars().ValueV("dir", ""))
			copy := instance.Copy()
			source = append(source, &copy)
		}
		e.previewStates[coord] = source
	}
	return editing.NewSelectionMove(selection)
}
func (e *lifecycleEditor) PreviewSelectionMovePreview(move *editing.SelectionMove, shift util.Point) (util.Bounds, error) {
	area, _, err := move.Update(shift, e.m.MaxX, e.m.MaxY, move.Level())
	return area, err
}
func (e *lifecycleEditor) FinishSelectionMovePreview(move *editing.SelectionMove, cancel bool) error {
	move.Finish()
	if !cancel {
		shift := move.Shift()
		union := make(map[util.Point]struct{}, len(e.previewStates)*2)
		for source := range e.previewStates {
			union[source] = struct{}{}
			union[source.Plus(shift)] = struct{}{}
		}
		for coord := range union {
			e.m.GetTile(coord).Set(nil)
		}
		for source, instances := range e.previewStates {
			destination := source.Plus(shift)
			for _, instance := range instances {
				copy := instance.Copy()
				copy.SetCoord(destination)
				e.m.GetTile(destination).Set(append(e.m.GetTile(destination).Instances(), &copy))
			}
		}
		e.commits++
	}
	e.previewStates = nil
	return nil
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
	before := e.m.Copy()
	g.onStart(util.Point{X: 1, Y: 1, Z: 1})
	g.onMove(util.Point{X: 2, Y: 1, Z: 1})
	g.onMove(util.Point{X: 3, Y: 1, Z: 1})
	if !reflect.DeepEqual(e.m, &before) {
		t.Fatal("ordinary Grab hover mutated committed map content")
	}
	g.onStop(util.Point{X: 3, Y: 1, Z: 1})
	if e.commits != 1 || g.Bounds().X1 != 3 || e.m.Tiles[2].Instances()[0].StableID() != sourceID || e.m.Tiles[1].Instances()[0].StableID() != passedID {
		t.Fatal("release did not retain the posed selection as one commit intent")
	}
}

func TestGrabNewGestureDoesNotRestoreOldBackground(t *testing.T) {
	g, e := lifecycleFixture(t)
	g.onStart(util.Point{X: 1, Y: 1, Z: 1})
	g.onMove(util.Point{X: 2, Y: 1, Z: 1})
	g.onMove(util.Point{X: 3, Y: 1, Z: 1})
	i := e.m.Tiles[1].Instances()[0]
	i.SetPrefab(dmmprefab.New(0, i.Prefab().Path(), dmvars.Set(i.Prefab().Vars(), "marker", "remote")))
	g.onStop(util.Point{X: 3, Y: 1, Z: 1})
	if got := e.m.Tiles[1].Instances()[0].Prefab().Vars().ValueV("marker", ""); got != "remote" {
		t.Fatalf("move release restored stale passed-over content %s", got)
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
	if !g.HasSelectedArea() || !g.Stale() || e.commits != 0 || g.Bounds().X1 != 1 {
		t.Fatal("cancel lost committed membership or retained an active gesture")
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
	if !reflect.DeepEqual(e.m, &before) || !g.Stale() || e.commits != 0 {
		t.Fatal("Escape did not cancel the active drag through the tool frame handler")
	}
}
