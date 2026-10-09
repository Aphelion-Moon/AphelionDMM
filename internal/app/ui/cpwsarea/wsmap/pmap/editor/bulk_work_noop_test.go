// APHELION EDIT ADDITION START - LOCAL BULK PREPARATION
package editor

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

// A bulk action whose preparation finds nothing to change must complete as a
// silent no-op: no engine rejection surfaced, no revision, no undo entry.
func TestBulkActionWithZeroTileChangesIsSilentNoop(t *testing.T) {
	e := largeBulkEditor(t)
	app := &noopReportingApp{editorTestApp: e.app.(*editorTestApp)}
	e.app = app
	before, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	absent := dmmprefab.New(0, "/obj/aphelion_absent_from_map", dmvars.FromParent(nil))
	e.InstancesDeleteByPrefab(absent)
	if e.localWork == nil {
		t.Fatal("bulk action did not reach the local work owner")
	}
	for e.localWork != nil {
		app.runScheduled(t)
	}
	if len(app.errors) != 0 {
		t.Fatalf("empty bulk action reported errors: %v", app.errors)
	}
	if e.collaborationErr != nil {
		t.Fatalf("empty bulk action retained collaboration error: %v", e.collaborationErr)
	}
	after, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || !reflect.DeepEqual(before.Tiles, after.Tiles) {
		t.Fatal("empty bulk action changed authority")
	}
	if e.app.CommandStorage().HasUndoV("test") {
		t.Fatal("empty bulk action created undo history")
	}
}

// APHELION EDIT ADDITION END
