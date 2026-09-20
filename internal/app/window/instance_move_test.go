package window_test

import (
	"context"
	"testing"

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
