package editor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/server"
	collabui "sdmm/internal/aphelion/collab/ui"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/command"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmsnap"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type historyTestApp struct {
	*editorTestApp
	errors []error
}

func (app *historyTestApp) ReportCollaborationError(_ string, err error) {
	app.errors = append(app.errors, err)
}

func TestSelectionMovePinsNetworkSourceBeforeDisplayPublication(t *testing.T) {
	e := selectionEditor(t)
	initial := model.CloneSnapshot(e.authoritative)
	latest := model.CloneSnapshot(initial)
	latest.Revision++
	latest.Tiles[0].State.Prefabs[2].Vars["dir"] = "8"
	network, err := client.NewNetworkExecutor(newEditorNetworkTransport(), initial, e.actorID, "source-capture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { network.Terminate(nil) })
	if err = e.AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	if err = network.ReplaceAcknowledgedSnapshot(context.Background(), latest); err != nil {
		t.Fatal(err)
	}
	oldArea, oldTurf := dmmap.BaseArea, dmmap.BaseTurf
	dmmap.BaseArea, dmmap.BaseTurf = e.dmm.Tiles[0].Instances()[0].Prefab(), e.dmm.Tiles[0].Instances()[1].Prefab()
	t.Cleanup(func() { dmmap.BaseArea, dmmap.BaseTurf = oldArea, oldTurf })
	_, err = e.BeginSelectionMovePreview(editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.CancelSelectionMovePreview)
	select {
	case result := <-e.selectionMovePreview.results:
		defer result.reservation.Release()
		if result.err != nil {
			t.Fatal(result.err)
		}
		state, ok := result.payload.OriginalSource(model.Coord{X: 1, Y: 1, Z: 1})
		if !ok || !state.Equal(latest.Tiles[0].State) {
			t.Fatal("move captured stale display instead of its pinned network revision")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("move source preparation timed out")
	}
}

// Exercise the shipped selection gesture, independent command stacks, real
// WebSockets and authority publication together, including overlapping moves.
func TestMultiClientAreaMoveHistory(t *testing.T) {
	ctx := context.Background()
	seed := selectionEditor(t)
	side := 16
	if os.Getenv("APHELIONDMM_UNDO_STRESS") == "1" {
		side = 96
	}
	prefabs := seed.dmm.Tiles[0].Instances().Prefabs()
	prefabs[2] = dmmprefab.New(dmmprefab.IdNone, prefabs[2].Path(), dmvars.Set(prefabs[2].Vars(), "unknown_override", `list(/obj/missing, "keep me", 42)`))
	seed.dmm.Tiles = nil
	seed.dmm.MaxX, seed.dmm.MaxY = side*3, side*2
	for y := 1; y <= seed.dmm.MaxY; y++ {
		for x := 1; x <= seed.dmm.MaxX; x++ {
			tile := &dmmap.Tile{Coord: util.Point{X: x, Y: y, Z: 1}}
			tile.InstancesSet(prefabs)
			seed.dmm.Tiles = append(seed.dmm.Tiles, tile)
		}
	}
	seed.initializeCollaboration()
	initial, err := seed.SaveSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldArea, oldTurf := dmmap.BaseArea, dmmap.BaseTurf
	dmmap.BaseArea, dmmap.BaseTurf = prefabs[0], prefabs[1]
	t.Cleanup(func() { dmmap.BaseArea, dmmap.BaseTurf = oldArea, oldTurf })
	embedded, err := server.StartEmbedded(ctx, initial)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = embedded.Shutdown(ctx) })
	var editors []*Editor
	var sessions []*collabui.SessionClient
	for i := 0; i < 3; i++ {
		session := collabui.NewSessionClient(collabui.SessionClientConfig{})
		sessions = append(sessions, session)
		t.Cleanup(func() { _ = session.Leave(ctx) })
		var invitation collabui.Invitation
		if i == 0 {
			invitation, err = session.Create(ctx, embedded.Endpoint(), embedded.TakeLaunchToken(), initial)
		} else {
			invitation, err = sessions[0].CreateInvitation(ctx, collabui.InvitationRoleEditor, fmt.Sprintf("mapper-%d", i))
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = session.Join(ctx, invitation); err != nil {
			t.Fatal(err)
		}
		m := editorTestMap(seed.app.LoadedEnvironment())
		if err = mapadapter.ApplyWithEnvironment(m, initial, seed.app.LoadedEnvironment()); err != nil {
			t.Fatal(err)
		}
		app := &editorTestApp{commands: command.NewStorage(), environment: seed.app.LoadedEnvironment(), paths: dm.NewPathsFilterEmpty(), runLater: make(chan func(), 64)}
		app.commands.SetStack("test")
		e := New(&historyTestApp{editorTestApp: app}, &editorTestAttachedMap{snapshot: dmmsnap.New(m)}, m)
		if err = e.AttachCollaborationExecutor(session.NetworkExecutor()); err != nil {
			t.Fatal(err)
		}
		editors = append(editors, e)
		t.Cleanup(e.Close)
	}
	pump := func() {
		for _, e := range editors {
			app := e.app.(*historyTestApp)
			for len(app.runLater) > 0 {
				(<-app.runLater)()
			}
			e.ProcessCollaborationUpdates()
			e.ProcessPasteWork()
		}
	}
	await := func(ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for {
			pump()
			if ready() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("multi-client history did not settle")
			}
			time.Sleep(time.Millisecond)
		}
	}
	var revision model.Revision
	check := func(want *model.Snapshot) model.Snapshot {
		t.Helper()
		await(func() bool {
			for _, e := range editors {
				if e.authoritative.Revision != revision || e.CollaborationSynchronizing() || e.selectionMovePreview != nil {
					return false
				}
			}
			return true
		})
		var got model.Snapshot
		for i, e := range editors {
			saved, err := e.SaveSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			display, err := mapadapter.Import(e.dmm, initial.DocumentID, initial.EnvironmentHash)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(saved.Tiles, display.Tiles) {
				t.Fatalf("client %d display diverged from save at revision %d", i, revision)
			}
			if i == 0 {
				got = saved
			} else if !reflect.DeepEqual(saved.Tiles, got.Tiles) {
				t.Fatalf("clients diverged at revision %d", revision)
			}
			if want != nil && !reflect.DeepEqual(saved.Tiles, want.Tiles) {
				t.Fatalf("client %d lost exact map contents at revision %d", i, revision)
			}
		}
		return got
	}
	move := func(actor, x, y, dx, dy int) {
		t.Helper()
		e := editors[actor]
		pose, err := e.BeginSelectionMovePreview(editing.RectangleSelection(util.Bounds{X1: float32(x), Y1: float32(y), X2: float32(x + side - 1), Y2: float32(y + side - 1)}, 1))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.PreviewSelectionMovePreview(pose, util.Point{X: dx, Y: dy}); err != nil {
			t.Fatal(err)
		}
		if err = e.FinishSelectionMovePreview(pose, false); err != nil {
			t.Fatal(err)
		}
		revision++
	}
	startHistory := func(actor int, redo bool, wantError bool) func() {
		t.Helper()
		e := editors[actor]
		done := false
		var result error
		complete := func(err error) { done = true; result = err }
		started := false
		if redo {
			started = e.app.CommandStorage().RedoAsyncV("test", complete)
		} else {
			started = e.app.CommandStorage().UndoAsyncV("test", complete)
		}
		if !started {
			t.Fatalf("client %d cannot start redo=%v", actor, redo)
		}
		return func() {
			t.Helper()
			await(func() bool { return done })
			if (result != nil) != wantError {
				t.Fatalf("client %d redo=%v error=%v wantError=%v", actor, redo, result, wantError)
			}
			if result == nil {
				revision++
			}
		}
	}
	history := func(actor int, redo bool, wantError bool) { t.Helper(); startHistory(actor, redo, wantError)() }
	check(&initial)
	started := time.Now()
	for cycle := 0; cycle < 4; cycle++ {
		move(0, 1, 1, 1, 1)
		a := check(nil)
		move(1, side+2, 1, 1, 1)
		ab := check(nil)
		move(2, 2, 2, 0, 1)
		abc := check(nil) // overlaps the first actor's moved area
		history(0, false, true)
		check(&abc) // must reject atomically, retaining history
		history(1, false, false)
		ac := check(nil) // disjoint actor may undo independently
		history(2, false, false)
		check(&a)
		history(0, false, false)
		check(&initial)
		history(1, true, false)
		b := check(nil)
		history(0, true, false)
		check(&ab)
		history(2, true, false)
		check(&abc)
		history(1, false, false)
		check(&ac)
		history(2, false, false)
		check(&a)
		history(0, false, false)
		check(&initial)
		history(1, true, false)
		check(&b)
		history(1, false, false)
		check(&initial)
	}
	t.Logf("sequential stress: %d revisions in %s", revision, time.Since(started))
	// Offer disjoint moves and histories together rather than waiting for each
	// acknowledgement. Both clients may submit against the same base revision.
	for cycle := 0; cycle < 4; cycle++ {
		move(0, 1, 1, 1, 1)
		move(1, side+2, 1, 1, 1)
		both := check(nil)
		undoA, undoB := startHistory(0, false, false), startHistory(1, false, false)
		undoB()
		undoA()
		check(&initial)
		redoB, redoA := startHistory(1, true, false), startHistory(0, true, false)
		redoA()
		redoB()
		check(&both)
		undoA, undoB = startHistory(0, false, false), startHistory(1, false, false)
		undoA()
		undoB()
		check(&initial)
	}
	// A remote undo arrives while another client is preparing/holding a move
	// of that same area. Releasing the invalidated gesture must change nothing.
	history(1, true, false)
	check(nil)
	e := editors[0]
	pose, err := e.BeginSelectionMovePreview(editing.RectangleSelection(util.Bounds{X1: float32(side + 3), Y1: 2, X2: float32(side*2 + 2), Y2: float32(side + 1)}, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.PreviewSelectionMovePreview(pose, util.Point{Y: 1}); err != nil {
		t.Fatal(err)
	}
	history(1, false, false)
	await(func() bool { return e.authoritative.Revision == revision && !e.selectionMovePreview.preparing })
	if err = e.FinishSelectionMovePreview(pose, false); err == nil && e.selectionMovePreview != nil && e.selectionMovePreview.err == nil {
		t.Fatal("remote undo failed to invalidate selected source")
	}
	e.CancelSelectionMovePreview()
	check(&initial)
	// Reported sequence: two large undos, then a dozen single-turf edits
	// while the other actor repeatedly moves an area. Disjoint edits retain
	// all twelve undo steps, regardless of intervening remote history.
	move(0, 1, 1, 1, 1)
	check(nil)
	move(1, side+2, 1, 1, 1)
	check(nil)
	history(1, false, false)
	check(nil)
	history(0, false, false)
	check(&initial)
	for i := 0; i < 12; i++ {
		e := editors[1]
		instance := e.dmm.GetTile(util.Point{X: side*2 + 4, Y: i + 1, Z: 1}).Instances()[1]
		e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", fmt.Sprint(i+10))))
		e.CommitOperation("Single turf during remote area move")
		revision++
		if i%2 == 0 {
			move(0, 1, 1, 1, 1)
		} else {
			history(0, false, false)
		}
		check(nil)
	}
	for i := 11; i >= 0; i-- {
		history(1, false, false)
		check(nil)
	}
	check(&initial)
	// The same edits inside the move are intentionally blocked until the later
	// move is undone. The refusal must preserve all twelve history entries.
	for i := 0; i < 12; i++ {
		e := editors[1]
		instance := e.dmm.GetTile(util.Point{X: 2, Y: i + 1, Z: 1}).Instances()[1]
		e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", fmt.Sprint(i+10))))
		e.CommitOperation("Single turf inside later area move")
		revision++
		check(nil)
	}
	edited := check(nil)
	// Save and native-reparse the edited state, retaining opaque variables and
	// recovering collaboration identities through the existing reimport path.
	savePath := filepath.Join(t.TempDir(), "history-roundtrip.dmm")
	output, err := mapadapter.Export(edited, savePath, true, "\n")
	if err != nil {
		t.Fatal(err)
	}
	if err = output.Save(); err != nil {
		t.Fatal(err)
	}
	reparsed, err := dmmdata.New(savePath)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, _ := dmmap.New(seed.app.LoadedEnvironment(), reparsed, savePath)
	roundtrip, err := mapadapter.Reimport(reloaded, edited)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundtrip.Tiles, edited.Tiles) {
		t.Fatal("save/reload lost edited tiles, identity or variables")
	}
	move(0, 1, 1, 1, 1)
	moved := check(nil)
	history(1, false, true)
	check(&moved)
	history(0, false, false)
	check(&edited)
	for i := 0; i < 12; i++ {
		history(1, false, false)
		check(nil)
	}
	check(&initial)
	t.Logf("three clients: %d accepted revisions, %dx%d selected tiles, %s", revision, side, side, time.Since(started))
}
