package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
)

func TestWebSocketTransportClassifiesRateLimitedClose(t *testing.T) {
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
		_ = connection.Close(server.CloseRateLimited, "presence rate exceeded")
	}))
	defer testServer.Close()

	transport := NewWebSocketTransport(TransportConfig{})
	if err := transport.Connect(context.Background(), protocol.JoinRequest{BaseURL: testServer.URL, Origin: "http://127.0.0.1", Token: "token", SessionID: "session"}, func(protocol.ServerEnvelope) {}); err != nil {
		t.Fatal(err)
	}
	waitContext, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	err := transport.Wait(waitContext)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("Wait() = %v, want ErrRateLimited", err)
	}
	if websocket.CloseStatus(err) != server.CloseRateLimited {
		t.Fatalf("close status = %d, want %d preserved", websocket.CloseStatus(err), server.CloseRateLimited)
	}
	if !strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("error %q does not tell the user the session reconnects", err)
	}
}

func TestClassifyCloseErrorLeavesOtherErrorsAlone(t *testing.T) {
	t.Parallel()

	if ClassifyCloseError(nil) != nil {
		t.Fatal("nil error was classified")
	}
	plain := errors.New("plain")
	if got := ClassifyCloseError(plain); got != plain {
		t.Fatalf("plain error = %v, want unchanged", got)
	}
	policy := websocket.CloseError{Code: websocket.StatusPolicyViolation}
	if errors.Is(ClassifyCloseError(policy), ErrRateLimited) {
		t.Fatal("policy close classified as rate limited")
	}
	once := ClassifyCloseError(websocket.CloseError{Code: server.CloseRateLimited})
	if twice := ClassifyCloseError(once); twice != once {
		t.Fatal("classification is not idempotent")
	}
	if errors.Is(once, ErrAuthenticationDenied) {
		t.Fatal("rate limit must not be treated as an authentication failure")
	}
}
