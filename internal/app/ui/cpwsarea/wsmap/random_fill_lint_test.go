package wsmap

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/util"
)

// Random Fill commits through the paste worker. With the repository's
// identical-duplicate rule active, an appended duplicate is skipped, a changed
// variable is placed, and the transient notice reports the skipped tiles.
func TestRandomFillSkipsIdenticalDuplicatesThroughWorker(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	tools.SetEditor(e)
	defer tools.ReleaseEditor(e)
	selection := editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 3, Y2: 1}, 1)
	palette := func(dir string) editing.RandomPalette {
		return editing.RandomPalette{Version: 1, Entries: []editing.PaletteEntry{{ID: "a", Weight: 1, Prefab: model.PrefabState{Path: "/obj/lintfoo", Vars: map[string]string{"dir": dir}}}}}
	}
	fill := func(dir string, policy *editing.PastePolicy) {
		t.Helper()
		if err := e.StartRandomFill(selection, palette(dir), 7, 1, util.Point{X: 1, Y: 1, Z: 1}); err != nil {
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
		if policy != nil {
			e.SetPastePolicy(*policy)
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
		settleMapperWork(t, ws, app)
	}

	baseline := countFoo(mustSnapshot(t, e))
	fill("2", nil) // no rules loaded yet: places three atoms
	root := t.TempDir()
	dir := filepath.Join(root, "tools", "maplint", "lints")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rule := "help: No duplicate foo.\n/obj/lintfoo:\n  banned_neighbors:\n    /obj/lintfoo:\n      identical: true\n"
	if err := os.WriteFile(filepath.Join(dir, "foo.yml"), []byte(rule), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := maplint.Active()
	guard.Begin(filepath.Join(root, "game.dme"), func(f func()) { f() })
	defer guard.Reset()
	for i := 0; guard.Status().Loading; i++ {
		if i > 2000 {
			t.Fatal("rules did not load")
		}
		time.Sleep(2 * time.Millisecond)
	}

	before, err := e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	appendOver := editing.PastePolicy{Mode: editing.ApplyOver, Channels: editing.AllChannels}
	fill("2", &appendOver) // identical on every tile: nothing is placed
	same, err := e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if same.Revision != before.Revision || !reflect.DeepEqual(before.Tiles, same.Tiles) {
		t.Fatalf("identical duplicates were placed (revision %d -> %d)", before.Revision, same.Revision)
	}
	if message, _ := e.PlacementLintNotice(); !strings.Contains(message, "Skipped 3 tiles") {
		t.Fatalf("notice = %q", message)
	}

	fill("4", &appendOver) // a different variable is not a duplicate
	changed, err := e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision <= same.Revision {
		t.Fatal("distinct atoms were not placed")
	}
	if got := countFoo(changed) - baseline; got != 6 {
		t.Fatalf("foo atoms added = %d, want 6 (3 from each placing fill)", got)
	}
}

func mustSnapshot(t *testing.T, e interface {
	CollaborationSnapshot(context.Context) (model.Snapshot, error)
}) model.Snapshot {
	t.Helper()
	snapshot, err := e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// countFoo counts /obj/lintfoo atoms on the selected row.
func countFoo(snapshot model.Snapshot) (n int) {
	for _, tile := range snapshot.Tiles {
		if tile.Coord.Y != 1 || tile.Coord.X > 3 {
			continue
		}
		for _, p := range tile.State.Prefabs {
			if p.Path == "/obj/lintfoo" {
				n++
			}
		}
	}
	return n
}
