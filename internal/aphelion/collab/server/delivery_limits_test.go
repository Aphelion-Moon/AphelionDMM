package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestDocumentOwnerTileLimitAndInverse(t *testing.T) {
	snapshot := testSnapshot(t, 65)
	snapshot.MaxY = 65
	for i := 0; i <= engine.MaxTileChanges; i++ {
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: i%65 + 1, Y: i/65 + 1, Z: 1}})
	}
	store := NewMemoryStore()
	owner, err := StartDocument(context.Background(), snapshot, store)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close(context.Background()) }()
	operation := testOperation(t, snapshot, 1)
	operation.Changes = make([]model.TileChange, engine.MaxTileChanges)
	for i := range operation.Changes {
		operation.Changes[i] = model.TileChange{Coord: snapshot.Tiles[i].Coord, After: model.TileState{Prefabs: []model.PrefabState{{
			StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", i+1)), Path: "/obj/test", Vars: map[string]string{},
		}}}}
	}
	accepted, err := owner.Submit(context.Background(), operation)
	if err != nil || accepted.Revision != 1 || len(accepted.Changes) != engine.MaxTileChanges {
		t.Fatalf("4096-tile acceptance: revision=%d count=%d err=%v", accepted.Revision, len(accepted.Changes), err)
	}
	inverseID, err := model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	inverse, err := owner.BuildInverse(context.Background(), operation.ActorID, operation.OperationID, inverseID)
	if err != nil {
		t.Fatal(err)
	}
	if undone, err := owner.Submit(context.Background(), inverse); err != nil || undone.Revision != 2 {
		t.Fatalf("4096-tile inverse: revision=%d err=%v", undone.Revision, err)
	}
	current, err := owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	got, err := current.Hash()
	if err != nil || got != want {
		t.Fatal("4096-tile inverse did not restore the original hash")
	}
	operation.OperationID, err = model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	operation.Changes = append(operation.Changes, model.TileChange{Coord: snapshot.Tiles[engine.MaxTileChanges].Coord})
	if _, err := owner.Submit(context.Background(), operation); engine.CodeOf(err) != engine.CodeInvalidOperation {
		t.Fatalf("4097-tile operation was not rejected: %v", err)
	}
	_, operations, err := store.Load(context.Background(), snapshot.DocumentID)
	if err != nil || len(operations) != 2 {
		t.Fatalf("tile-limit refusal entered durable history: count=%d err=%v", len(operations), err)
	}
}

func TestWebSocketLargeAcceptanceDuplicateInverseAndReplay(t *testing.T) {
	service, created, httpServer := startHTTPTestSession(t)
	owner := service.sessions[created.SessionID].owner
	snapshot, err := owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	connection := connectTestClient(t, httpServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	connection.SetReadLimit(protocol.MaxMessageBytes)
	operation := testOperation(t, snapshot, 1)
	operation.Changes[0].After.Prefabs[0].Vars["payload"] = strings.Repeat("x", protocol.MaxMessageBytes-4096)
	submitTestOperation(t, connection, created.SessionID, "large", operation)
	accepted := readAccepted(t, connection)
	if accepted.Revision != 1 {
		t.Fatal("large operation was not accepted")
	}
	// Maximum-length escaped request IDs must not produce invalid response IDs.
	submitTestOperation(t, connection, created.SessionID, strings.Repeat("<", protocol.MaxIdentifierBytes), operation)
	if duplicate := readAccepted(t, connection); duplicate.Revision != 1 || duplicate.OperationID != accepted.OperationID {
		t.Fatal("duplicate changed revision or operation identity")
	}
	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "undo", SessionID: created.SessionID, Type: protocol.ClientInverseRequest}, protocol.InverseRequestPayload{OperationID: accepted.OperationID})
	if inverse := readAccepted(t, connection); inverse.Revision != 2 || inverse.InverseOf == nil || *inverse.InverseOf != accepted.OperationID {
		t.Fatal("large actor-scoped inverse was not accepted")
	}
	token := createTestJoinToken(t, httpServer.URL, created.SessionID, created.OwnerToken, RoleViewer, "Replay")
	replay, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/v1/collaboration", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}, "Origin": []string{"http://127.0.0.1"}}, Subprotocols: []string{WebSocketSubprotocol},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = replay.CloseNow() }()
	replay.SetReadLimit(protocol.MaxMessageBytes)
	writeClientEnvelope(t, replay, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "join", SessionID: created.SessionID, Type: protocol.ClientJoin}, protocol.JoinPayload{JoinToken: token})
	readServerEnvelopeType(t, replay, protocol.ServerJoined)
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for revision := model.Revision(1); revision <= 2; revision++ {
		event := readAccepted(t, replay)
		if event.Revision != revision {
			t.Fatal("replay lost operation order")
		}
		applyAccepted(t, document, event)
	}
	complete := readServerEnvelopeType(t, replay, protocol.ServerReplayComplete).Payload.(*protocol.ReplayCompletePayload)
	current, err := owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want, err := current.Hash()
	if err != nil {
		t.Fatal(err)
	}
	got, err := document.Snapshot().Hash()
	if err != nil || complete.Revision != 2 || complete.MapHash != want || got != want {
		t.Fatal("large-operation replay did not converge to the authoritative hash")
	}
}

func TestWebSocketRejectsUndeliverableAcceptanceBeforePersistence(t *testing.T) {
	service, created, httpServer := startHTTPTestSession(t)
	owner := service.sessions[created.SessionID].owner
	snapshot, err := owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	connection := connectTestClient(t, httpServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	connection.SetReadLimit(protocol.MaxMessageBytes)
	operation := testOperation(t, snapshot, 1)
	operation.Changes[0].After.Prefabs[0].Vars["payload"] = ""
	message := protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "edge", SessionID: created.SessionID, Type: protocol.ClientOperationSubmit}
	payload, err := json.Marshal(protocol.OperationSubmitPayload{Operation: operation})
	if err != nil {
		t.Fatal(err)
	}
	message.Payload = payload
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	operation.Changes[0].After.Prefabs[0].Vars["payload"] = strings.Repeat("x", protocol.MaxMessageBytes-len(encoded))
	submitTestOperation(t, connection, created.SessionID, message.MessageID, operation)
	rejected := readServerEnvelopeType(t, connection, protocol.ServerOperationRejected).Payload.(*protocol.OperationRejectedPayload)
	if rejected.Code != "limit_exceeded" || rejected.OperationID != operation.OperationID || rejected.Revision != 0 {
		t.Fatalf("unexpected rejection: %+v", rejected)
	}
	current, err := owner.Snapshot(context.Background())
	if err != nil || current.Revision != 0 {
		t.Fatalf("undeliverable operation changed authority: revision=%d err=%v", current.Revision, err)
	}
	_, operations, err := service.store.Load(context.Background(), snapshot.DocumentID)
	if err != nil || len(operations) != 0 {
		t.Fatalf("undeliverable operation entered durable history: count=%d err=%v", len(operations), err)
	}
	// Reusing the refused ID with a smaller intent proves it was not consumed.
	operation.Changes[0].After.Prefabs[0].Vars["payload"] = "small"
	submitTestOperation(t, connection, created.SessionID, "retry", operation)
	if accepted := readAccepted(t, connection); accepted.Revision != 1 || accepted.OperationID != operation.OperationID {
		t.Fatal("corrected retry was not accepted at the first revision")
	}
}

func TestWebSocketBoundsLargeRejectionWithoutClosing(t *testing.T) {
	service, created, httpServer := startHTTPTestSession(t)
	snapshot, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	connection := connectTestClient(t, httpServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	connection.SetReadLimit(protocol.MaxMessageBytes)
	for x := 1; x <= 2; x++ {
		operation := testOperation(t, snapshot, x)
		operation.Changes[0].After.Prefabs[0].Vars["payload"] = strings.Repeat("x", 600*1024)
		submitTestOperation(t, connection, created.SessionID, "seed", operation)
		if accepted := readAccepted(t, connection); accepted.Revision != model.Revision(x) {
			t.Fatal("seed operation was not accepted")
		}
	}
	stale := testOperation(t, snapshot, 1)
	stale.Changes = append(stale.Changes, testOperation(t, snapshot, 2).Changes[0])
	submitTestOperation(t, connection, created.SessionID, "stale", stale)
	rejected := readServerEnvelopeType(t, connection, protocol.ServerOperationRejected).Payload.(*protocol.OperationRejectedPayload)
	if rejected.Code != "precondition_failed" || rejected.Revision != 2 || rejected.OperationID != stale.OperationID {
		t.Fatalf("unexpected rejection: code=%s revision=%d id=%s", rejected.Code, rejected.Revision, rejected.OperationID)
	}
	if len(rejected.AuthoritativeValues) != 0 {
		t.Fatal("oversized optional conflict details were not omitted")
	}
	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "alive", SessionID: created.SessionID, Type: protocol.ClientPing}, protocol.PingPayload{Nonce: "alive"})
	readServerEnvelopeType(t, connection, protocol.ServerPong)
	current, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil || current.Revision != 2 {
		t.Fatalf("rejection changed authority: revision=%d err=%v", current.Revision, err)
	}
}
