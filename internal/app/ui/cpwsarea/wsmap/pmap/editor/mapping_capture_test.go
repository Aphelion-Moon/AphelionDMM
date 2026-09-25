package editor

import (
	"context"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

func TestMappingCaptureTracksAcceptedEditsUndoAndPendingGuards(t *testing.T) {
	e := selectionEditor(t)
	defer e.Close()
	capture := func() *mapping.Source {
		t.Helper()
		handle, version, err := e.CaptureSaveSnapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		source, err := mapping.FromAccepted(context.Background(), "source.dmm", e.app.LoadedEnvironment(), mapping.AcceptedSource{Snapshot: handle, Generation: version.Generation})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(source.Close)
		return source
	}
	before := capture()
	instance := e.dmm.Tiles[0].Instances()[2]
	e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "4")))
	if _, _, err := e.CaptureSaveSnapshot(context.Background()); err == nil {
		t.Fatal("pending edit exposed as accepted context")
	}
	e.CommitOperation("Source direction")
	after := capture()
	if after.Identity.Revision <= before.Identity.Revision || after.Identity.StructuralHash == before.Identity.StructuralHash {
		t.Fatal("accepted edit did not refresh source identity")
	}
	e.app.CommandStorage().UndoV("test")
	undone := capture()
	if undone.Identity.StructuralHash != before.Identity.StructuralHash || undone.Identity.Revision <= after.Identity.Revision {
		t.Fatal("undo did not restore source content at a new accepted revision")
	}
	point := util.Point{X: 1, Y: 1, Z: 1}
	if mapping.CompareCell(before.Cell(point), undone.Cell(point)).Objects {
		t.Fatal("source context diverged from undo")
	}
}
