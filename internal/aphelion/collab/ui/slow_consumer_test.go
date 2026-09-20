package ui

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/server"
	"sdmm/internal/aphelion/collab/store/sqlite"
)

// Block a real service socket after joining, rather than manually closing a
// subscription. Three accepted edits fill and overflow its one-entry queue.
func TestSessionClientRecoversFromSlowConsumerQueueOverflow(t *testing.T) {
	t.Run("memory", func(t *testing.T) { verifySlowConsumerRecovery(t, server.NewMemoryStore()) })
	t.Run("sqlite", func(t *testing.T) {
		store, err := sqlite.Open(filepath.Join(t.TempDir(), "recovery.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		verifySlowConsumerRecovery(t, store)
	})
}

func verifySlowConsumerRecovery(t *testing.T, store server.SessionStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gate := &slowWriteGate{ctx: ctx, blocked: make(chan struct{}), release: make(chan struct{})}
	defer gate.unblock()
	service := server.NewService(server.ServiceConfig{
		Store: store, AllowedOrigins: []string{"http://127.0.0.1"},
		Limits: server.Limits{DurableQueueDepth: 1},
	})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	var connections atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/collaboration" && connections.Add(1) == 2 {
			w = &slowResponseWriter{ResponseWriter: w, gate: gate}
		}
		service.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(backend.Close)
	launch, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	initial := controllerSnapshot(t)
	owner := NewSessionClient(SessionClientConfig{})
	t.Cleanup(func() { _ = owner.Leave(context.Background()) })
	invitation, err := owner.Create(ctx, backend.URL, launch, initial)
	if err != nil {
		t.Fatal(err)
	}
	invitation.Origin = "http://127.0.0.1"
	if err := owner.Join(ctx, invitation); err != nil {
		t.Fatal(err)
	}
	invite, err := owner.CreateInvitation(ctx, InvitationRoleEditor, "Slow mapper")
	if err != nil {
		t.Fatal(err)
	}
	slow := NewSessionClient(SessionClientConfig{})
	t.Cleanup(func() { _ = slow.Leave(context.Background()) })
	ended := make(chan error, 1)
	first := &observedSessionTransport{sessionTransport: collabclient.NewWebSocketTransport(collabclient.TransportConfig{}), ended: ended}
	var transportCount atomic.Int32
	slow.newTransport = func() SessionTransport {
		if transportCount.Add(1) == 1 {
			return first
		}
		return collabclient.NewWebSocketTransport(collabclient.TransportConfig{})
	}
	if err := slow.Join(ctx, invite); err != nil {
		t.Fatal(err)
	}
	network := slow.NetworkExecutor()
	slow.mutex.Lock()
	initialCredential, actor := slow.resumptionToken, slow.actorID
	slow.mutex.Unlock()
	gate.armed.Store(true)
	offered := make([]model.OperationID, 0, 5)
	for index := 0; index < 3; index++ {
		snapshot, err := owner.NetworkExecutor().Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		operation := sessionConflictOperation(t, snapshot, owner.actorID)
		if len(snapshot.Tiles) != 0 {
			operation.Changes[0].Before = model.CloneTileState(snapshot.Tiles[0].State)
		}
		operation.Changes[0].After.Prefabs[0].Path = fmt.Sprintf("/obj/recovery_%d", index)
		accepted, err := owner.NetworkExecutor().Execute(ctx, operation)
		if err != nil {
			t.Fatal(err)
		}
		offered = append(offered, accepted.OperationID)
		if index == 0 {
			select {
			case <-gate.blocked:
			case <-ctx.Done():
				t.Fatal("slow socket never blocked")
			}
		}
	}
	gate.unblock()
	select {
	case err := <-ended:
		if websocket.CloseStatus(err) != server.CloseSlowConsumer {
			t.Fatalf("slow socket ended with %v", err)
		}
	case <-ctx.Done():
		t.Fatal("queue overflow did not disconnect the slow consumer")
	}
	waitForClientRevision(t, ctx, slow, 3)
	slow.mutex.Lock()
	rotated := slow.resumptionToken != "" && slow.resumptionToken != initialCredential
	retainedActor := slow.actorID == actor
	slow.mutex.Unlock()
	if !rotated || !retainedActor || slow.NetworkExecutor() != network || transportCount.Load() != 2 {
		t.Fatal("recovery failed to reuse executor/actor with a rotated credential and one reconnect")
	}
	authority, err := owner.fetchSnapshot(ctx, invitation)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := network.Snapshot(ctx)
	if err != nil || mustSnapshotHash(t, recovered) != mustSnapshotHash(t, authority) {
		t.Fatal("replay did not converge to server authority")
	}
	forward, err := network.Execute(ctx, sessionReplacementOperation(t, recovered, actor, "/obj/after_recovery"))
	if err != nil {
		t.Fatal(err)
	}
	offered = append(offered, forward.OperationID)
	waitForClientRevision(t, ctx, owner, 4)
	inverse, err := network.BuildInverse(ctx, forward.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	undone, err := network.Execute(ctx, inverse)
	if err != nil {
		t.Fatal(err)
	}
	offered = append(offered, undone.OperationID)
	for _, client := range []*SessionClient{owner, slow} {
		waitForClientRevision(t, ctx, client, 5)
		snapshot, err := client.NetworkExecutor().Snapshot(ctx)
		if err != nil || mustSnapshotHash(t, snapshot) != mustSnapshotHash(t, authority) {
			t.Fatal("post-recovery inverse did not converge")
		}
	}
	_, operations, err := store.Load(ctx, initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != len(offered) {
		t.Fatalf("stored %d operations, offered %d", len(operations), len(offered))
	}
	for index, operation := range operations {
		if operation.OperationID != offered[index] || operation.Revision != model.Revision(index+1) {
			t.Fatal("offered edit missing, duplicated or reordered in authoritative history")
		}
	}
}

func waitForClientRevision(t *testing.T, ctx context.Context, client *SessionClient, revision model.Revision) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		status := client.Status()
		if status.State == collabclient.StateCaughtUp && status.Revision == revision {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("client did not reach revision %d: state %s revision %d", revision, status.State, status.Revision)
		}
	}
}

type slowWriteGate struct {
	ctx                    context.Context
	armed                  atomic.Bool
	blocked, release       chan struct{}
	blockOnce, releaseOnce sync.Once
}

func (gate *slowWriteGate) unblock() { gate.releaseOnce.Do(func() { close(gate.release) }) }

type slowResponseWriter struct {
	http.ResponseWriter
	gate *slowWriteGate
}

func (writer *slowResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	connection, buffered, err := writer.ResponseWriter.(http.Hijacker).Hijack()
	if err != nil {
		return nil, nil, err
	}
	slow := &slowServerConnection{Conn: connection, gate: writer.gate}
	return slow, bufio.NewReadWriter(buffered.Reader, bufio.NewWriter(slow)), nil
}

type slowServerConnection struct {
	net.Conn
	gate *slowWriteGate
}

func (connection *slowServerConnection) Write(data []byte) (int, error) {
	if connection.gate.armed.Load() {
		connection.gate.blockOnce.Do(func() { close(connection.gate.blocked) })
		select {
		case <-connection.gate.release:
		case <-connection.gate.ctx.Done():
			return 0, connection.gate.ctx.Err()
		}
	}
	return connection.Conn.Write(data)
}

type observedSessionTransport struct {
	sessionTransport
	ended chan<- error
}

func (transport *observedSessionTransport) Wait(ctx context.Context) error {
	err := transport.sessionTransport.Wait(ctx)
	select {
	case transport.ended <- err:
	default:
	}
	return err
}
