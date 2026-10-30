package cpvareditor

import (
	"context"
	"fmt"
	"testing"

	"sdmm/internal/aphelion/varclip"
	"sdmm/internal/app/command"
	"sdmm/internal/app/config"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmmsnap"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
)

type clipboardApp struct {
	captureVariableApp
	configuration *vareditorConfig
}

func (a *clipboardApp) ConfigFind(string) config.Config { return a.configuration }
func (*clipboardApp) SyncPrefabs()                      {}
func (*clipboardApp) SyncVarEditor()                    {}
func (*clipboardApp) RunLater(f func())                 { f() }
func (*clipboardApp) PathsFilter() *dm.PathsFilter      { return dm.NewPathsFilterEmpty() }

type clipboardMap struct{ snapshot *dmmsnap.DmmSnap }

func (*clipboardMap) ActiveLevel() int                                  { return 1 }
func (*clipboardMap) SetActiveLevel(int)                                {}
func (m *clipboardMap) Snapshot() *dmmsnap.DmmSnap                      { return m.snapshot }
func (*clipboardMap) Size() imgui.Vec2                                  { return imgui.Vec2{} }
func (*clipboardMap) Canvas() *canvas.Canvas                            { return &canvas.Canvas{} }
func (*clipboardMap) CanvasState() *canvas.State                        { return nil }
func (*clipboardMap) CanvasControl() *canvas.Control                    { return nil }
func (*clipboardMap) CanvasOverlay() *canvas.Overlay                    { return nil }
func (*clipboardMap) PushAreaHover(util.Bounds, util.Color, util.Color) {}
func (*clipboardMap) OnMapSizeChange()                                  {}

// clipboardFixture is a 2x1 map with /obj/test on each tile; /obj/other
// declares a different variable set.
func clipboardFixture(t *testing.T) (*clipboardApp, []*dmminstance.Instance) {
	t.Helper()
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	objects := make(map[string]*dmenv.Object)
	for _, path := range []string{"/world", "/area/test", "/turf/test", "/obj/test", "/obj/other"} {
		vars := &dmvars.MutableVariables{}
		vars.Put("custom_value", "0")
		vars.Put("name", `"thing"`)
		if path == "/obj/test" {
			vars.Put("test_only", "0")
		}
		if path == "/world" {
			vars.Put("area", "/area/test")
			vars.Put("turf", "/turf/test")
		}
		objects[path] = &dmenv.Object{Path: path, Vars: vars.ToImmutable()}
	}
	a := &clipboardApp{captureVariableApp: captureVariableApp{environment: &dmenv.Dme{Objects: objects}, commands: command.NewStorage()}, configuration: &vareditorConfig{Version: configVersion}}
	dmmap.Init(a.environment)
	t.Cleanup(dmmap.Free)
	var tiles []*dmmap.Tile
	var instances []*dmminstance.Instance
	for x, path := range []string{"/obj/test", "/obj/other"} {
		tile := &dmmap.Tile{Coord: util.Point{X: x + 1, Y: 1, Z: 1}}
		tile.InstancesAdd(dmmap.BaseArea)
		tile.InstancesAdd(dmmap.BaseTurf)
		tile.InstancesAdd(dmmap.PrefabStorage.Initial(path))
		for i, instance := range tile.Instances() {
			instance.SetStableID(fmt.Sprintf("00000000-0000-7000-8000-%06x%06x", x+1, i))
		}
		tiles = append(tiles, tile)
		instances = append(instances, tile.Instances()[2])
	}
	m := &dmmap.Dmm{Path: dmmap.DmmPath{Absolute: "clipboard"}, MaxX: 2, MaxY: 1, MaxZ: 1, Tiles: tiles}
	a.commands.SetStack("clipboard")
	for _, tile := range tiles {
		tile.InstancesRegenerate()
	}
	a.current = editor.New(a, &clipboardMap{snapshot: dmmsnap.New(m)}, m)
	t.Cleanup(a.current.Close)
	if _, err := a.current.SaveSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, instances
}

func TestInstanceVariableRejectsUnsaveableValue(t *testing.T) {
	a, instances := clipboardFixture(t)
	v := &VarEditor{app: a}
	v.EditInstance(instances[0])
	v.setInstanceVariable("name", `"door\"`)
	if got := instances[0].Prefab().Vars().ValueV("name", ""); got != `"thing"` {
		t.Fatalf("unsaveable value was applied: %s", got)
	}
	if !v.noticeIsError || v.notice == "" {
		t.Fatal("rejection was not reported in the panel")
	}
	if a.commands.HasUndoV("clipboard") {
		t.Fatal("rejected value created an undo step")
	}
	if _, err := a.current.SaveSnapshot(context.Background()); err != nil {
		t.Fatalf("map no longer saves: %v", err)
	}

	// The reported case: a lone quote is closed into an empty string.
	if got := v.normalizeValue(`"`); got != `""` {
		t.Fatalf("lone quote normalised to %s", got)
	}
	if !v.acceptValue("name", `"`) || v.noticeIsError {
		t.Fatalf("closed lone quote was rejected: %s", v.notice)
	}
	// Restoring the type default is always allowed; it is deleted, not saved.
	if !v.acceptValue("name", `"thing"`) {
		t.Fatal("type default was rejected")
	}
}

func TestCopyChangedVariablesPlansAndPastesAsOneEdit(t *testing.T) {
	a, instances := clipboardFixture(t)
	source := dmmap.PrefabStorage.Get("/obj/test",
		dmvars.Set(dmvars.Set(dmvars.FromParent(a.environment.Objects["/obj/test"].Vars), "custom_value", "7"), "test_only", "1"))
	instances[0].SetPrefab(source)

	v := &VarEditor{app: a}
	v.EditInstance(instances[0])
	changed := v.changedEntries()
	if len(changed) != 2 || changed[0].Name != "custom_value" || changed[1].Name != "test_only" {
		t.Fatalf("changed = %v, want custom_value and test_only", changed)
	}
	v.varClip.Set(v.prefab.Path(), changed)

	// Prefab mode replaces every /obj/other on the map in one command.
	v.EditPrefab(instances[1].Prefab())
	apply, skipped := v.pastePlan(v.varClip.Entries())
	if len(apply) != 1 || apply[0].Name != "custom_value" || len(skipped) != 1 || skipped[0] != "test_only" {
		t.Fatalf("plan = %v skipped %v", apply, skipped)
	}
	v.pasteEntries(apply, skipped)
	if v.noticeIsError {
		t.Fatalf("paste reported an error: %s", v.notice)
	}
	if got := v.prefab.Vars().ValueV("custom_value", ""); got != "7" || v.prefab.Path() != "/obj/other" {
		t.Fatalf("pasted prefab %s has custom_value %s", v.prefab.Path(), got)
	}

	// An unsaveable clipboard value is refused before anything changes.
	before := v.prefab
	v.pasteEntries([]varclip.Entry{{Name: "name", Value: `"bad\"`}}, nil)
	if v.prefab != before || !v.noticeIsError {
		t.Fatal("unsaveable pasted value was applied or not reported")
	}
}
