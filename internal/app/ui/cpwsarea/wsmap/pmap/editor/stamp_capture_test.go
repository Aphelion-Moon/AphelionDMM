package editor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/util"
)

func TestNetworkStampCapturePinsSourceAcrossRemotePublication(t *testing.T) {
	e := selectionEditor(t)
	initial := model.CloneSnapshot(e.authoritative)
	network, err := client.NewNetworkExecutor(newEditorNetworkTransport(), initial, e.actorID, "stamp-capture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { network.Terminate(nil) })
	if err = e.AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	e.workBudget = resources.NewFixedBudget(16 << 20)
	selection := editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1)
	work, err := e.PrepareStampCapture("Pinned selection", selection)
	if err != nil {
		t.Fatal(err)
	}
	original, err := work(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "original.admmstamp")
	if err = original.Save(path); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original.Close()
	after := model.CloneTileState(initial.Tiles[0].State)
	after.Prefabs[2].Vars["dir"] = "8"
	op, err := e.forwardOperation(initial, []model.TileChange{{Coord: initial.Tiles[0].Coord, Before: initial.Tiles[0].State, After: after}})
	if err != nil {
		t.Fatal(err)
	}
	op.ActorID, err = model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	document, err := engine.NewDocument(initial)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := document.Apply(op, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := document.Snapshot().Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err = network.Receive(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, SessionID: "stamp-capture", MessageID: "remote-edit", Type: protocol.ServerOperationAccepted, Payload: mustEditorJSON(t, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.PrepareStampCapture("Unsynchronized selection", selection); err == nil {
		t.Fatal("capture admitted an undisplayed source revision")
	}
	e.ProcessCollaborationUpdates()
	assertEditorDirection(t, e.dmm, "8")
	// The worker may run after another edit, or after the source tab closes.
	e.Close()
	stamp, err := work(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stamp.Close()
	if err = stamp.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("deferred stamp capture changed after remote publication")
	}
	stamp.Close()
	if e.workBudget.Used() != 0 {
		t.Fatal("stamp capture leaked its scratch reservation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = work(ctx); err == nil || e.workBudget.Used() != 0 {
		t.Fatal("cancelled capture retained work", err)
	}
}
