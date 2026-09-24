package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestSessionClientInitialJoinRetriesAfterSnapshotRequired(t *testing.T) {
	t.Parallel()

	initial := controllerSnapshot(t)
	replacement := model.CloneSnapshot(initial)
	replacement.Revision = initial.Revision + 5
	replacementHash := mustSnapshotHash(t, replacement)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	initialToken := "initial-owner-token"
	resumptionToken := "rotated-after-initial-join"
	expiresAt := time.Now().Add(time.Hour)
	var requestMutex sync.Mutex
	var snapshotRequests []string
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/sessions/session-1/snapshot" {
			http.NotFound(writer, request)
			return
		}
		requestMutex.Lock()
		snapshotRequests = append(snapshotRequests, request.Header.Get("Authorization"))
		requestIndex := len(snapshotRequests)
		requestMutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if requestIndex == 1 {
			_ = json.NewEncoder(writer).Encode(initial)
			return
		}
		_ = json.NewEncoder(writer).Encode(replacement)
	}))
	t.Cleanup(httpServer.Close)

	joined := protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "joined", SessionID: "session-1", Type: protocol.ServerJoined, Payload: mustRawJSON(t, protocol.JoinedPayload{
		DocumentID: initial.DocumentID, ActorID: actorID, Role: "owner", Revision: replacement.Revision, MapHash: replacementHash,
		PresenceIntervalMS: 100, ResumptionToken: resumptionToken, ResumptionTokenExpiresAt: expiresAt,
	})}
	snapshotRequired := protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "snapshot-required", SessionID: "session-1", Type: protocol.ServerSessionNotice, Payload: mustRawJSON(t, protocol.SessionNoticePayload{Code: protocol.NoticeSnapshotRequired, Message: "snapshot required"})}
	replayComplete := protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "replay-complete", SessionID: "session-1", Type: protocol.ServerReplayComplete, Payload: mustRawJSON(t, protocol.ReplayCompletePayload{Revision: replacement.Revision, MapHash: replacementHash})}
	first := newScriptedReconnectTransport(joined, snapshotRequired)
	second := newScriptedReconnectTransport(joined, replayComplete)
	transports := []sessionTransport{first, second}
	client := NewSessionClient(SessionClientConfig{Reconnect: collabclient.ReconnectPolicy{MaxAttempts: 2}})
	client.newTransport = func() sessionTransport {
		transport := transports[0]
		transports = transports[1:]
		return transport
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	invit := Invitation{BaseURL: httpServer.URL, Origin: httpServer.URL, SessionID: "session-1", Token: initialToken}
	if err := client.Join(ctx, invit); err != nil {
		t.Fatalf("Join() after snapshot-required fallback: %v", err)
	}
	t.Cleanup(func() { _ = client.Leave(context.Background()) })
	if status := client.Status(); status.State != collabclient.StateCaughtUp || status.Revision != replacement.Revision {
		t.Fatalf("status after initial snapshot fallback = %#v", status)
	}
	requests := first.requests()
	secondRequests := second.requests()
	if len(requests) != 1 || requests[0].Token != initialToken || requests[0].AcknowledgedRevision != initial.Revision {
		t.Fatalf("first join request = %#v", requests)
	}
	if len(secondRequests) != 1 || secondRequests[0].Token != resumptionToken || secondRequests[0].AcknowledgedRevision != replacement.Revision {
		t.Fatalf("retry join request = %#v", secondRequests)
	}
	requestMutex.Lock()
	gotSnapshotRequests := append([]string(nil), snapshotRequests...)
	requestMutex.Unlock()
	if len(gotSnapshotRequests) != 2 || gotSnapshotRequests[0] != "Bearer "+initialToken || gotSnapshotRequests[1] != "Bearer "+resumptionToken {
		t.Fatalf("snapshot request authorizations = %#v", gotSnapshotRequests)
	}
	current, err := client.NetworkExecutor().Snapshot(context.Background())
	if err != nil || current.Revision != replacement.Revision || mustSnapshotHash(t, current) != replacementHash {
		t.Fatalf("joined snapshot = revision %d, hash %s, error %v", current.Revision, mustSnapshotHash(t, current), err)
	}
}

func TestSessionClientInitialJoinReturnsWhenTransportEndsBeforeReplay(t *testing.T) {
	t.Parallel()

	snapshot := controllerSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	snapshotHash := mustSnapshotHash(t, snapshot)
	expiresAt := time.Now().Add(time.Hour)
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(snapshot)
	}))
	t.Cleanup(httpServer.Close)
	joined := protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "joined", SessionID: "session-1", Type: protocol.ServerJoined, Payload: mustRawJSON(t, protocol.JoinedPayload{
		DocumentID: snapshot.DocumentID, ActorID: actorID, Role: "owner", Revision: snapshot.Revision, MapHash: snapshotHash,
		PresenceIntervalMS: 100, ResumptionToken: "rotated-resumption-token", ResumptionTokenExpiresAt: expiresAt,
	})}
	transport := newScriptedReconnectTransport(joined)
	client := NewSessionClient(SessionClientConfig{})
	client.newTransport = func() sessionTransport { return transport }
	invitation := Invitation{BaseURL: httpServer.URL, Origin: httpServer.URL, SessionID: "session-1", Token: "initial-owner-token"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- client.Join(ctx, invitation) }()

	deadline := time.Now().Add(time.Second)
	for len(transport.requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(transport.requests()) == 0 {
		t.Fatal("initial join did not reach transport Connect")
	}
	if err := transport.Close(websocket.StatusNormalClosure, "simulated disconnect before replay completion"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Join() after pre-replay disconnect = %v; want transport failure without waiting for caller deadline", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Join() remained blocked after transport ended before replay completion")
	}
}

func TestInitialJoinUsesQueuedReplayCompleteWhenTransportEnds(t *testing.T) {
	ready := make(chan model.Revision, 1)
	ready <- 42
	errorsFound := make(chan error, 1)
	snapshotRequired := make(chan struct{}, 1)
	transportEnded := make(chan error, 1)
	transportErr := errors.New("socket closed after replay")
	transportEnded <- transportErr
	transportErr = <-transportEnded // Model Join selecting the completed transport result.

	revision, fallback, err := initialJoinOutcomeOnTransportEnd(ready, errorsFound, snapshotRequired, transportErr, transportEnded)
	if err != nil || fallback || revision != 42 {
		t.Fatalf("initial join outcome = revision %d, fallback %t, error %v; want queued replay revision 42", revision, fallback, err)
	}
	select {
	case got := <-transportEnded:
		if !errors.Is(got, transportErr) {
			t.Fatalf("transport monitor received %v; want %v", got, transportErr)
		}
	default:
		t.Fatal("initial join consumed the transport result instead of leaving it for the monitor")
	}
}
