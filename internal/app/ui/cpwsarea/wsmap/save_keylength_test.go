package wsmap

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/app/command"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmsave"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// A real WsMap save whose edit pushes the unique tile contents past the
// one-letter key space must ask before re-keying the file; Cancel leaves the
// file byte-identical and Save anyway writes two-letter keys.
func TestSaveWarnsBeforeKeyLengthGrows(t *testing.T) {
	if os.Getenv("APHELIONDMM_GL_TEST") != "1" {
		t.Skip("set APHELIONDMM_GL_TEST=1 for the real hidden-context workspace save gate")
	}
	workspaceContext(t)
	imguiContext := imgui.CreateContext(nil)
	defer imguiContext.Destroy()

	const tiles = 53 // 52 distinct contents fill every one-letter key; tile 53 repeats tile 1.
	alphabet := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var file strings.Builder
	objects := make(map[string]*dmenv.Object)
	addObject := func(path string) {
		variables := &dmvars.MutableVariables{}
		variables.Put("dir", "2")
		if path == "/world" {
			variables.Put("area", "/area/foo")
			variables.Put("turf", "/turf/foo")
			variables.Put("icon_size", "32")
		}
		objects[path] = &dmenv.Object{Path: path, Vars: variables.ToImmutable()}
	}
	for _, path := range []string{"/world", "/area/foo", "/turf/foo"} {
		addObject(path)
	}
	for i, key := range alphabet {
		objectPath := fmt.Sprintf("/obj/o%d", i)
		addObject(objectPath)
		fmt.Fprintf(&file, "\"%c\" = (/area/foo,/turf/foo,%s)\n", key, objectPath)
	}
	file.WriteString("(1,1,1) = {\"\n" + alphabet + "a\n\"}\n")
	original := []byte(file.String())

	directory := t.TempDir()
	path := filepath.Join(directory, "map.dmm")
	backup := filepath.Join(directory, "backup.dmm")
	for _, target := range []string{path, backup} {
		if err := os.WriteFile(target, original, 0600); err != nil {
			t.Fatal(err)
		}
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
	if data.KeyLength != 1 {
		t.Fatalf("fixture key length = %d", data.KeyLength)
	}
	mapState, _ := dmmap.New(environment, data, backup)
	app := &saveTestApp{environment: environment, commands: command.NewStorage(), jobs: make(chan func(), 16)}
	app.commands.SetStack(path)
	ws := New(app, mapState)

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
	last := mapState.GetTile(util.Point{X: tiles, Y: 1, Z: 1}).Instances()[2]
	ws.Map().Editor().InstanceReplace(last, dmmprefab.New(dmmprefab.IdNone, last.Prefab().Path(), dmvars.Set(last.Prefab().Vars(), "dir", "4")))
	ws.Map().Editor().CommitOperation("Make the 53rd unique tile content")
	accepted, err := delayed.Execute(context.Background(), delayed.operation)
	if err != nil {
		t.Fatal(err)
	}
	delayed.complete(accepted, nil)
	(<-app.jobs)()

	var prompts []dmmsave.KeyLengthChange
	answer := false
	ws.presentKeyLengthChange = func(change dmmsave.KeyLengthChange, respond func(bool)) {
		prompts = append(prompts, change)
		respond(answer)
	}

	if saveForTest(t, ws, app.jobs) {
		t.Fatal("canceled key length change reported a saved map")
	}
	if len(prompts) != 1 {
		t.Fatalf("prompts after cancel = %d", len(prompts))
	}
	plan := prompts[0].Plan
	if plan.Current != 1 || plan.Required != 2 || plan.Unique != 53 || plan.CurrentCapacity != 52 {
		t.Fatalf("unexpected plan %+v", plan)
	}
	warning, detail := keyLengthWarning(prompts[0])
	if warning != "Saving will change the key length from 1 to 2; every tile key in map.dmm will change (large diff)." ||
		detail != "Unique tile contents: 53 (capacity at 1: 52)" {
		t.Fatalf("unexpected wording %q / %q", warning, detail)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("canceled save changed the file: %v", err)
	}
	if !ws.HasUnsavedChanges() || ws.activeSave != nil {
		t.Fatal("canceled save cleared dirty state or left a save active")
	}

	answer = true
	if !saveForTest(t, ws, app.jobs) {
		t.Fatal("confirmed key length change did not save")
	}
	if len(prompts) != 2 {
		t.Fatalf("prompts after confirm = %d", len(prompts))
	}
	saved, err := dmmdata.New(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.KeyLength != 2 || len(saved.Dictionary) != 53 {
		t.Fatalf("saved key length %d with %d contents", saved.KeyLength, len(saved.Dictionary))
	}

	// The load-time backup still has one-letter keys, but the user already approved
	// two-letter keys for this file in this session: no second prompt.
	last = mapState.GetTile(util.Point{X: tiles, Y: 1, Z: 1}).Instances()[2]
	ws.Map().Editor().InstanceReplace(last, dmmprefab.New(dmmprefab.IdNone, last.Prefab().Path(), dmvars.Set(last.Prefab().Vars(), "dir", "6")))
	ws.Map().Editor().CommitOperation("Edit after approved re-key")
	accepted, err = delayed.Execute(context.Background(), delayed.operation)
	if err != nil {
		t.Fatal(err)
	}
	delayed.complete(accepted, nil)
	(<-app.jobs)()
	answer = false
	if !saveForTest(t, ws, app.jobs) {
		t.Fatal("second save after approval failed")
	}
	if len(prompts) != 2 {
		t.Fatalf("approved key length prompted again: %d prompts", len(prompts))
	}
}
