package stamps

import (
	"context"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
	"testing"
)

func TestModelCaptureKeepsSparseCoordinatesAndUnknownProvenance(t *testing.T) {
	selection, _ := editing.MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}})
	count := 0
	s, err := CaptureModel(context.Background(), "Selection", "", selection, dm.NewPathsFilterEmpty(), func(model.Coord) (model.TileState, bool) {
		count++
		return model.TileState{Prefabs: []model.PrefabState{{Path: "/obj/test", Vars: map[string]string{"dir": "NORTH"}}}}, true
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if count != 2 || s.TileCount() != 2 || s.EnvironmentHash() != "" || s.data.Width != 3 || s.data.Tiles[1].X != 3 {
		t.Fatal("capture broadened membership or fabricated provenance")
	}
}
