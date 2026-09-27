package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

type admissionGateTransport struct {
	*fakeTransport
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	err     error
}

func (transport *admissionGateTransport) Send(ctx context.Context, _ protocol.ClientEnvelope) error {
	transport.once.Do(func() { close(transport.entered) })
	select {
	case <-transport.release:
		return transport.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type admissionRecordingFailureTransport struct{ *fakeTransport }

func (transport *admissionRecordingFailureTransport) Send(_ context.Context, message protocol.ClientEnvelope) error {
	transport.sent <- message
	return ErrTransportNotConnected
}

func TestQueuedAdmissionSurvivesConnectionInterruption(t *testing.T) {
	for _, transition := range []string{"suspend", "terminate", "resume", "discard", "discard-terminate"} {
		t.Run(transition, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			snapshot := projectionSnapshot(t)
			snapshot.MaxX = 3
			transport := &admissionGateTransport{fakeTransport: newFakeTransport(), entered: make(chan struct{}), release: make(chan struct{}), err: ErrTransportNotConnected}
			network, err := NewNetworkExecutor(transport, snapshot, mustActorID(t), "session")
			if err != nil {
				t.Fatal(err)
			}
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(transport.release) }); network.Terminate(errors.New("test cleanup")) })
			completed := make(chan model.OperationID, 3)
			want := make([]model.Operation, 3)
			for i := range want {
				operation := projectionOperation(t, snapshot, i+1)
				operation.ActorID = network.actor
				want[i] = model.CloneOperation(operation)
				id := operation.OperationID
				if err := network.ExecuteAsync(ctx, operation, func(_ model.AcceptedOperation, err error) {
					if err == nil {
						t.Error("interrupted edit succeeded")
					}
					completed <- id
				}); err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					select {
					case <-transport.entered:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				operation.Changes[0].After.Prefabs[0].Vars["dir"] = "caller mutation"
			}
			if transition == "terminate" {
				network.Terminate(errors.New("terminal failure"))
			} else {
				network.Suspend(errors.New("connection interrupted"))
			}
			if count := network.ConflictCount(); count != len(want) {
				t.Errorf("interruption retained %d of %d admitted drafts before dispatch resumed", count, len(want))
			}
			if transition == "discard" || transition == "discard-terminate" {
				if _, err := network.DiscardConflict(ctx, want[1].OperationID); err != nil {
					t.Fatal(err)
				}
			}
			if transition == "discard-terminate" {
				network.Terminate(errors.New("terminal after suspension"))
			}
			fresh := &admissionRecordingFailureTransport{newFakeTransport()}
			if transition == "resume" {
				if err := network.Resume(fresh); err != nil {
					t.Fatal(err)
				}
			}
			release.Do(func() { close(transport.release) })
			for _, operation := range want {
				select {
				case id := <-completed:
					if id != operation.OperationID {
						t.Error("completion order changed")
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if len(fresh.sent) != 0 {
				t.Errorf("reconnect automatically sent %d old queued edits", len(fresh.sent))
			}
			for i, operation := range want {
				retained, ok := network.Conflict(operation.OperationID)
				if (transition == "discard" || transition == "discard-terminate") && i == 1 {
					if ok {
						t.Error("late admission cleanup resurrected explicitly discarded draft")
					}
					continue
				}
				if !ok || !reflect.DeepEqual(retained.Draft, operation) {
					t.Errorf("queued draft %d was lost or changed", i)
					continue
				}
				if i > 0 && retained.Code != "submission_failed" {
					t.Errorf("unsent draft marked %q", retained.Code)
				}
			}
			if network.HasUnacknowledgedOperations() {
				t.Fatal("interruption leaked pending admission")
			}
			if transition == "resume" {
				freshOperation := projectionOperation(t, snapshot, 1)
				if _, err := network.Execute(ctx, freshOperation); !errors.Is(err, ErrTransportNotConnected) {
					t.Fatal("fresh dispatch was not resumed", err)
				}
				if len(fresh.sent) != 1 {
					t.Fatal("fresh edit was not dispatched")
				}
			}
		})
	}
}

func TestInterruptedAdmissionDoesNotRecoverAlreadyAcknowledgedEdit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	snapshot := projectionSnapshot(t)
	transport := &admissionGateTransport{fakeTransport: newFakeTransport(), entered: make(chan struct{}), release: make(chan struct{})}
	network, err := NewNetworkExecutor(transport, snapshot, mustActorID(t), "session")
	if err != nil {
		t.Fatal(err)
	}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(transport.release) }); network.Terminate(errors.New("cleanup")) })
	first := projectionOperation(t, snapshot, 1)
	first.ActorID = network.actor
	completed := make(chan error, 2)
	if err := network.ExecuteAsync(ctx, first, func(_ model.AcceptedOperation, err error) { completed <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	second := projectionOperation(t, snapshot, 2)
	if err := network.ExecuteAsync(ctx, second, func(_ model.AcceptedOperation, err error) { completed <- err }); err != nil {
		t.Fatal(err)
	}
	accepted := model.AcceptedOperation{Operation: first, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	after := snapshotWithOperation(t, snapshot, accepted)
	hash, err := after.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})); err != nil {
		t.Fatal(err)
	}
	network.Suspend(errors.New("connection ended after acknowledgement"))
	if network.ConflictCount() != 1 {
		t.Fatal("acknowledged edit was falsely recovered or queued edit was lost")
	}
	if _, found := network.Conflict(first.OperationID); found {
		t.Fatal("acknowledged edit became a draft")
	}
	if _, found := network.Conflict(second.OperationID); !found {
		t.Fatal("queued draft missing")
	}
	release.Do(func() { close(transport.release) })
	for _, wantFailure := range []bool{false, true} {
		select {
		case err := <-completed:
			if (err != nil) != wantFailure {
				t.Fatal("acknowledgement outcome changed", err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if network.HasUnacknowledgedOperations() {
		t.Fatal("late handoff leaked admission")
	}
}
