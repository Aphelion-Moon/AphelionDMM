package wsmap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/app/command"
	"sdmm/internal/app/prefs"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
)

type selectionTestApp struct {
	*saveTestApp
	errors        []error
	clipboard     *dmmclip.Clipboard
	selectionStep int
	paths         *dm.PathsFilter
	network       bool
}

func (app *selectionTestApp) Prefs() prefs.Prefs {
	value := app.saveTestApp.Prefs()
	value.Editor.SelectionMoveStep = app.selectionStep
	return value
}

func (app *selectionTestApp) Clipboard() *dmmclip.Clipboard { return app.clipboard }

func (app *selectionTestApp) PathsFilter() *dm.PathsFilter {
	if app.paths == nil {
		app.paths = dm.NewPathsFilterEmpty()
	}
	return app.paths
}

func (app *selectionTestApp) ReportCollaborationError(_ string, err error) {
	app.errors = append(app.errors, err)
}

func newSelectionWorkspace(t *testing.T) (*WsMap, *selectionTestApp) {
	t.Helper()
	if os.Getenv("APHELIONDMM_GL_TEST") != "1" {
		t.Skip("set APHELIONDMM_GL_TEST=1 for hidden workspace move verification")
	}
	workspaceContext(t)
	ctx := imgui.CreateContext(nil)
	t.Cleanup(ctx.Destroy)
	dir := t.TempDir()
	path := filepath.Join(dir, "map.dmm")
	if err := os.WriteFile(path, []byte("\"a\" = (/area/foo,/turf/foo,/obj/foo{dir = 2})\n(1,1,1) = {\"\naaaa\naaaa\naaaa\naaaa\n\"}\n(1,1,2) = {\"\naaaa\naaaa\naaaa\naaaa\n\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	objects := make(map[string]*dmenv.Object)
	for _, p := range []string{"/world", "/area/foo", "/turf/foo", "/obj/foo"} {
		vars := &dmvars.MutableVariables{}
		vars.Put("dir", "2")
		if p == "/world" {
			vars.Put("area", "/area/foo")
			vars.Put("turf", "/turf/foo")
			vars.Put("icon_size", "32")
		}
		objects[p] = &dmenv.Object{Path: p, Vars: vars.ToImmutable()}
	}
	environment := &dmenv.Dme{RootDir: dir, Objects: objects}
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	dmmap.Init(environment)
	t.Cleanup(dmmap.Free)
	data, err := dmmdata.New(path)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := dmmap.New(environment, data, path)
	app := &selectionTestApp{saveTestApp: &saveTestApp{environment: environment, commands: command.NewStorage(), jobs: make(chan func(), 8)}, clipboard: dmmclip.New()}
	app.commands.SetStack(path)
	ws := New(app, m)
	t.Cleanup(ws.Map().Editor().Close)
	return ws, app
}

func TestSelectionMoveWorkspaceLifecycle(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	m := e.Dmm()
	path := m.Path.Absolute
	snapshot, err := e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantHash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	area := util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}
	move, err := e.BeginSelectionMove(area, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.PreviewSelectionMove(move, util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.CollaborationSnapshot(context.Background()); err == nil {
		t.Fatal("unfinished drag became committed state")
	}
	e.FinishSelectionMove(move, true)
	current, err := e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotHash, err := current.Hash()
	if err != nil || gotHash != wantHash || app.commands.HasUndoV(path) {
		t.Fatal("cancel changed authority or history")
	}
	move, err = e.BeginSelectionMove(area, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.PreviewSelectionMove(move, util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	ws.Map().SetActiveLevel(2)
	if _, err = e.PreviewSelectionMove(move, util.Point{X: 2}); err == nil {
		t.Fatal("preview continued editing a non-visible level")
	}
	ws.Map().SetActiveLevel(1)
	e.FinishSelectionMove(move, false)
	current, err = e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotHash, err = current.Hash()
	if err != nil || gotHash != wantHash || app.commands.HasUndoV(path) {
		t.Fatal("level switch did not cancel the preview")
	}
	move, err = e.BeginSelectionMove(area, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, shift := range []int{1, 2} {
		if _, err = e.PreviewSelectionMove(move, util.Point{X: shift}); err != nil {
			t.Fatal(err)
		}
	}
	e.FinishSelectionMove(move, false)
	if m.Tiles[2].Instances()[2].StableID() != string(snapshot.Tiles[0].State.Prefabs[2].StableID) {
		t.Fatal("committed move changed source ID")
	}
	if m.Tiles[1].Instances()[2].StableID() != string(snapshot.Tiles[1].State.Prefabs[2].StableID) {
		t.Fatal("passed tile changed ID")
	}
	app.commands.UndoV(path)
	current, err = e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotHash, err = current.Hash()
	if err != nil || gotHash != wantHash {
		t.Fatal("undo did not restore exact map hash")
	}
	app.commands.RedoV(path)
	if m.Tiles[2].Instances()[2].StableID() != string(snapshot.Tiles[0].State.Prefabs[2].StableID) {
		t.Fatal("redo changed moved identity")
	}
}
