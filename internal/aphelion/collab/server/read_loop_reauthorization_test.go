package server

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func startHostedReadLoopSession(t *testing.T, authDelay time.Duration) (*latencyAuthorizer, CreateSessionResponse, *websocket.Conn, model.Snapshot) {
	t.Helper()
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &latencyAuthorizer{fakeHostedAuthorizer: &fakeHostedAuthorizer{session: auth.Session{
		ActorID: actorID, Issuer: "https://issuer.example", Subject: "editor", DisplayName: "Hosted Editor", Role: auth.RoleOwner, ExpiresAt: time.Now().Add(time.Hour),
	}}}
	service := NewService(ServiceConfig{AllowedOrigins: []string{"http://127.0.0.1"}, HostedAuth: authorizer, OnWebSocketError: func(error) {}})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	launchToken, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(service.Handler())
	t.Cleanup(testServer.Close)
	snapshot := testSnapshot(t, 8)
	created := createTestSession(t, testServer.URL, launchToken, snapshot)
	connection, _ := connectTestClientWithCredential(t, testServer.URL, created.SessionID, "hosted-token", 0)
	t.Cleanup(func() { _ = connection.CloseNow() })
	authorizer.calls.Store(0)
	authorizer.delay.Store(int64(authDelay))
	return authorizer, created, connection, snapshot
}

// Every presence update used to cost one hosted authorization round trip on
// the connection's only read loop, so a registry slower than the combined
// message interval stalled acknowledgements and queued presence. Lossy
// non-durable messages are covered by the periodic reauthorization instead.
func TestHostedPresenceAndPingDoNotReauthorizePerMessage(t *testing.T) {
	t.Parallel()

	authorizer, created, connection, _ := startHostedReadLoopSession(t, 200*time.Millisecond)
	sendPresenceBurst(t, connection, created.SessionID, "hosted-presence", 1, 20)
	started := time.Now()
	requireAlive(t, connection, created.SessionID, "hosted-ping")
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("ping behind 20 presence updates took %v with a 200ms registry, want no per-message authorization", elapsed)
	}
	if calls := authorizer.calls.Load(); calls != 0 {
		t.Fatalf("presence and ping caused %d session authorizations, want 0", calls)
	}
}

// Durable edits keep their per-message authorization so a revoked or
// downgraded member cannot append between periodic checks.
func TestHostedDurableOperationStillReauthorizesBeforeAppend(t *testing.T) {
	t.Parallel()

	authorizer, created, connection, snapshot := startHostedReadLoopSession(t, 0)
	submitTestOperation(t, connection, created.SessionID, "hosted-edit", testOperation(t, snapshot, 1))
	_ = readAccepted(t, connection)
	if calls := authorizer.calls.Load(); calls != 1 {
		t.Fatalf("durable operation caused %d session authorizations, want 1", calls)
	}
	authorizer.revoked.Store(true)
	submitTestOperation(t, connection, created.SessionID, "revoked-edit", testOperation(t, snapshot, 2))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, data, err := connection.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatalf("revoked edit close status = %d from %v", websocket.CloseStatus(err), err)
			}
			return
		}
		if decoded, decodeErr := protocol.DecodeServer(data); decodeErr == nil && decoded.Envelope.Type == protocol.ServerOperationAccepted {
			t.Fatal("revoked member's edit was accepted")
		}
	}
}
