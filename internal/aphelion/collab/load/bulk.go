package load

import (
	"context"
	"fmt"
	"time"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/bulktransport"
	"sdmm/internal/aphelion/collab/protocol"
)

func decodeLoadEnvelope(ctx context.Context, connection loadReader, kind websocket.MessageType, data []byte) (protocol.DecodedServer, error) {
	if kind != websocket.MessageBinary {
		peer, ok := connection.(interface{ Subprotocol() string })
		return protocol.DecodeServerForSession(data, ok && peer.Subprotocol() == protocol.BulkSubprotocol)
	}
	peer, ok := connection.(interface{ Subprotocol() string })
	if !ok || peer.Subprotocol() != protocol.BulkSubprotocol {
		return protocol.DecodedServer{}, fmt.Errorf("unnegotiated bulk load message")
	}
	codec := bulktransport.New("", bulktransport.DefaultSpoolBytes)
	h, op, err := codec.Read(ctx, connection, data, 5*time.Second)
	if err != nil {
		return protocol.DecodedServer{}, err
	}
	return bulktransport.Acceptance(h, op)
}
