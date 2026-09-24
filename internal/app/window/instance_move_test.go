package window_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/util"
)

func TestInstanceMoveKeepsIdentityAcrossMatchingPrefabs(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	initial, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hash := func(state model.Snapshot) string {
		t.Helper()
		value, err := state.Hash()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	wantHash := hash(initial)
	origin := util.Point{X: 1, Y: 1, Z: 1}
	moved := e.Dmm().GetTile(origin).Instances()[2]
	wantCoords := make(map[string]util.Point)
	for _, tile := range e.Dmm().Tiles {
		for _, instance := range tile.Instances() {
			wantCoords[instance.StableID()] = tile.Coord
		}
	}
	frame := mouseWorkspaceFrame(t, ws, app.mouse)
	tools.SetSelected(tools.TNMove)
	frame(false, 1, 1)
	frame(false, 1, 1)
	ws.Map().CanvasState().SetHoveredInstance(moved)
	frame(true, 1, 1)
	frame(true, 1, 1)
	// Every destination already contains an identical prefab. Selecting by
	// prefab ID would silently move the destination's resident on the next hop.
	for _, x := range []int{2, 3, 1, 4} {
		frame(true, x, 1)
		destination := util.Point{X: x, Y: 1, Z: 1}
		wantCoords[moved.StableID()] = destination
		seen := make(map[string]bool)
		for _, tile := range e.Dmm().Tiles {
			for _, instance := range tile.Instances() {
				id := instance.StableID()
				if seen[id] || wantCoords[id] != tile.Coord || instance.Coord() != tile.Coord {
					t.Fatalf("hop %d changed identity or moved the wrong instance: %q at %v", x, id, tile.Coord)
				}
				seen[id] = true
			}
		}
		if len(seen) != len(wantCoords) || !seen[moved.StableID()] {
			t.Fatalf("hop %d lost an existing instance", x)
		}
		if moved.Coord() != destination {
			t.Fatal("move left the selected instance reference at its old coordinate")
		}
		if x == 1 {
			display, err := mapadapter.Import(e.Dmm(), initial.DocumentID, initial.EnvironmentHash)
			if err != nil || hash(display) != wantHash {
				t.Fatal("returning through the origin changed the original map", err)
			}
		}
	}
	frame(false, 4, 1)
	frame(false, 4, 1)
	committed, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if committed.Revision != initial.Revision+1 || hash(committed) == wantHash {
		t.Fatal("drag was not committed as one operation")
	}
	app.commands.UndoV(ws.CommandStackId())
	undone, err := e.SaveSnapshot(context.Background())
	if err != nil || hash(undone) != wantHash || app.commands.HasUndoV(ws.CommandStackId()) {
		t.Fatal("move undo did not restore exact identity and order", err)
	}
	app.commands.RedoV(ws.CommandStackId())
	redone, err := e.SaveSnapshot(context.Background())
	if err != nil || hash(redone) != hash(committed) {
		t.Fatal("move redo did not restore exact final state", err)
	}
	if len(app.errors) != 0 {
		t.Fatalf("unexpected errors: %v", app.errors)
	}
}

func TestInstanceMoveCaptureFailureKeepsDisplay(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		startFail bool
		priorHop  bool
		index     int
	}{
		{name: "source", startFail: true, index: 2},
		{name: "first destination", index: 2},
		{name: "later destination", priorHop: true, index: 2},
		{name: "turf destination", priorHop: true, index: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ws, app := newMouseNetworkWorkspace(t)
			e := ws.Map().Editor()
			initial, err := e.CollaborationSnapshot(context.Background())
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
			authority, err := executor.NewLocal(document, actor)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.AttachCollaborationExecutor(authority); err != nil {
				t.Fatal(err)
			}
			origin := util.Point{X: 1, Y: 1, Z: 1}
			moved := e.Dmm().GetTile(origin).Instances()[scenario.index]
			if scenario.index == 1 {
				app.PathsFilter().TogglePath("/obj/foo")
			}
			fault := util.Point{X: 3, Y: 1, Z: 1}
			if scenario.startFail {
				fault = origin
			}
			e.Dmm().GetTile(fault).Instances()[2].SetStableID("invalid-move-capture")
			frame := mouseWorkspaceFrame(t, ws, app.mouse)
			tools.SetSelected(tools.TNMove)
			frame(false, 1, 1)
			frame(false, 1, 1)
			// The current-frame picker selects the visible turf when objects are hidden.
			frame(true, 1, 1)
			frame(true, 1, 1)
			if scenario.priorHop {
				frame(true, 2, 1)
				if moved.Coord().X != 2 {
					t.Fatal("fixture did not establish a valid earlier preview")
				}
			}
			before := e.Dmm().Copy()
			frame(true, 3, 1)
			if !reflect.DeepEqual(e.Dmm(), &before) {
				t.Fatal("failed capture changed the display")
			}
			// Neither subsequent tile input nor Shift pixel movement may extend a
			// gesture after its capture failed. Turf release must not prune peers.
			frame(true, 4, 1)
			io := imgui.CurrentIO()
			io.KeyPress(int(glfw.KeyLeftShift))
			frame(true, 2, 1)
			frame(true, 3, 1)
			io.KeyRelease(int(glfw.KeyLeftShift))
			frame(false, 3, 1)
			frame(false, 3, 1)
			if !reflect.DeepEqual(e.Dmm(), &before) {
				t.Fatal("input or release changed the faulted preview")
			}
			if _, err := e.SaveSnapshot(context.Background()); err == nil {
				t.Fatal("capture failure lost the Save guard")
			}
			after, err := authority.Snapshot(context.Background())
			if err != nil || !reflect.DeepEqual(after, initial) {
				t.Fatal("capture failure changed authority", err)
			}
			if app.commands.HasUndoV(ws.CommandStackId()) || len(app.errors) != 1 {
				t.Fatalf("failed capture created history or reported %d errors, want one", len(app.errors))
			}
			if !tools.Selected().Stale() {
				t.Fatal("release retained the faulted tool instance")
			}
		})
	}
}
