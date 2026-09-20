package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
)

func TestWebSocketTransportConnectSendAndClose(t *testing.T) {
	t.Parallel()

	baseURL, sessionID, token, _, shutdown := startClientTestService(t)
	defer shutdown()
	transport := NewWebSocketTransport(TransportConfig{})
	received := make(chan protocol.ServerEnvelope, 16)
	if err := transport.Connect(context.Background(), protocol.JoinRequest{BaseURL: baseURL, Origin: "http://127.0.0.1", Token: token, SessionID: sessionID}, func(message protocol.ServerEnvelope) {
		received <- message
	}); err != nil {
		t.Fatal(err)
	}
	if message := waitForServerType(t, received, protocol.ServerJoined); message.Type != protocol.ServerJoined {
		t.Fatal("transport did not receive joined")
	}
	payload, err := json.Marshal(protocol.PingPayload{Nonce: "nonce"})
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.Send(context.Background(), protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "ping", SessionID: sessionID, Type: protocol.ClientPing, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if message := waitForServerType(t, received, protocol.ServerPong); message.Type != protocol.ServerPong {
		t.Fatal("transport did not receive pong")
	}
	if err := transport.Close(websocket.StatusNormalClosure, "test complete"); err != nil {
		t.Fatal(err)
	}
	waitContext, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	if err := transport.Wait(waitContext); err != nil && websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("Wait() error = %v after explicit close", err)
	}
}

func TestWebSocketTransportRejectsMalformedServerEnvelope(t *testing.T) {
	t.Parallel()

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{server.WebSocketSubprotocol}})
		if err != nil {
			return
		}
		defer func() { _ = connection.CloseNow() }()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _, _ = connection.Read(ctx)
		_ = connection.Write(ctx, websocket.MessageText, []byte(`{"protocol_version":1,"type":"unknown"}`))
	}))
	defer testServer.Close()

	transport := NewWebSocketTransport(TransportConfig{})
	if err := transport.Connect(context.Background(), protocol.JoinRequest{BaseURL: testServer.URL, Origin: "http://127.0.0.1", Token: "token", SessionID: "session"}, func(protocol.ServerEnvelope) {}); err != nil {
		t.Fatal(err)
	}
	waitContext, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	if err := transport.Wait(waitContext); err == nil || !strings.Contains(err.Error(), "decode server envelope") {
		t.Fatalf("Wait() error = %v, want malformed envelope error", err)
	}
}

func TestWebSocketTransportPreservesServerCloseCode(t *testing.T) {
	t.Parallel()

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{server.WebSocketSubprotocol}})
		if err != nil {
			return
		}
		defer func() { _ = connection.CloseNow() }()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _, _ = connection.Read(ctx)
		_ = connection.Close(websocket.StatusPolicyViolation, "policy violation")
	}))
	defer testServer.Close()

	transport := NewWebSocketTransport(TransportConfig{})
	if err := transport.Connect(context.Background(), protocol.JoinRequest{BaseURL: testServer.URL, Origin: "http://127.0.0.1", Token: "token", SessionID: "session"}, func(protocol.ServerEnvelope) {}); err != nil {
		t.Fatal(err)
	}
	waitContext, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	if err := transport.Wait(waitContext); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("Wait() close status = %d from %v, want %d", websocket.CloseStatus(err), err, websocket.StatusPolicyViolation)
	}
}

func TestWebSocketTransportConnectHonorsDialDeadline(t *testing.T) {
	t.Parallel()

	requestStarted := make(chan struct{})
	testServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer testServer.Close()

	transport := NewWebSocketTransport(TransportConfig{DialTimeout: time.Second})
	err := transport.Connect(context.Background(), protocol.JoinRequest{BaseURL: testServer.URL, Origin: "http://127.0.0.1", Token: "token", SessionID: "session"}, func(protocol.ServerEnvelope) {})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Connect() error = %v, want context deadline exceeded", err)
	}
	select {
	case <-requestStarted:
	default:
		t.Fatal("dial deadline expired before reaching the test server")
	}
}

func TestWebSocketTransportClassifiesPermanentReconnectFailures(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		status int
		want   error
	}{
		"authentication": {status: http.StatusUnauthorized, want: ErrAuthenticationDenied},
		"protocol":       {status: http.StatusUpgradeRequired, want: ErrIncompatibleProtocol},
	} {
		name, testCase := name, testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
			}))
			defer testServer.Close()
			transport := NewWebSocketTransport(TransportConfig{})
			err := transport.Connect(context.Background(), protocol.JoinRequest{BaseURL: testServer.URL, Origin: "http://127.0.0.1", Token: "token", SessionID: "session"}, func(protocol.ServerEnvelope) {})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Connect() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestCollaborationURLRequiresTLSOutsideLoopback(t *testing.T) {
	t.Parallel()

	if _, err := collaborationURL("http://example.invalid"); err == nil {
		t.Fatal("collaborationURL accepted cleartext non-loopback URL")
	}
	if actual, err := collaborationURL("http://127.0.0.1:1234"); err != nil || actual != "ws://127.0.0.1:1234/v1/collaboration" {
		t.Fatalf("loopback collaborationURL = %q, %v", actual, err)
	}
}

func startClientTestService(t *testing.T) (string, string, string, model.Snapshot, func()) {
	t.Helper()
	return startClientTestServiceWithConfig(t, server.ServiceConfig{AllowedOrigins: []string{"http://127.0.0.1"}})
}

func startClientTestServiceWithConfig(t *testing.T, config server.ServiceConfig) (string, string, string, model.Snapshot, func()) {
	t.Helper()
	service := server.NewService(config)
	launchToken, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(service.Handler())
	documentID, err := model.NewDocumentID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: documentID, EnvironmentHash: strings.Repeat("a", 64), MaxX: 1, MaxY: 1, MaxZ: 1}
	body, err := json.Marshal(map[string]any{"snapshot": snapshot})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, testServer.URL+"/v1/sessions", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+launchToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	var created server.CreateSessionResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return testServer.URL, created.SessionID, created.OwnerToken, snapshot, func() {
		testServer.Close()
		_ = service.Shutdown(context.Background())
	}
}

func waitForServerType(t *testing.T, messages <-chan protocol.ServerEnvelope, wanted protocol.ServerType) protocol.ServerEnvelope {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case message := <-messages:
			if message.Type == wanted {
				return message
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q", wanted)
		}
	}
}
