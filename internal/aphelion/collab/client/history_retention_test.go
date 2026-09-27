package client

import (
	"context"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestRemoteHistoryDoesNotRetainUndoPayloads(t *testing.T) {
	snapshot := projectionSnapshot(t)
	operation := projectionOperation(t, snapshot, 1)
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, mustActorID(t), "remote-history")
	if err != nil {
		t.Fatal(err)
	}
	accepted := model.AcceptedOperation{Operation: operation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	after := snapshotWithOperation(t, snapshot, accepted)
	hash, err := after.Hash()
	if err != nil {
		t.Fatal(err)
	}
	envelope := serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})
	if err = network.Receive(envelope); err != nil {
		t.Fatal(err)
	}
	if len(network.accepted) != 0 {
		t.Fatal("remote whole-area payload retained despite actor-scoped undo")
	}
	if _, err = network.BuildInverse(context.Background(), operation.OperationID); err == nil {
		t.Fatal("remote actor history became locally undoable")
	}
	if err = network.Receive(envelope); err != nil {
		t.Fatal("exact remote duplicate failed", err)
	}
	accepted.Changes[0].After.Prefabs[0].Path = "/turf/tampered"
	if err = network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})); err == nil {
		t.Fatal("changed remote duplicate accepted")
	}
}
