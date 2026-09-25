package mapping

import (
	"context"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/util"
	"testing"
)

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
