package protocol

import (
	"encoding/json"
	"sdmm/internal/aphelion/collab/model"
	"testing"
)

func TestWholeLevelPresenceRequiresBulkCapability(t *testing.T) {
	payload, _ := json.Marshal(PresenceUpdatePayload{Sequence: 1, Selection: &PresenceSelection{Min: model.Coord{X: 1, Y: 1, Z: 1}, Max: model.Coord{X: 256, Y: 256, Z: 1}}, Status: "active"})
	data, _ := json.Marshal(ClientEnvelope{ProtocolVersion: 1, MessageID: "presence", SessionID: "session", Type: ClientPresenceUpdate, Payload: payload})
	if _, err := DecodeClient(data); err == nil {
		t.Fatal("legacy count contract changed")
	}
	if _, err := DecodeClientForSession(data, true); err != nil {
		t.Fatalf("whole-level bulk presence: %v", err)
	}
	payload, _ = json.Marshal(PresenceUpdatePayload{Sequence: 1, Selection: &PresenceSelection{Min: model.Coord{X: 1, Y: 1, Z: 1}, Max: model.Coord{X: int(^uint(0) >> 1), Y: 256, Z: 1}}, Status: "active"})
	data, _ = json.Marshal(ClientEnvelope{ProtocolVersion: 1, MessageID: "presence", SessionID: "session", Type: ClientPresenceUpdate, Payload: payload})
	if _, err := DecodeClientForSession(data, true); err == nil {
		t.Fatal("overflowed presence coordinates accepted")
	}
}
