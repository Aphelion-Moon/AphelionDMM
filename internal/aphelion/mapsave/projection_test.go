package mapsave

import (
	"reflect"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
)

func TestProjectOwnsDetachedExplicitPrefabData(t *testing.T) {
	documentID, err := model.NewDocumentID()
	if err != nil {
		t.Fatal(err)
	}
	stableID, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		ProtocolVersion: model.ProtocolVersion,
		SchemaVersion:   model.SchemaVersion,
		DocumentID:      documentID,
		EnvironmentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MaxX:            1,
		MaxY:            1,
		MaxZ:            1,
		Tiles: []model.Tile{{
			Coord: model.Coord{X: 1, Y: 1, Z: 1},
			State: model.TileState{Prefabs: []model.PrefabState{{
				StableID: stableID,
				Path:     "/obj/aphelion_save_projection_test_unique",
				Vars:     map[string]string{"zeta": "2", "alpha": "1"},
			}}},
		}},
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}

	document, err := Project(snapshot, Metadata{
		Name:   "capture.dmm",
		Path:   dmmap.DmmPath{Readable: "capture.dmm", Absolute: "C:/maps/capture.dmm"},
		Backup: "C:/maps/backup.dmm",
	})
	if err != nil {
		t.Fatalf("project captured save snapshot: %v", err)
	}
	if document.Name != "capture.dmm" || document.Path.Absolute != "C:/maps/capture.dmm" || document.Backup != "C:/maps/backup.dmm" {
		t.Fatalf("save projection metadata = %#v", document)
	}
	instance := document.Tiles[0].Instances()[0]
	if instance.StableID() != string(stableID) {
		t.Fatalf("stable id = %q, want %q", instance.StableID(), stableID)
	}
	prefab := instance.Prefab()
	if prefab.Vars().HasParent() {
		t.Fatal("save projection linked variables to environment-owned state")
	}
	if got := prefab.Vars().Iterate(); !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("explicit variable order = %v, want [alpha zeta]", got)
	}
	if cached, exists := dmmap.PrefabStorage.GetById(prefab.Id()); exists || cached != nil {
		t.Fatal("save projection interned a prefab into the editor's global storage")
	}
}
