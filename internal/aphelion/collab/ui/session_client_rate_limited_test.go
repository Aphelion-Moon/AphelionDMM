package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
)

// refusingSessionTransport fails every connect so a reconnect stays visible in
// the Reconnecting state.
type refusingSessionTransport struct{ err error }

func (transport refusingSessionTransport) Connect(context.Context, protocol.JoinRequest, func(protocol.ServerEnvelope)) error {
	return transport.err
}
func (refusingSessionTransport) Send(context.Context, protocol.ClientEnvelope) error { return nil }
func (refusingSessionTransport) Close(websocket.StatusCode, string) error            { return nil }
func (refusingSessionTransport) Wait(context.Context) error                          { return nil }

func TestSessionClientRateLimitedCloseReconnectsAndStaysDistinguishable(t *testing.T) {
	t.Parallel()

	snapshot := controllerSnapshot(t)
	embedded, err := server.StartEmbedded(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = embedded.Shutdown(context.Background()) })
	client := NewSessionClient(SessionClientConfig{Reconnect: collabclient.ReconnectPolicy{MaxAttempts: 2, Wait: func(context.Context, time.Duration) error { return nil }}})
	invitation, err := client.Create(context.Background(), embedded.Endpoint(), embedded.TakeLaunchToken(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Join(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Leave(context.Background()) })
	client.mutex.Lock()
	transport, network, machine := client.transport, client.network, client.machine
	client.newTransport = func() sessionTransport { return refusingSessionTransport{err: errors.New("dial refused")} }
	client.mutex.Unlock()

	client.monitorTransportResult(machine, transport, network, websocket.CloseError{Code: server.CloseRateLimited, Reason: "presence rate exceeded"})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		client.mutex.Lock()
		settled := !client.reconnecting
		client.mutex.Unlock()
		if settled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	status := client.Status()
	if status.State != collabclient.StateReconnecting {
		t.Fatalf("state = %q, want %q", status.State, collabclient.StateReconnecting)
	}
	// A later failed attempt must not hide why the session is reconnecting.
	if !errors.Is(status.Err, collabclient.ErrRateLimited) {
		t.Fatalf("status error = %v, want ErrRateLimited retained", status.Err)
	}
	if !status.ReconnectReady {
		t.Fatal("rate-limited session cannot be retried manually after backoff is exhausted")
	}
	if _, err := network.Execute(context.Background(), sessionConflictOperation(t, snapshot, client.actorID)); !errors.Is(err, collabclient.ErrRateLimited) {
		t.Fatalf("edit while suspended = %v, want ErrRateLimited", err)
	}
}
