package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/compat"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestHostedWebSocketReauthorizesSessionAndClosesRevokedCredential(t *testing.T) {
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &fakeHostedAuthorizer{session: auth.Session{
		ActorID: actorID, Issuer: "https://issuer.example", Subject: "subject", DisplayName: "Hosted Mapper", Role: auth.RoleEditor, ExpiresAt: time.Now().Add(time.Hour),
	}}
	_, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"}, HostedAuth: authorizer, HostedReauthorizationInterval: 10 * time.Millisecond,
	})
	websocketURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/v1/collaboration"
	connection, _, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer hosted-token"}, "Origin": []string{"http://127.0.0.1"}}, Subprotocols: []string{WebSocketSubprotocol},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "hosted-join", SessionID: created.SessionID, Type: protocol.ClientJoin}, protocol.JoinPayload{JoinToken: "hosted-token"})
	joined := readServerEnvelope(t, connection)
	if joined.Envelope.Type != protocol.ServerJoined || joined.Payload.(*protocol.JoinedPayload).Role != string(RoleEditor) {
		t.Fatalf("joined = %#v", joined)
	}
	_ = readServerEnvelopeType(t, connection, protocol.ServerReplayComplete)
	_ = readServerEnvelopeType(t, connection, protocol.ServerPresenceSnapshot)
	authorizer.revoked.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err = connection.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("revoked close status = %d from %v", websocket.CloseStatus(err), err)
	}
}

type fakeHostedAuthorizer struct {
	mutex   sync.RWMutex
	session auth.Session
	revoked atomic.Bool
}

func (authorizer *fakeHostedAuthorizer) Authorize(context.Context, string) (auth.Session, error) {
	if authorizer.revoked.Load() {
		return auth.Session{}, auth.ErrInvalidSession
	}
	authorizer.mutex.RLock()
	defer authorizer.mutex.RUnlock()
	return authorizer.session, nil
}

func (authorizer *fakeHostedAuthorizer) AuthorizeSession(context.Context, string, string) (auth.Session, error) {
	return authorizer.Authorize(context.Background(), "")
}

func (authorizer *fakeHostedAuthorizer) setRole(role auth.Role) {
	authorizer.mutex.Lock()
	authorizer.session.Role = role
	authorizer.mutex.Unlock()
}

func TestJoinCompatibilityRejectsUnsupportedCompiledProtocol(t *testing.T) {
	service := NewService(ServiceConfig{Compatibility: compat.Matrix{Releases: []compat.Release{{
		Name: compat.CurrentRelease, ProtocolVersions: []uint16{model.ProtocolVersion + 1}, SchemaVersions: []uint16{model.SchemaVersion},
	}}}})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	if err := service.validateJoinCompatibility(engine.Metadata{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion}); err == nil {
		t.Fatal("validateJoinCompatibility() error = nil")
	}
}

func TestWebSocketRejectsOriginAndNegotiatesProtocol(t *testing.T) {
	t.Parallel()

	service, created, testServer := startHTTPTestSession(t)
	_ = service
	websocketURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/v1/collaboration"

	_, response, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + created.OwnerToken}, "Origin": []string{"https://forbidden.example"}},
		Subprotocols: []string{WebSocketSubprotocol},
	})
	if err == nil {
		t.Fatal("forbidden origin WebSocket dial succeeded")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("forbidden origin status = %#v, want 403", response)
	}

	_, response, err = websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + created.OwnerToken}, "Origin": []string{"http://127.0.0.1"}},
	})
	if err == nil {
		t.Fatal("missing subprotocol WebSocket dial succeeded")
	}
	if response == nil || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing subprotocol status = %#v, want 400", response)
	}
}

func TestWebSocketJoinPingPongAndGracefulClose(t *testing.T) {
	t.Parallel()

	_, created, testServer := startHTTPTestSession(t)
	websocketURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/v1/collaboration"
	connection, _, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + created.OwnerToken}, "Origin": []string{"http://127.0.0.1"}},
		Subprotocols: []string{WebSocketSubprotocol},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.CloseNow() }()

	writeClientEnvelope(t, connection, protocol.ClientEnvelope{
		ProtocolVersion: model.ProtocolVersion,
		MessageID:       "join-1",
		SessionID:       created.SessionID,
		Type:            protocol.ClientJoin,
	}, protocol.JoinPayload{JoinToken: created.OwnerToken})
	joined := readServerEnvelope(t, connection)
	if joined.Envelope.Type != protocol.ServerJoined {
		t.Fatalf("first server message = %q, want joined", joined.Envelope.Type)
	}

	writeClientEnvelope(t, connection, protocol.ClientEnvelope{
		ProtocolVersion: model.ProtocolVersion,
		MessageID:       "ping-1",
		SessionID:       created.SessionID,
		Type:            protocol.ClientPing,
	}, protocol.PingPayload{Nonce: "nonce-1"})
	pong := readServerEnvelopeType(t, connection, protocol.ServerPong)
	if pong.Envelope.Type != protocol.ServerPong {
		t.Fatalf("server message = %q, want pong", pong.Envelope.Type)
	}
	if err := connection.Close(websocket.StatusNormalClosure, "test complete"); err != nil {
		t.Fatal(err)
	}
}

func TestWebSocketRotatesSingleUseResumptionCredential(t *testing.T) {
	t.Parallel()

	_, created, testServer := startHTTPTestSession(t)
	first, firstCredential := connectTestClientWithCredential(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	if err := first.Close(websocket.StatusNormalClosure, "rotate credential"); err != nil {
		t.Fatal(err)
	}
	second, secondCredential := connectTestClientWithCredential(t, testServer.URL, created.SessionID, firstCredential, 0)
	defer func() { _ = second.CloseNow() }()
	if secondCredential == "" || secondCredential == firstCredential {
		t.Fatalf("rotated credential = %q, want a fresh non-empty value", secondCredential)
	}
	joinBody, err := json.Marshal(map[string]any{"role": RoleEditor, "display_name": "Denied"})
	if err != nil {
		t.Fatal(err)
	}
	joinResponse := postJSON(t, testServer.URL+"/v1/sessions/"+created.SessionID+"/join-tokens", secondCredential, joinBody)
	_ = joinResponse.Body.Close()
	if joinResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("resumption credential administrative status = %d, want %d", joinResponse.StatusCode, http.StatusForbidden)
	}

	websocketURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/v1/collaboration"
	for _, staleCredential := range []string{created.OwnerToken, firstCredential} {
		stale, response, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
			HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + staleCredential}, "Origin": []string{"http://127.0.0.1"}},
			Subprotocols: []string{WebSocketSubprotocol},
		})
		if stale != nil {
			_ = stale.CloseNow()
		}
		if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("reused credential dial = response %#v error %v, want HTTP 401", response, err)
		}
	}
}

func TestWebSocketRequiresSnapshotWhenAcknowledgedRevisionPredatesRetainedHistory(t *testing.T) {
	t.Parallel()

	service, created, testServer := startHTTPTestSession(t)
	record := service.sessions[created.SessionID]
	initial, err := record.owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := record.owner.Submit(context.Background(), testOperation(t, initial, 1)); err != nil {
		t.Fatal(err)
	}
	compacted, err := record.owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SaveSnapshot(context.Background(), compacted); err != nil {
		t.Fatal(err)
	}

	websocketURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/v1/collaboration"
	connection, _, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + created.OwnerToken}, "Origin": []string{"http://127.0.0.1"}},
		Subprotocols: []string{WebSocketSubprotocol},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.CloseNow() }()
	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "stale-join", SessionID: created.SessionID, Type: protocol.ClientJoin}, protocol.JoinPayload{JoinToken: created.OwnerToken, AcknowledgedRevision: initial.Revision})
	joined := readServerEnvelope(t, connection)
	if joined.Envelope.Type != protocol.ServerJoined {
		t.Fatalf("first message = %q, want joined", joined.Envelope.Type)
	}
	resumptionToken := joined.Payload.(*protocol.JoinedPayload).ResumptionToken
	notice := readServerEnvelope(t, connection)
	if notice.Envelope.Type != protocol.ServerSessionNotice || notice.Payload.(*protocol.SessionNoticePayload).Code != protocol.NoticeSnapshotRequired {
		t.Fatalf("fallback message = %#v, want snapshot-required notice", notice)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err = connection.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusServiceRestart {
		t.Fatalf("fallback close status = %d error %v, want %d", websocket.CloseStatus(err), err, websocket.StatusServiceRestart)
	}
	request, err := http.NewRequest(http.MethodGet, testServer.URL+"/v1/sessions/"+created.SessionID+"/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+resumptionToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resumption snapshot status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var fetched model.Snapshot
	if err := json.NewDecoder(response.Body).Decode(&fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.Revision != compacted.Revision {
		t.Fatalf("resumption snapshot revision = %d, want %d", fetched.Revision, compacted.Revision)
	}
}

func connectTestClientWithCredential(t *testing.T, baseURL, sessionID, token string, acknowledged model.Revision) (*websocket.Conn, string) {
	t.Helper()
	websocketURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/v1/collaboration"
	connection, _, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + token}, "Origin": []string{"http://127.0.0.1"}},
		Subprotocols: []string{WebSocketSubprotocol},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "join", SessionID: sessionID, Type: protocol.ClientJoin}, protocol.JoinPayload{JoinToken: token, AcknowledgedRevision: acknowledged})
	joined := readServerEnvelope(t, connection)
	if joined.Envelope.Type != protocol.ServerJoined {
		t.Fatalf("first message = %q, want joined", joined.Envelope.Type)
	}
	payload := joined.Payload.(*protocol.JoinedPayload)
	if acknowledged == 0 {
		_ = readServerEnvelopeType(t, connection, protocol.ServerReplayComplete)
		_ = readServerEnvelopeType(t, connection, protocol.ServerPresenceSnapshot)
	}
	return connection, payload.ResumptionToken
}

func readServerEnvelopeType(t *testing.T, connection *websocket.Conn, wanted protocol.ServerType) protocol.DecodedServer {
	t.Helper()
	for {
		message := readServerEnvelope(t, connection)
		if message.Envelope.Type == wanted {
			return message
		}
	}
}

func startHTTPTestSession(t *testing.T) (*Service, CreateSessionResponse, *httptest.Server) {
	t.Helper()
	return startHTTPTestSessionWithConfig(t, ServiceConfig{AllowedOrigins: []string{"http://127.0.0.1"}})
}

func startHTTPTestSessionWithConfig(t *testing.T, config ServiceConfig) (*Service, CreateSessionResponse, *httptest.Server) {
	t.Helper()
	config.OnWebSocketError = func(error) {}
	service := NewService(config)
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	launchToken, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(service.Handler())
	t.Cleanup(testServer.Close)
	body, err := json.Marshal(map[string]any{"snapshot": testSnapshot(t, 2)})
	if err != nil {
		t.Fatal(err)
	}
	response := postJSON(t, testServer.URL+"/v1/sessions", launchToken, body)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create session status = %d", response.StatusCode)
	}
	var created CreateSessionResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return service, created, testServer
}

func writeClientEnvelope(t *testing.T, connection *websocket.Conn, envelope protocol.ClientEnvelope, payload any) {
	t.Helper()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Payload = payloadJSON
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := connection.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func readServerEnvelope(t *testing.T, connection *websocket.Conn) protocol.DecodedServer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	messageType, data, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if messageType != websocket.MessageText {
		t.Fatalf("message type = %d, want text", messageType)
	}
	decoded, err := protocol.DecodeServer(data)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
