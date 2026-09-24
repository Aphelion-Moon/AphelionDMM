package editor

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

func TestLargeMaskTransformsPrepareWithoutDisplayCapture(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		t.Run(map[bool]string{false: "mirror", true: "rotate"}[rotate], func(t *testing.T) {
			e := largeBulkEditor(t)
			original := e.dmm.Tiles[0].Instances().Prefabs()
			e.dmm.MaxX, e.dmm.MaxY = 16, 16
			e.dmm.Tiles = nil
			var points []util.Point
			for y := 1; y <= 16; y++ {
				for x := 1; x <= 16; x++ {
					p := util.Point{X: x, Y: y, Z: 1}
					tile := &dmmap.Tile{Coord: p}
					tile.InstancesSet(original)
					e.dmm.Tiles = append(e.dmm.Tiles, tile)
					if x != 8 || y != 8 {
						points = append(points, p)
					}
				}
			}
			e.initializeCollaboration()
			e.pMap.Snapshot().Sync()
			before, err := e.SaveSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			selection, err := editing.MaskSelection(points)
			if err != nil {
				t.Fatal(err)
			}
			// Compare the worker's model result with the established sparse planner.
			var expected editing.Transform
			if rotate {
				expected, err = editing.RotateMask(e.dmm, selection, true, e.app.PathsFilter().IsVisiblePath)
			} else {
				expected, err = editing.MirrorMask(e.dmm, selection, editing.MirrorHorizontal, e.app.PathsFilter().IsVisiblePath)
			}
			if err != nil {
				t.Fatal(err)
			}
			var outcomes []bool
			err = e.TrackSelectionTransform(func(applied bool) { outcomes = append(outcomes, applied) }, func() error {
				if rotate {
					_, err = e.RotateSelectionMask(selection, true)
				} else {
					_, err = e.MirrorSelectionMask(selection, editing.MirrorHorizontal)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if e.localWork == nil || len(e.pendingChanges) != 0 {
				t.Fatal("large transform bypassed background owner")
			}
			app := e.app.(*editorTestApp)
			for e.localWork != nil {
				app.runScheduled(t)
			}
			after, err := e.SaveSnapshot(context.Background())
			if err != nil || after.Revision != before.Revision+1 {
				t.Fatal("transform revision", err)
			}
			for _, tile := range expected.Tiles {
				tile.InstancesRegenerate()
				got := e.dmm.GetTile(tile.Coord).Instances()
				want := tile.Instances()
				if len(got) != len(want) {
					t.Fatal("transform lost contents")
				}
				for n := range want {
					if (want[n].StableID() != "" && got[n].StableID() != want[n].StableID()) || got[n].Prefab().ContentKey() != want[n].Prefab().ContentKey() {
						t.Fatal("transform differs from sparse planner", tile.Coord, n)
					}
				}
			}
			app.commands.UndoV("test")
			for e.localWork != nil {
				app.runScheduled(t)
			}
			undone, err := e.SaveSnapshot(context.Background())
			if err != nil || !reflect.DeepEqual(before.Tiles, undone.Tiles) {
				t.Fatal("transform undo lost exact source", err)
			}
			if !reflect.DeepEqual(outcomes, []bool{true, false}) {
				t.Fatal("selection outcome/history", outcomes)
			}
		})
	}
}
