package wsmap

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/app/command"
	"sdmm/internal/app/config"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// This gate exercises the real PaneMap constructor and WsMap.Save with a
// hidden OpenGL context. Headless CI must report it as unrun, not as GUI proof.
func TestSaveAcknowledgementBoundaries(t *testing.T) {
	if os.Getenv("APHELIONDMM_GL_TEST") != "1" {
		t.Skip("set APHELIONDMM_GL_TEST=1 for the real hidden-context workspace save gate")
	}
	workspaceContext(t)
	imguiContext := imgui.CreateContext(nil)
	defer imguiContext.Destroy()

	directory := t.TempDir()
	path := filepath.Join(directory, "map.dmm")
	backup := filepath.Join(directory, "backup.dmm")
	original := []byte("\"a\" = (/area/foo,/turf/foo,/obj/foo{dir = 2})\n(1,1,1) = {\"\na\n\"}\n")
	for _, target := range []string{path, backup} {
		if err := os.WriteFile(target, original, 0600); err != nil {
			t.Fatal(err)
		}
	}
	objects := make(map[string]*dmenv.Object)
	for _, objectPath := range []string{"/world", "/area/foo", "/turf/foo", "/obj/foo"} {
		variables := &dmvars.MutableVariables{}
		variables.Put("dir", "2")
		if objectPath == "/world" {
			variables.Put("area", "/area/foo")
			variables.Put("turf", "/turf/foo")
			variables.Put("icon_size", "32")
		}
		objects[objectPath] = &dmenv.Object{Path: objectPath, Vars: variables.ToImmutable()}
	}
	environment := &dmenv.Dme{RootDir: directory, Objects: objects}
	dmmap.PrefabStorage.Free()
	defer dmmap.PrefabStorage.Free()
	dmmap.Init(environment)
	defer dmmap.Free()
	data, err := dmmdata.New(path)
	if err != nil {
		t.Fatal(err)
	}
	mapState, _ := dmmap.New(environment, data, backup)
	app := &saveTestApp{environment: environment, commands: command.NewStorage(), jobs: make(chan func(), 16)}
	app.commands.SetStack(path)
	ws := New(app, mapState)
	// Exercise the shipped shortcut registry -> Grab -> Editor path with this real
	// hidden-context workspace before testing network save acknowledgement.
	ws.Map().OnActivate()
	ws.Map().SetShortcutsVisible(true)
	grab := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}})
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	for _, key := range []glfw.Key{glfw.KeyRightBracket, glfw.KeyLeftBracket} {
		io.KeyPress(int(key))
		imgui.NewFrame()
		shortcut.Process()
		imgui.EndFrame()
		io.KeyRelease(int(key))
		got := mapState.Tiles[0].Instances()[2].Prefab().Vars().ValueV("dir", "")
		want := "8"
		if key == glfw.KeyLeftBracket {
			want = "2"
		}
		if got != want {
			t.Fatalf("rotation shortcut %v: dir=%s want=%s", key, got, want)
		}
	}
	if !grab.HasSelectedArea() {
		t.Fatal("rotation lost the grabbed selection")
	}
	for _, binding := range []struct {
		key  glfw.Key
		tool string
	}{{glfw.Key5, tools.TNPick}, {glfw.Key6, tools.TNDelete}, {glfw.Key7, tools.TNReplace}} {
		io.KeyPress(int(binding.key))
		imgui.NewFrame()
		shortcut.Process()
		imgui.EndFrame()
		io.KeyRelease(int(binding.key))
		if !tools.IsSelected(binding.tool) {
			t.Fatalf("shortcut %v did not select %s", binding.key, binding.tool)
		}
	}
	ws.Map().SetShortcutsVisible(false)
	ws.Map().OnDeactivate()
	initial, err := ws.Map().Editor().CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	document, err := engine.NewDocument(initial)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	local, err := executor.NewLocal(document, actor)
	if err != nil {
		t.Fatal(err)
	}
	delayed := &saveDelayedExecutor{Local: local}
	if err := ws.Map().Editor().AttachCollaborationExecutor(delayed); err != nil {
		t.Fatal(err)
	}
	instance := mapState.Tiles[0].Instances()[2]
	ws.Map().Editor().InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "4")))
	ws.Map().Editor().CommitOperation("Save verification edit")
	if ws.Save() {
		t.Fatal("Save accepted an unacknowledged operation")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("pending save changed original: %v", err)
	}
	// Error presentation is queued; consume it without opening a modal dialog.
	select {
	case <-app.jobs:
	default:
	}
	accepted, err := delayed.Execute(context.Background(), delayed.operation)
	if err != nil {
		t.Fatal(err)
	}
	delayed.complete(accepted, nil)
	(<-app.jobs)()
	// A stale display must not override the acknowledged snapshot being saved.
	instance = mapState.Tiles[0].Instances()[2]
	instance.SetPrefab(dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "8")))
	if !ws.Save() {
		t.Fatal("acknowledged save failed")
	}
	saved, err := dmmdata.New(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, prefabs := range saved.Dictionary {
		for _, prefab := range prefabs {
			if prefab.Path() == "/obj/foo" {
				found = true
			}
			if prefab.Path() == "/obj/foo" && prefab.Vars().ValueV("dir", "") != "4" {
				t.Fatal("saved speculative display instead of acknowledged contents")
			}
		}
	}
	if !found {
		t.Fatal("saved map dropped the edited prefab")
	}
	if app.commands.IsModified(path) {
		t.Fatal("successful save did not balance commands")
	}
	external := []byte("external editor replacement")
	if err := os.WriteFile(path, external, 0600); err != nil {
		t.Fatal(err)
	}
	app.commands.Push(command.Make("Unsaved edit before external conflict", func() {}, func() {}))
	if ws.Save() {
		t.Fatal("save silently replaced a map changed on disk")
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, external) {
		t.Fatalf("conflicting save changed external file: %v", err)
	}
	if !app.commands.IsModified(path) || !ws.HasUnsavedChanges() {
		t.Fatal("conflicting save marked the editor clean")
	}
	observedExternal, err := diskversion.Capture(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ws.overwriteAfterConflict(observedExternal) {
		t.Fatal("explicit overwrite of the observed disk version failed")
	}
	if app.commands.IsModified(path) || ws.HasUnsavedChanges() {
		t.Fatal("successful explicit overwrite did not acknowledge the saved map")
	}
	if _, err := dmmdata.New(path); err != nil {
		t.Fatalf("explicit overwrite produced an unreadable map: %v", err)
	}
	external = []byte("external edit before Save As")
	if err := os.WriteFile(path, external, 0600); err != nil {
		t.Fatal(err)
	}
	app.commands.Push(command.Make("Unsaved edit before Save As", func() {}, func() {}))
	if ws.Save() {
		t.Fatal("save silently replaced a second external edit")
	}
	if ws.saveAsTo(path) {
		t.Fatal("Save As overwrote an existing destination by default")
	}
	unchanged, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, external) {
		t.Fatalf("rejected Save As changed the existing destination: %v", err)
	}
	if ws.CommandStackId() != path || !ws.HasUnsavedChanges() {
		t.Fatal("rejected Save As changed workspace identity or cleared dirty state")
	}
	copyPath := filepath.Join(directory, "map-copy.dmm")
	if !ws.saveAsTo(copyPath) {
		t.Fatal("Save As to a new destination failed")
	}
	if ws.CommandStackId() != copyPath || ws.HasUnsavedChanges() {
		t.Fatal("successful Save As did not rebind and acknowledge the workspace")
	}
	if !app.commands.HasUndoV(copyPath) || app.commands.HasUndoV(path) || !app.commands.HasUndo() {
		t.Fatal("successful Save As did not move the active undo history to the new workspace identity")
	}
	unchanged, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, external) {
		t.Fatalf("Save As changed the conflicted source file: %v", err)
	}
	if _, err := dmmdata.New(copyPath); err != nil {
		t.Fatalf("Save As produced an unreadable map: %v", err)
	}
	app.commands.Push(command.Make("Unsaved verification edit", func() {}, func() {}))
	mapState.Backup = filepath.Join(directory, "missing-backup.dmm")
	beforeFailure, err := os.ReadFile(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Save() {
		t.Fatal("failed staging reported success")
	}
	afterFailure, err := os.ReadFile(copyPath)
	if err != nil || !bytes.Equal(beforeFailure, afterFailure) || !app.commands.IsModified(copyPath) {
		t.Fatal("failed save changed file or dirty state")
	}
}

type saveTestApp struct {
	App
	environment *dmenv.Dme
	commands    *command.Storage
	jobs        chan func()
}

func (app *saveTestApp) LoadedEnvironment() *dmenv.Dme    { return app.environment }
func (app *saveTestApp) CommandStorage() *command.Storage { return app.commands }
func (app *saveTestApp) Prefs() prefs.Prefs {
	return prefs.Prefs{Editor: prefs.Editor{SaveFormat: prefs.SaveFormatDMM}}
}
func (app *saveTestApp) PathsFilter() *dm.PathsFilter            { return dm.NewPathsFilterEmpty() }
func (app *saveTestApp) RunLater(job func())                     { app.jobs <- job }
func (*saveTestApp) ConfigRegister(config.Config)                {}
func (*saveTestApp) AddMouseChangeCallback(func(uint, uint)) int { return 0 }
func (*saveTestApp) RemoveMouseChangeCallback(int)               {}
func (*saveTestApp) SyncPrefabs()                                {}
func (*saveTestApp) SyncVarEditor()                              {}

type saveDelayedExecutor struct {
	*executor.Local
	operation model.Operation
	complete  func(model.AcceptedOperation, error)
}

func (delayed *saveDelayedExecutor) ExecuteAsync(_ context.Context, operation model.Operation, complete func(model.AcceptedOperation, error)) error {
	delayed.operation, delayed.complete = model.CloneOperation(operation), complete
	return nil
}
