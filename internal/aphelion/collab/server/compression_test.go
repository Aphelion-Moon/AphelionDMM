package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Collaboration sockets negotiate per-message deflate without a shared
// window, and the message limit applies to the decompressed size, so a small
// compressed frame cannot expand past it.
func TestWebSocketCompressesPerMessageAndLimitsDecompressedSize(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxWebSocketMessageBytes = 8 << 10
	_, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{AllowedOrigins: []string{"http://127.0.0.1"}, Limits: limits})
	token := createTestJoinToken(t, testServer.URL, created.SessionID, created.OwnerToken, RoleEditor, "Editor")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(testServer.URL, "http")+"/v1/collaboration", &websocket.DialOptions{
		HTTPHeader:      http.Header{"Authorization": []string{"Bearer " + token}, "Origin": []string{"http://127.0.0.1"}},
		Subprotocols:    []string{WebSocketSubprotocol},
		CompressionMode: websocket.CompressionNoContextTakeover,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.CloseNow() }()
	extensions := response.Header.Get("Sec-WebSocket-Extensions")
	if !strings.Contains(extensions, "permessage-deflate") || !strings.Contains(extensions, "server_no_context_takeover") {
		t.Fatalf("negotiated extensions = %q, want per-message deflate without context takeover", extensions)
	}
	// 1 MiB of one byte compresses to about 1 KiB on the wire.
	bomb := `{"protocol_version":2,"message_id":"m","session_id":"` + created.SessionID + `","type":"ping","payload":{"nonce":"` + strings.Repeat("a", 1<<20) + `"}}`
	if err := connection.Write(ctx, websocket.MessageText, []byte(bomb)); err != nil {
		t.Fatal(err)
	}
	for {
		if _, _, err := connection.Read(ctx); err != nil {
			if status := websocket.CloseStatus(err); status != websocket.StatusMessageTooBig {
				t.Fatalf("expanding message closed with %v (%v), want message too big", status, err)
			}
			return
		}
	}
}

// An outdated editor joining with a retired protocol is told which versions
// differ instead of receiving a generic "valid join required".
func TestWebSocketNamesUnsupportedJoinProtocol(t *testing.T) {
	t.Parallel()

	_, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{AllowedOrigins: []string{"http://127.0.0.1"}})
	token := createTestJoinToken(t, testServer.URL, created.SessionID, created.OwnerToken, RoleEditor, "Editor")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(testServer.URL, "http")+"/v1/collaboration", &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + token}, "Origin": []string{"http://127.0.0.1"}},
		Subprotocols: []string{WebSocketSubprotocol},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.CloseNow() }()
	join := `{"protocol_version":1,"message_id":"join","session_id":"` + created.SessionID + `","type":"join","payload":{"join_token":"` + token + `","acknowledged_revision":0}}`
	if err := connection.Write(ctx, websocket.MessageText, []byte(join)); err != nil {
		t.Fatal(err)
	}
	_, _, err = connection.Read(ctx)
	var closed websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != websocket.StatusPolicyViolation || !strings.Contains(closed.Reason, "unsupported collaboration protocol v1") || !strings.Contains(closed.Reason, "requires v2") {
		t.Fatalf("v1 join closed with %v", err)
	}
}
