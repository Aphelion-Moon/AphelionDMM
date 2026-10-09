package cphelpers

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/helpers"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type fakeApp struct {
	env      *dmenv.Dme
	selected *dmminstance.Instance
}

func (a *fakeApp) LoadedEnvironment() *dmenv.Dme { return a.env }
func (a *fakeApp) SelectedInstance() (*dmminstance.Instance, bool) {
	return a.selected, a.selected != nil
}

type fakeEditor struct {
	dmm     *dmmap.Dmm
	deleted []*dmminstance.Instance
	commits []string
}

func (e *fakeEditor) Dmm() *dmmap.Dmm                       { return e.dmm }
func (e *fakeEditor) TryBeginTileChange(...util.Point) bool { return true }
func (e *fakeEditor) UpdateCanvasByCoords([]util.Point)     {}
func (e *fakeEditor) InstanceDelete(i *dmminstance.Instance) {
	e.deleted = append(e.deleted, i)
	e.dmm.GetTile(i.Coord()).InstancesRemoveByInstance(i)
}
func (e *fakeEditor) CommitOperation(label string) { e.commits = append(e.commits, label) }

const door = "/obj/machinery/door/airlock/public/glass"

func fixture(t *testing.T) (*Panel, *fakeApp, *fakeEditor) {
	t.Helper()
	prefab := func(path string) *dmmprefab.Prefab {
		vars := &dmvars.MutableVariables{}
		vars.Put("name", `"`+path[len(path)-5:]+`"`)
		return dmmprefab.New(dmmprefab.IdNone, path, vars.ToImmutable())
	}
	tile := &dmmap.Tile{Coord: util.Point{X: 1, Y: 1, Z: 1}}
	tile.InstancesAdd(prefab(door))
	tile.InstancesAdd(prefab(helpers.Root + "/airlock/locked"))
	m := &dmmap.Dmm{MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []*dmmap.Tile{tile}}

	family := helpers.Root + "/airlock"
	list := []helpers.Helper{
		{Type: helpers.Type{Path: family + "/locked", Name: "locked"}, Family: family, Relative: "locked"},
		{Type: helpers.Type{Path: family + "/welded", Name: "welded"}, Family: family, Relative: "welded"},
	}
	for r := 0; r < 20; r++ {
		rel := fmt.Sprintf("access/all/engineering/role%d", r)
		list = append(list, helpers.Helper{Type: helpers.Type{Path: family + "/" + rel, Name: fmt.Sprintf("role%d", r)}, Family: family, Relative: rel})
	}
	app := &fakeApp{env: &dmenv.Dme{Objects: map[string]*dmenv.Object{}}, selected: tile.Instances()[0]}
	ed := &fakeEditor{dmm: m}
	p := &Panel{}
	p.Init(app, func() Editor { return ed })
	p.env, p.cache = app.env, map[string][]helpers.Helper{door: list}
	return p, app, ed
}

func TestTabHighlightFollowsSelection(t *testing.T) {
	p, app, _ := fixture(t)
	if !p.TabHighlight() {
		t.Fatal("an airlock with helpers must highlight the tab")
	}
	app.selected = nil
	if p.TabHighlight() {
		t.Fatal("nothing selected must not highlight the tab")
	}
	p.cache["/obj/structure/table"] = nil
	app.selected = dmminstance.New(util.Point{X: 1, Y: 1, Z: 1}, dmmprefab.New(dmmprefab.IdNone, "/obj/structure/table", nil))
	if p.TabHighlight() {
		t.Fatal("an object without helpers must not highlight the tab")
	}
}

func TestPanelRendersAndTogglesHelpers(t *testing.T) {
	p, _, ed := fixture(t)
	present := p.presentOnTile(ed.dmm.Tiles[0].Instances()[0])
	if present[helpers.Root+"/airlock/locked"] == nil || len(present) != 1 {
		t.Fatalf("present = %v", present)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 800, Y: 600})
	io.Fonts().TextureDataRGBA32()
	frame := func() {
		imgui.NewFrame()
		imgui.Begin("Mapping Helpers")
		p.Process(0)
		imgui.End()
		imgui.EndFrame()
	}
	frame()
	p.query, p.category = "role1", "airlock/access"
	frame()

	// Removing goes through the editor as one operation.
	p.remove(present[helpers.Root+"/airlock/locked"], helpers.Root+"/airlock/locked")
	if len(ed.deleted) != 1 || len(ed.commits) != 1 || ed.commits[0] != "Remove Mapping Helper" {
		t.Fatalf("deleted %d, commits %v", len(ed.deleted), ed.commits)
	}
	if len(p.presentOnTile(ed.dmm.Tiles[0].Instances()[0])) != 0 {
		t.Fatal("removed helper still present")
	}
}
