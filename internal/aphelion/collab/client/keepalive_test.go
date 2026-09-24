package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestTransportKeepsIdlePreparationConnectionAlive(t *testing.T) {
	result := make(chan protocol.DecodedClient, 1)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{collaborationSubprotocol}})
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if _, _, err := c.Read(ctx); err != nil {
			return
		}
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		decoded, err := protocol.DecodeClient(data)
		if err == nil {
			result <- decoded
		}
	}))
	defer host.Close()
	transport := NewWebSocketTransport(TransportConfig{KeepaliveInterval: 20 * time.Millisecond})
	if err := transport.Connect(context.Background(), protocol.JoinRequest{BaseURL: host.URL, Origin: "http://127.0.0.1", Token: "token", SessionID: "session"}, func(protocol.ServerEnvelope) {}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transport.Close(websocket.StatusNormalClosure, "done") }()
	select {
	case message := <-result:
		if message.Envelope.Type != protocol.ClientPing || message.Envelope.SessionID != "session" {
			t.Fatal("invalid keepalive")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("idle preparation had no transport keepalive")
	}
}
