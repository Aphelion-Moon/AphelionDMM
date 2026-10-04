package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestRecoveryRetainsAllDraftsAcrossConflictBursts(t *testing.T) {
	for _, suspend := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejections", true: "suspend"}[suspend], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			snapshot := projectionSnapshot(t)
			transport := newFakeTransport()
			network, err := NewNetworkExecutor(transport, snapshot, mustActorID(t), "session")
			if err != nil {
				t.Fatal(err)
			}
			defer network.Terminate(errors.New("test cleanup"))
			hash, err := snapshot.Hash()
			if err != nil {
				t.Fatal(err)
			}
			var ids []model.OperationID
			completions := make(chan error, 103)
			for i := 0; i < 103; i++ {
				operation := projectionOperation(t, snapshot, 1)
				if suspend && i >= 99 {
					// Keep later speculation compatible with earlier pending edits.
					capture, err := network.CaptureProjection(ctx)
					if err != nil {
						t.Fatal(err)
					}
					visible, err := capture.VisibleSnapshot()
					if err != nil {
						t.Fatal(err)
					}
					operation.Changes[0].Before = tileAt(visible, 1)
				}
				if err := network.ExecuteAsync(ctx, operation, func(_ model.AcceptedOperation, err error) { completions <- err }); err != nil {
					t.Fatal(err)
				}
				decoded, err := protocol.DecodeClient(mustJSON(t, transport.next(t)))
				if err != nil {
					t.Fatal(err)
				}
				id := decoded.Payload.(*protocol.OperationSubmitPayload).Operation.OperationID
				ids = append(ids, id)
				if !suspend || i < 99 {
					if err := network.Receive(serverEnvelope(t, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: id, Code: "precondition_failed", Message: "conflict", Revision: snapshot.Revision, MapHash: hash})); err != nil {
						t.Fatal(err)
					}
				}
			}
			if suspend {
				network.Suspend(errors.New("connection lost"))
			}
			for range ids {
				select {
				case err := <-completions:
					if err == nil {
						t.Fatal("unexpected accepted edit")
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			conflicts := network.Conflicts()
			if len(conflicts) != len(ids) {
				t.Fatalf("retained %d of %d unresolved drafts", len(conflicts), len(ids))
			}
			for i, id := range ids {
				if conflicts[i].OperationID != id || len(conflicts[i].Draft.Changes) != 1 {
					t.Fatal("lost draft contents or ordering")
				}
			}
			// The oldest draft must still be actionable, not merely counted.
			if _, err := network.BuildConflictRebuild(ctx, ids[0]); err != nil {
				t.Fatal(err)
			}
			if _, err := network.DiscardConflict(ctx, ids[0]); err != nil {
				t.Fatal(err)
			}
			if len(network.Conflicts()) != len(ids)-1 {
				t.Fatal("explicit discard removed the wrong number of drafts")
			}
		})
	}
}
