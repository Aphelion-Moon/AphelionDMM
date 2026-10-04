package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func queuedTransportFixture() *WebSocketTransport {
	transport := NewWebSocketTransport(TransportConfig{})
	transport.durable = make(chan protocol.ClientEnvelope, 2)
	transport.presence = make(chan protocol.ClientEnvelope, 1)
	transport.bulkWrites = make(chan bulkWrite)
	transport.done = make(chan struct{})
	return transport
}

func TestTransportRefusesKnownStoppedSends(t *testing.T) {
	for _, stopped := range []string{"canceled", "finished", "done"} {
		for _, presence := range []bool{false, true} {
			name := stopped + "/durable"
			if presence {
				name = stopped + "/presence"
			}
			t.Run(name, func(t *testing.T) {
				transport := queuedTransportFixture()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cause := errors.New("connection ended")
				if stopped == "canceled" {
					cancel()
					cause = context.Canceled
				} else {
					transport.finish(cause)
					if stopped == "done" {
						close(transport.done)
					}
				}
				message := protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "ping", SessionID: "session", Type: protocol.ClientPing, Payload: mustJSON(t, protocol.PingPayload{Nonce: "ping"})}
				if presence {
					message.Type = protocol.ClientPresenceUpdate
					message.Payload = mustJSON(t, protocol.PresenceUpdatePayload{Sequence: 1, Status: "active"})
					// A refused update must not evict a previously queued observation.
					transport.presence <- protocol.ClientEnvelope{MessageID: "retained"}
				}
				if err := transport.Send(ctx, message); !errors.Is(err, cause) {
					t.Errorf("Send = %v, want %v", err, cause)
				}
				if len(transport.durable) != 0 {
					t.Error("stopped send entered the durable queue")
				}
				if presence {
					select {
					case queued := <-transport.presence:
						if queued.MessageID != "retained" {
							t.Error("stopped presence send replaced queued state")
						}
					default:
						t.Error("stopped presence send dropped queued state")
					}
				}
			})
		}
	}
}

func TestExecutorPreservesDraftWhenTransportAlreadyFinished(t *testing.T) {
	transport := queuedTransportFixture()
	cause := errors.New("connection ended before submission")
	transport.finish(cause) // Worker shutdown need not have closed done yet.
	snapshot := projectionSnapshot(t)
	operation := projectionOperation(t, snapshot, 1)
	network, err := NewNetworkExecutor(transport, snapshot, operation.ActorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	defer network.Terminate(cause)
	result := make(chan error, 1)
	go func() {
		_, err := network.Execute(ctx, operation)
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, cause) {
			t.Fatalf("submission waited for an impossible acknowledgement: %v", err)
		}
	case <-ctx.Done():
		network.Suspend(cause)
		<-result
		t.Fatal("submission waited for an impossible acknowledgement")
	}
	if network.HasUnacknowledgedOperations() || len(transport.durable) != 0 {
		t.Fatal("failed send remained queued or pending")
	}
	drafts := network.Conflicts()
	if len(drafts) != 1 || drafts[0].OperationID != operation.OperationID || !drafts[0].Draft.Changes[0].After.Equal(operation.Changes[0].After) {
		t.Fatal("failed send lost its recoverable draft")
	}
}

func TestBulkSubmissionRefusesKnownStoppedTransport(t *testing.T) {
	transport := queuedTransportFixture()
	transport.bulkEnabled = true
	cause := errors.New("connection ended before bulk submission")
	transport.finish(cause)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := transport.SendOperation(ctx, "session", admissionSizeFixture(65)); !errors.Is(err, cause) {
		t.Fatalf("bulk submission ignored known shutdown: %v", err)
	}
}
