package editor

import (
	"errors"
	"sdmm/internal/aphelion/collab/model"
	"testing"
)

func TestEditStatusSeparatesOwnedGestureFromRecovery(t *testing.T) {
	e := selectionEditor(t)
	e.pendingChanges[e.authoritative.Tiles[0].Coord] = model.TileState{}
	if message, recovery, busy := e.EditStatus(true); message != "" || recovery || busy {
		t.Fatal("normal gesture warned", message)
	}
	if !e.HasLocalRecovery() {
		t.Fatal("presentation hid recoverable data")
	}
	if message, recovery, _ := e.EditStatus(false); message == "" || !recovery {
		t.Fatal("ownerless data hidden")
	}
	e.collaborationErr = errors.New("capture failed")
	if message, recovery, _ := e.EditStatus(true); message != "capture failed" || !recovery {
		t.Fatal("fault masked by gesture", message)
	}
}
