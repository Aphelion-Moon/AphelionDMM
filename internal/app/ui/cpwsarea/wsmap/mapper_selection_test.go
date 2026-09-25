package wsmap

import (
	"context"
	"path/filepath"
	"reflect"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/editing/stamps"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
	"testing"
	"time"
)

func TestPersistentSelectionStampCaptureAfterToolSwitch(t *testing.T) {
	ws, _ := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	mask, _ := editing.MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}})
	e.WorkingSelection().Set(mask)
	tools.SetEditor(e)
	tools.SetSelected(tools.TNPick)
	defer tools.ReleaseEditor(e)
	if got := tools.SelectedTiles(); len(got) != 2 {
		t.Fatal("copy source lost persistent membership")
	}
	before, _ := e.CollaborationSnapshot(context.Background())
	beforeHash, _ := before.Hash()
	work, err := e.PrepareStampCapture("Selection", mask)
	if err != nil {
		t.Fatal(err)
	}
	s, err := work(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	path := filepath.Join(t.TempDir(), "selection.admmstamp")
	if err = s.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := stamps.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.TileCount() != 2 || !e.StampEnvironmentMatches(loaded) {
		t.Fatal("stamp lost mask or source environment")
	}
	after, _ := e.CollaborationSnapshot(context.Background())
	afterHash, _ := after.Hash()
	if beforeHash != afterHash || before.Revision != after.Revision {
		t.Fatal("read-only capture changed authority")
	}
	e.Close()
	if err = s.Save(path); err != nil {
		t.Fatal("ready stamp depended on an open map", err)
	}
}

func TestRandomFillPreviewCommitAndUndoUseSameChoices(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	tools.SetEditor(e)
	defer tools.ReleaseEditor(e)
	before, err := e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	shape, err := editing.ShapeSelection(editing.ShapeDescriptor{Kind: editing.ShapeCircle, Width: 3, Height: 3}, util.Point{X: 1, Y: 1, Z: 1})
	if err != nil {
		t.Fatal(err)
	}
	palette := editing.RandomPalette{Version: 1, Entries: []editing.PaletteEntry{{ID: "a", Weight: 1, Prefab: model.PrefabState{Path: "/obj/foo", Vars: map[string]string{"dir": "2"}}}, {ID: "b", Weight: 2, Prefab: model.PrefabState{Path: "/obj/foo", Vars: map[string]string{"dir": "4"}}}}}
	if err = e.StartRandomFill(shape, palette, 123, 1, util.Point{X: 1, Y: 1, Z: 1}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.ProcessPasteWork()
		_, ready, err := e.UpdatePastePlacement(util.Point{X: 1, Y: 1, Z: 1})
		if ready && err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("preview did not prepare", err)
		}
		time.Sleep(time.Millisecond)
	}
	preview, err := e.CollaborationSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(before.Tiles, preview.Tiles) {
		t.Fatal("preview mutated authority", err)
	}
	if !tools.Selected().(*tools.ToolGrab).ConfirmPlacement() {
		t.Fatal("confirmation refused")
	}
	deadline = time.Now().Add(5 * time.Second)
	for e.HasPastePlacement() {
		e.ProcessPasteWork()
		select {
		case job := <-app.jobs:
			job()
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("commit did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	after, err := e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	choices, _ := palette.Compile()
	for _, tile := range after.Tiles {
		coord := util.Point{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z}
		if !shape.Contains(coord) {
			continue
		}
		want, _ := choices.Choose(123, coord.Minus(util.Point{X: 1, Y: 1, Z: 1}), 1)
		found := false
		for _, prefab := range tile.State.Prefabs {
			if prefab.Path == want.Path && reflect.DeepEqual(prefab.Vars, want.Vars) {
				found = true
			}
		}
		if !found {
			t.Fatalf("committed choice differs at %v", coord)
		}
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	deadline = time.Now().Add(5 * time.Second)
	for {
		select {
		case job := <-app.jobs:
			job()
		default:
		}
		_, ready := e.MapViewVersion()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("undo did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	undone, err := e.CollaborationSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(before.Tiles, undone.Tiles) {
		t.Fatal("undo changed authored content", err)
	}
	app.commands.RedoV(e.Dmm().Path.Absolute)
	settleMapperWork(t, ws, app)
	redone, err := e.SaveSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(after.Tiles, redone.Tiles) {
		t.Fatal("redo rerolled choices", err)
	}
	if !saveForTest(t, ws, app.jobs) {
		t.Fatal("random Fill save failed")
	}
	data, err := dmmdata.New(e.Dmm().Path.Absolute)
	if err != nil {
		t.Fatal(err)
	}
	reopened, _ := dmmap.New(app.environment, data, e.Dmm().Path.Absolute)
	for _, tile := range e.Dmm().Tiles {
		other := reopened.GetTile(tile.Coord)
		if len(tile.Instances()) != len(other.Instances()) {
			t.Fatal("save/reopen changed random Fill count", tile.Coord)
		}
		// The shipped writer orders channels as objects, turfs, areas while
		// preserving authored order within each channel.
		for i, instance := range tile.Instances().Sorted() {
			if instance.Prefab().ContentKey() != other.Instances()[i].Prefab().ContentKey() {
				t.Fatalf("save/reopen changed content at %v index %d: %s %v -> %s %v", tile.Coord, i, instance.Prefab().Path(), instance.Prefab().Vars(), other.Instances()[i].Prefab().Path(), other.Instances()[i].Prefab().Vars())
			}
		}
	}
}

func settleMapperWork(t *testing.T, ws *WsMap, app *selectionTestApp) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ws.Map().Editor().ProcessPasteWork()
		ws.Map().Editor().ProcessCollaborationUpdates()
		select {
		case job := <-app.jobs:
			job()
		default:
		}
		_, ready := ws.Map().Editor().MapViewVersion()
		if ready && !ws.Map().Editor().HasPastePlacement() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("mapper work did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRandomFillSessionSendsExplicitIdenticalOutcomes(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	tools.SetEditor(e)
	defer tools.ReleaseEditor(e)
	network, transport, document := selectionNetwork(t, ws)
	peer, err := engine.NewDocument(document.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	shape, _ := editing.ShapeSelection(editing.ShapeDescriptor{Kind: editing.ShapeCircle, Width: 3, Height: 3}, util.Point{X: 1, Y: 1, Z: 1})
	palette := editing.RandomPalette{Version: 1, Entries: []editing.PaletteEntry{{ID: "explicit", Weight: 1, Prefab: model.PrefabState{Path: "/obj/foo", Vars: map[string]string{"dir": "8"}}}}}
	if err := e.StartRandomFill(shape, palette, 99, 0.7, util.Point{X: 1, Y: 1, Z: 1}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.ProcessPasteWork()
		_, ready, err := e.UpdatePastePlacement(util.Point{X: 1, Y: 1, Z: 1})
		if ready && err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session random preview did not prepare", err)
		}
		time.Sleep(time.Millisecond)
	}
	if !tools.Selected().(*tools.ToolGrab).ConfirmPlacement() {
		t.Fatal("session confirmation refused")
	}
	op := transport.next(t)
	if len(op.Changes) == 0 {
		t.Fatal("session omitted explicit tiles")
	}
	acceptSelection(t, network, document, op)
	if _, err := peer.Apply(op, time.Unix(0, 1)); err != nil {
		t.Fatal(err)
	}
	runSelectionJob(t, app)
	settleMapperWork(t, ws, app)
	if !reflect.DeepEqual(document.Snapshot().Tiles, peer.Snapshot().Tiles) {
		t.Fatal("peer outcomes differ")
	}
	actual, err := e.SaveSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(actual.Tiles, peer.Snapshot().Tiles) {
		t.Fatal("editor outcomes differ from peer", err)
	}
}
