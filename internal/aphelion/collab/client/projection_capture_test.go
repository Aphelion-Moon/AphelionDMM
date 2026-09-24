package client

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestProjectionCaptureSurvivesExecutorTransitions(t *testing.T) {
	for _, transition := range []string{"accept", "reject", "suspend"} {
		t.Run(transition, func(t *testing.T) {
			network, _, baseHash := acceptedNetworkFixture(t)
			defer network.Suspend(errors.New("test cleanup"))
			base, err := network.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			operation := projectionOperation(t, base, 2)
			operation.Changes[0].After.Prefabs[0].Vars["dir"] = "4"
			results := make(chan error, 1)
			if err := network.ExecuteAsync(context.Background(), operation, func(_ model.AcceptedOperation, err error) { results <- err }); err != nil {
				t.Fatal(err)
			}
			decoded, err := protocol.DecodeClient(mustJSON(t, network.transport.(*fakeTransport).next(t)))
			if err != nil {
				t.Fatal(err)
			}
			operation = decoded.Payload.(*protocol.OperationSubmitPayload).Operation
			capture, err := network.CaptureProjection(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !capture.HasPending() || capture.BaseRevision() != base.Revision {
				t.Fatal("capture lost its pending operation or acknowledged revision")
			}
			visible, err := capture.VisibleSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			original := model.CloneSnapshot(visible)
			for i := range visible.Tiles {
				visible.Tiles[i].State.Prefabs[0].Path = "/obj/mutated"
				visible.Tiles[i].State.Prefabs[0].Vars["dir"] = "99"
			}
			// Exercise worker reads concurrently with executor replacement. Captured
			// pending changes and nested acknowledged state must both stay immutable.
			readDone := make(chan error, 1)
			go func() {
				for range 50 {
					next, err := capture.VisibleSnapshot()
					if err != nil {
						readDone <- err
						return
					}
					if !reflect.DeepEqual(next, original) {
						readDone <- errors.New("captured visible state changed or aliases a returned snapshot")
						return
					}
					_, revision, _, hash, err := capture.OperationBase()
					if err != nil || revision != base.Revision || hash != baseHash || capture.EstimatedBytes() == 0 {
						readDone <- errors.New("captured operation base changed")
						return
					}
				}
				readDone <- nil
			}()
			var transitionErr error
			switch transition {
			case "accept":
				accepted := model.AcceptedOperation{Operation: operation, Revision: base.Revision + 1, AcceptedAt: time.Unix(2, 0)}
				after := snapshotWithOperation(t, base, accepted)
				hash, err := after.Hash()
				if err != nil {
					t.Fatal(err)
				}
				transitionErr = network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash}))
			case "reject":
				transitionErr = network.Receive(serverEnvelope(t, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: operation.OperationID, Code: "precondition_failed", Message: "conflict", Revision: base.Revision, MapHash: baseHash}))
			case "suspend":
				network.Suspend(errors.New("transport lost"))
			}
			if err := <-readDone; err != nil {
				t.Fatal(err)
			}
			if transitionErr != nil {
				t.Fatal(transitionErr)
			}
			select {
			case err := <-results:
				if (transition == "accept") != (err == nil) {
					t.Fatalf("transition %s completion: %v", transition, err)
				}
			case <-time.After(time.Second):
				t.Fatal("transition did not resolve pending operation")
			}
			current, err := network.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if transition != "accept" && !reflect.DeepEqual(current, base) {
				t.Fatal("returned capture mutation reached acknowledged state")
			}
		})
	}
}
