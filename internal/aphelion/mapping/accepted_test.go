package mapping

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/util"
	"testing"
)

func TestRepeatedSourceUsesTrackAcceptedEditUndoRedoBeforeSave(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	files := map[string]string{
		"base.dmm":     "\"a\" = (/obj/modular_map_root{key = \"room\"; config_file = \"modules.toml\"})\n(1,1,1) = {\"\naa\n\"}\n",
		"module.dmm":   "\"a\" = (/obj/modular_map_connector,/obj/value{v = 0})\n(1,1,1) = {\"\na\n\"}\n",
		"modules.toml": "directory = \"\"\n[rooms.room]\nmodules = [\"module.dmm\"]\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := &dmenv.Dme{RootDir: dir, Objects: map[string]*dmenv.Object{}}
	hash, _ := env.EnvironmentHash()
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: hash, MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []model.Tile{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: "01890f3e-7b5c-7abc-8def-0123456789ac", Path: "/obj/modular_map_connector"}, {StableID: "01890f3e-7b5c-7abc-8def-0123456789ad", Path: "/obj/value", Vars: map[string]string{}}}}}}}
	var scenario Scenario
	for _, value := range []string{"2", "1", "2"} {
		snapshot.Revision++
		snapshot.Tiles[0].State.Prefabs[1].Vars["v"] = value
		catalog := NewCatalog(env)
		catalog.SetAcceptedSources(map[string]AcceptedSource{filepath.Join(dir, "module.dmm"): {Snapshot: snapshotFixture{snapshot}, Generation: 1}})
		base, err := catalog.Load(ctx, filepath.Join(dir, "base.dmm"))
		if err != nil {
			t.Fatal(err)
		}
		projection, err := catalog.Compose(ctx, base, scenario, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(projection.Placements) != 2 {
			t.Fatalf("wanted two uses, got %d: %+v", len(projection.Placements), projection.Diagnostics)
		}
		seen := map[string]bool{}
		for _, placement := range projection.Placements {
			for _, atom := range projection.Source.Cell(placement.Root.Destination) {
				if atom.Path == "/obj/value" && atom.Occurrence == placement.Root.ID && atom.Vars["v"] == value {
					seen[atom.Occurrence] = true
				}
			}
		}
		if len(seen) != 2 {
			t.Fatal("accepted source revision did not reach both displayed uses", value, seen)
		}
		if scenario.Choices != nil && !reflect.DeepEqual(scenario.Choices, projection.Scenario.Choices) {
			t.Fatal("refresh rerolled unrelated choices")
		}
		scenario = projection.Scenario
		projection.Close()
		catalog.Close()
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Fatal("accepted preview wrote source", name, err)
		}
	}
}

type snapshotFixture struct{ value model.Snapshot }

func (f snapshotFixture) Snapshot() model.Snapshot { return f.value }
func (snapshotFixture) EstimatedBytes() uint64     { return 4096 }

func TestAcceptedSourceUsesUnsavedRevisionWithoutMutatingAuthority(t *testing.T) {
	env := &dmenv.Dme{Objects: map[string]*dmenv.Object{}}
	hash, err := env.EnvironmentHash()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", Revision: 7, EnvironmentHash: hash, MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []model.Tile{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: "01890f3e-7b5c-7abc-8def-0123456789ac", Path: "/obj/unsaved", Vars: map[string]string{"v": "2"}}}}}}}
	source, err := FromAccepted(context.Background(), "unsaved.dmm", env, AcceptedSource{Snapshot: snapshotFixture{snapshot}, Generation: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	cell := source.Cell(util.Point{X: 1, Y: 1, Z: 1})
	if len(cell) != 1 || cell[0].Path != "/obj/unsaved" || source.Identity.Revision != 7 || source.Identity.Generation != 3 {
		t.Fatal("accepted source identity/content lost")
	}
	cell[0].Vars["v"] = "changed"
	if snapshot.Tiles[0].State.Prefabs[0].Vars["v"] != "2" {
		t.Fatal("reference modified authority")
	}
	snapshot.EnvironmentHash = "wrong-environment"
	if bad, err := FromAccepted(context.Background(), "unsaved.dmm", env, AcceptedSource{Snapshot: snapshotFixture{snapshot}}); err == nil {
		bad.Close()
		t.Fatal("cross-environment snapshot accepted")
	}
}
