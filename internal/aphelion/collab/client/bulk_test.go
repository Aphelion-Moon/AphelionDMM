package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
	"sdmm/internal/aphelion/collab/store/sqlite"
)

type pagedBulkTestStore struct{ *sqlite.Store }

func (s *pagedBulkTestStore) LoadReplay(context.Context, model.DocumentID) (model.Snapshot, []model.AcceptedOperation, map[model.Revision]string, error) {
	return model.Snapshot{}, nil, nil, fmt.Errorf("bulk session must not materialize the complete replay history")
}

func TestBulkSessionWholeLevelAcceptanceRetryInverse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	database, err := sqlite.Open(filepath.Join(t.TempDir(), "bulk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	service := server.NewService(server.ServiceConfig{Store: &pagedBulkTestStore{Store: database}, AllowedOrigins: []string{"http://127.0.0.1"}})
	defer func() { _ = service.Shutdown(context.Background()) }()
	host := httptest.NewServer(service.Handler())
	defer host.Close()
	token, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	id, _ := model.NewDocumentID()
	snapshot := model.Snapshot{ProtocolVersion: 1, SchemaVersion: 1, DocumentID: id, EnvironmentHash: strings.Repeat("a", 64), MaxX: 256, MaxY: 256, MaxZ: 1}
	for i := 0; i < 65536; i++ {
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: i%256 + 1, Y: i/256 + 1, Z: 1}})
	}
	body, _ := json.Marshal(map[string]any{"snapshot": snapshot, "bulk_edits": true})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, host.URL+"/v1/sessions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("bulk session create: HTTP %d", response.StatusCode)
	}
	var created server.CreateSessionResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	transport := NewWebSocketTransport(TransportConfig{})
	received := make(chan protocol.ServerEnvelope, 16)
	var execution atomic.Pointer[NetworkExecutor]
	onReceive := func(e protocol.ServerEnvelope) {
		// The altered retry below is intentionally sent directly through the
		// transport after its original operation has already left Pending.
		if current := execution.Load(); current != nil && e.Type != protocol.ServerOperationRejected {
			if err := current.Receive(e); err != nil {
				t.Error(err)
			}
		}
		received <- e
	}
	if err := transport.Connect(ctx, protocol.JoinRequest{BaseURL: host.URL, Origin: "http://127.0.0.1", Token: created.OwnerToken, SessionID: created.SessionID}, onReceive); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transport.Close(websocket.StatusNormalClosure, "done") }()
	next := func(kind protocol.ServerType) protocol.DecodedServer {
		t.Helper()
		for {
			select {
			case e := <-received:
				d, err := protocol.DecodeServerEnvelope(e)
				if err != nil {
					t.Fatal(err)
				}
				if e.Type == kind {
					return d
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	joined := next(protocol.ServerJoined).Payload.(*protocol.JoinedPayload)
	next(protocol.ServerReplayComplete)
	network, err := NewNetworkExecutor(transport, snapshot, joined.ActorID, created.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	execution.Store(network)
	peer, nextPeer := connectBulkObserver(t, ctx, host.URL, created.SessionID, created.OwnerToken, snapshot)
	presence, _ := json.Marshal(protocol.PresenceUpdatePayload{Sequence: 1, Selection: &protocol.PresenceSelection{Min: model.Coord{X: 1, Y: 1, Z: 1}, Max: model.Coord{X: 256, Y: 256, Z: 1}}, Status: "active"})
	if err := transport.Send(ctx, protocol.ClientEnvelope{ProtocolVersion: 1, MessageID: "whole-selection", SessionID: created.SessionID, Type: protocol.ClientPresenceUpdate, Payload: presence}); err != nil {
		t.Fatal(err)
	}
	next(protocol.ServerPresenceUpdate)
	opID, _ := model.NewOperationID()
	hash, _ := snapshot.Hash()
	op := model.Operation{ProtocolVersion: 1, DocumentID: id, OperationID: opID, ActorID: joined.ActorID, EnvironmentHash: snapshot.EnvironmentHash, BaseMapHash: hash, Kind: model.OperationKindTileChange}
	for i := 0; i < 65536; i++ {
		op.Changes = append(op.Changes, model.TileChange{Coord: model.Coord{X: i%256 + 1, Y: i/256 + 1, Z: 1}, After: model.TileState{Prefabs: []model.PrefabState{{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", i+1)), Path: "/obj/unknown", Vars: map[string]string{"raw": `list("a" = 12)`}}}}})
	}
	for retry := 0; retry < 2; retry++ {
		var err error
		if retry == 0 {
			_, err = network.Execute(ctx, op)
		} else {
			err = transport.SendOperation(ctx, created.SessionID, op)
		}
		if err != nil {
			t.Fatal(err)
		}
		accepted := next(protocol.ServerOperationAccepted).Payload.(*protocol.OperationAcceptedPayload)
		if accepted.Operation.Revision != 1 || len(accepted.Operation.Changes) != 65536 {
			t.Fatal("bulk edit split or retry appended")
		}
		if retry == 0 {
			observed := nextPeer()
			if observed.Operation.Revision != 1 || observed.MapHash != accepted.MapHash {
				t.Fatal("second peer did not converge on whole-level publication")
			}
		}
		t.Logf("whole-level acceptance and peer projection complete; retry=%d", retry)
	}
	op.Changes[65535].After.Prefabs[0].Vars["raw"] = "different body with same identity"
	if err := transport.SendOperation(ctx, created.SessionID, op); err != nil {
		t.Fatal(err)
	}
	rejected := next(protocol.ServerOperationRejected).Payload.(*protocol.OperationRejectedPayload)
	if rejected.Revision != 1 {
		t.Fatal("conflicting retry changed revision")
	}
	payload, _ := json.Marshal(protocol.InverseRequestPayload{OperationID: opID})
	if err := transport.Send(ctx, protocol.ClientEnvelope{ProtocolVersion: 1, MessageID: "inverse", SessionID: created.SessionID, Type: protocol.ClientInverseRequest, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	inverse := next(protocol.ServerOperationAccepted).Payload.(*protocol.OperationAcceptedPayload)
	if inverse.Operation.Revision != 2 || inverse.MapHash != hash {
		t.Fatalf("whole level inverse failed: revision %d, hash %s, expected %s", inverse.Operation.Revision, inverse.MapHash, hash)
	}
	projected, err := network.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	projectedHash, _ := projected.Hash()
	if projected.Revision != 2 || projectedHash != hash {
		t.Fatal("client projection differs after full-level undo")
	}
	t.Log("whole-level inverse and submitting projection complete")
	observedInverse := nextPeer()
	peerSnapshot, err := peer.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	peerHash, err := peerSnapshot.Hash()
	if err != nil || observedInverse.Operation.Revision != 2 || peerSnapshot.Revision != 2 || peerHash != hash {
		t.Fatal("second peer projection differs after full-level undo")
	}
	execution.Store(nil)
	_ = transport.Close(websocket.StatusNormalClosure, "reconnect")
	_ = transport.Wait(ctx)
	// A client without the capability is rejected before any durable delivery,
	// including when the current map is back to its empty starting contents.
	legacy, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(host.URL, "http")+"/v1/collaboration", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://127.0.0.1"}, "Authorization": []string{"Bearer " + joined.ResumptionToken}}, Subprotocols: []string{server.WebSocketSubprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	joinPayload, _ := json.Marshal(protocol.JoinPayload{JoinToken: joined.ResumptionToken})
	joinBytes, _ := json.Marshal(protocol.ClientEnvelope{ProtocolVersion: 1, MessageID: "legacy", SessionID: created.SessionID, Type: protocol.ClientJoin, Payload: joinPayload})
	if err := legacy.Write(ctx, websocket.MessageText, joinBytes); err != nil {
		t.Fatal(err)
	}
	_, _, err = legacy.Read(ctx)
	_ = legacy.CloseNow()
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("legacy client joined bulk session: %v", err)
	}
	transport = NewWebSocketTransport(TransportConfig{})
	received = make(chan protocol.ServerEnvelope, 16)
	if err := transport.Connect(ctx, protocol.JoinRequest{BaseURL: host.URL, Origin: "http://127.0.0.1", Token: joined.ResumptionToken, SessionID: created.SessionID}, func(e protocol.ServerEnvelope) { received <- e }); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transport.Close(websocket.StatusNormalClosure, "done") }()
	next(protocol.ServerJoined)
	for revision := model.Revision(1); revision <= 2; revision++ {
		replayed := next(protocol.ServerOperationAccepted).Payload.(*protocol.OperationAcceptedPayload)
		if replayed.Operation.Revision != revision || len(replayed.Operation.Changes) != 65536 {
			t.Fatal("bulk replay lost atomic event")
		}
	}
	complete := next(protocol.ServerReplayComplete).Payload.(*protocol.ReplayCompletePayload)
	if complete.Revision != 2 || complete.MapHash != hash {
		t.Fatal("bulk replay final authority differs")
	}
}

// A distinct viewer exercises unsolicited shared publication and projection,
// separately from the submitting client's acknowledgement path.
func connectBulkObserver(t *testing.T, ctx context.Context, baseURL, sessionID, ownerToken string, snapshot model.Snapshot) (*NetworkExecutor, func() *protocol.OperationAcceptedPayload) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/sessions/"+sessionID+"/join-tokens", strings.NewReader(`{"role":"viewer","display_name":"Observer"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("observer credential: HTTP %d", response.StatusCode)
	}
	var credential struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&credential); err != nil {
		t.Fatal(err)
	}
	transport := NewWebSocketTransport(TransportConfig{})
	var execution atomic.Pointer[NetworkExecutor]
	joined := make(chan model.ActorID, 1)
	ready := make(chan struct{}, 1)
	accepted := make(chan *protocol.OperationAcceptedPayload, 4)
	err = transport.Connect(ctx, protocol.JoinRequest{BaseURL: baseURL, Origin: "http://127.0.0.1", Token: credential.Token, SessionID: sessionID}, func(envelope protocol.ServerEnvelope) {
		decoded, decodeErr := protocol.DecodeServerEnvelope(envelope)
		if decodeErr != nil {
			t.Error(decodeErr)
			return
		}
		if current := execution.Load(); current != nil {
			if receiveErr := current.Receive(envelope); receiveErr != nil {
				t.Error(receiveErr)
			}
		}
		switch value := decoded.Payload.(type) {
		case *protocol.JoinedPayload:
			joined <- value.ActorID
		case *protocol.ReplayCompletePayload:
			ready <- struct{}{}
		case *protocol.OperationAcceptedPayload:
			accepted <- value
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = transport.Close(websocket.StatusNormalClosure, "observer done")
		_ = transport.Wait(context.Background())
	})
	var actor model.ActorID
	select {
	case actor = <-joined:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	network, err := NewNetworkExecutor(transport, snapshot, actor, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	execution.Store(network)
	return network, func() *protocol.OperationAcceptedPayload {
		t.Helper()
		select {
		case value := <-accepted:
			return value
		case <-ctx.Done():
			t.Fatal(ctx.Err())
			return nil
		}
	}
}
