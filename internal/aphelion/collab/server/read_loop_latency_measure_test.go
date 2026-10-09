package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/store/sqlite"
)

// Measurement harness for the per-connection read loop. It is opt-in
// (APHELIONDMM_READ_LOOP_MEASURE=<output file>) and records, for a client doing
// rapid small durable operations plus 100 ms presence, how long a ping takes to
// be answered. A pong cannot be written before every earlier message on the
// connection has been handled, so the pong round trip is the read loop's
// queueing delay.

type latencyAuthorizer struct {
	*fakeHostedAuthorizer
	delay atomic.Int64
	calls atomic.Int64
}

func (authorizer *latencyAuthorizer) AuthorizeSession(ctx context.Context, token, sessionID string) (auth.Session, error) {
	authorizer.calls.Add(1)
	if delay := time.Duration(authorizer.delay.Load()); delay > 0 {
		time.Sleep(delay)
	}
	return authorizer.Authorize(ctx, token)
}

type latencyStore struct {
	*sqlite.Store
	appendDelay time.Duration
	readDelay   time.Duration
	appends     atomic.Int64
	reads       atomic.Int64
}

func (store *latencyStore) Append(ctx context.Context, accepted model.AcceptedOperation) error {
	store.appends.Add(1)
	time.Sleep(store.appendDelay)
	return store.Store.Append(ctx, accepted)
}

func (store *latencyStore) RevisionHash(ctx context.Context, documentID model.DocumentID, revision model.Revision) (string, bool, error) {
	store.reads.Add(1)
	time.Sleep(store.readDelay)
	return store.Store.RevisionHash(ctx, documentID, revision)
}

func (store *latencyStore) LookupOperation(ctx context.Context, documentID model.DocumentID, operationID model.OperationID) (model.AcceptedOperation, bool, error) {
	store.reads.Add(1)
	time.Sleep(store.readDelay)
	return store.Store.LookupOperation(ctx, documentID, operationID)
}

type loopMeasurement struct {
	Name         string  `json:"name"`
	Hosted       bool    `json:"hosted"`
	AuthDelayMS  int64   `json:"auth_delay_ms"`
	AppendMS     int64   `json:"append_delay_ms"`
	StoreReadMS  int64   `json:"store_read_delay_ms"`
	Operations   int     `json:"operations"`
	AckedOps     int     `json:"acked_operations"`
	AckP50MS     float64 `json:"ack_p50_ms"`
	AckP95MS     float64 `json:"ack_p95_ms"`
	AckMaxMS     float64 `json:"ack_max_ms"`
	PingP50MS    float64 `json:"ping_p50_ms"`
	PingP95MS    float64 `json:"ping_p95_ms"`
	PingMaxMS    float64 `json:"ping_max_ms"`
	PresenceSent int     `json:"presence_sent"`
	AuthCalls    int64   `json:"auth_calls"`
	Closed       string  `json:"closed,omitempty"`
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[int(float64(len(sorted)-1)*p)]
}

func runReadLoopMeasurement(t *testing.T, name string, hosted bool, authDelay, appendDelay, readDelay time.Duration, duration time.Duration) loopMeasurement {
	t.Helper()
	database, err := sqlite.Open(filepath.Join(t.TempDir(), "measure.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := &latencyStore{Store: database, appendDelay: appendDelay, readDelay: readDelay}
	config := ServiceConfig{AllowedOrigins: []string{"http://127.0.0.1"}, Store: store, OnWebSocketError: func(error) {}}
	var authorizer *latencyAuthorizer
	if hosted {
		actorID, err := model.NewActorID()
		if err != nil {
			t.Fatal(err)
		}
		authorizer = &latencyAuthorizer{fakeHostedAuthorizer: &fakeHostedAuthorizer{session: auth.Session{
			ActorID: actorID, Issuer: "https://issuer.example", Subject: "editor", DisplayName: "Hosted Editor", Role: auth.RoleOwner, ExpiresAt: time.Now().Add(time.Hour),
		}}}
		config.HostedAuth = authorizer
	}
	service := NewService(config)
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	launchToken, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(service.Handler())
	t.Cleanup(testServer.Close)
	snapshot := testSnapshot(t, 4000)
	created := createTestSession(t, testServer.URL, launchToken, snapshot)
	token := created.OwnerToken
	if hosted {
		token = "hosted-token"
		authorizer.delay.Store(int64(authDelay))
	}
	connection, _ := connectTestClientWithCredential(t, testServer.URL, created.SessionID, token, 0)
	defer func() { _ = connection.CloseNow() }()
	if hosted {
		// The join itself is not under measurement.
		authorizer.calls.Store(0)
	}

	var mutex sync.Mutex
	sentAt := map[string]time.Time{}
	var acks, pings []float64
	closed := ""
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			_, data, err := connection.Read(context.Background())
			if err != nil {
				mutex.Lock()
				closed = fmt.Sprintf("status=%d err=%v", websocket.CloseStatus(err), err)
				mutex.Unlock()
				return
			}
			now := time.Now()
			decoded, err := protocol.DecodeServer(data)
			if err != nil {
				continue
			}
			switch decoded.Envelope.Type {
			case protocol.ServerOperationAccepted:
				id := string(decoded.Payload.(*protocol.OperationAcceptedPayload).Operation.OperationID)
				mutex.Lock()
				if start, ok := sentAt[id]; ok {
					acks = append(acks, float64(now.Sub(start))/float64(time.Millisecond))
					delete(sentAt, id)
				}
				mutex.Unlock()
			case protocol.ServerPong:
				id := decoded.Payload.(*protocol.PongPayload).Nonce
				mutex.Lock()
				if start, ok := sentAt[id]; ok {
					pings = append(pings, float64(now.Sub(start))/float64(time.Millisecond))
					delete(sentAt, id)
				}
				mutex.Unlock()
			}
		}
	}()
	send := func(envelope protocol.ClientEnvelope, payload any) {
		payloadJSON, _ := json.Marshal(payload)
		envelope.Payload = payloadJSON
		data, _ := json.Marshal(envelope)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = connection.Write(ctx, websocket.MessageText, data)
	}

	start := time.Now()
	presenceTicker := time.NewTicker(100 * time.Millisecond)
	defer presenceTicker.Stop()
	operationTicker := time.NewTicker(400 * time.Millisecond)
	defer operationTicker.Stop()
	pingTicker := time.NewTicker(250 * time.Millisecond)
	defer pingTicker.Stop()
	sequence, operations, pingCount := uint64(0), 0, 0
	for time.Since(start) < duration {
		select {
		case <-presenceTicker.C:
			sequence++
			send(protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: fmt.Sprintf("p-%d", sequence), SessionID: created.SessionID, Type: protocol.ClientPresenceUpdate}, protocol.PresenceUpdatePayload{Sequence: sequence, Status: "active", Cursor: &model.Coord{X: int(sequence%100) + 1, Y: 1, Z: 1}})
		case <-operationTicker.C:
			operations++
			operation := testOperation(t, snapshot, operations)
			mutex.Lock()
			sentAt[string(operation.OperationID)] = time.Now()
			mutex.Unlock()
			send(protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: fmt.Sprintf("o-%d", operations), SessionID: created.SessionID, Type: protocol.ClientOperationSubmit}, protocol.OperationSubmitPayload{Operation: operation})
		case <-pingTicker.C:
			pingCount++
			nonce := fmt.Sprintf("ping-%d", pingCount)
			mutex.Lock()
			sentAt[nonce] = time.Now()
			mutex.Unlock()
			send(protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: nonce, SessionID: created.SessionID, Type: protocol.ClientPing}, protocol.PingPayload{Nonce: nonce})
		case <-readerDone:
			start = time.Now().Add(-duration)
		}
	}
	// Let the backlog drain so acknowledgement latency includes any stall.
	time.Sleep(time.Second)
	mutex.Lock()
	defer mutex.Unlock()
	result := loopMeasurement{
		Name: name, Hosted: hosted, AuthDelayMS: authDelay.Milliseconds(), AppendMS: appendDelay.Milliseconds(), StoreReadMS: readDelay.Milliseconds(),
		Operations: operations, AckedOps: len(acks),
		AckP50MS: percentile(acks, 0.5), AckP95MS: percentile(acks, 0.95), AckMaxMS: percentile(acks, 1),
		PingP50MS: percentile(pings, 0.5), PingP95MS: percentile(pings, 0.95), PingMaxMS: percentile(pings, 1),
		PresenceSent: int(sequence), Closed: closed,
	}
	if authorizer != nil {
		result.AuthCalls = authorizer.calls.Load()
	}
	return result
}

func TestReadLoopLatencyMeasurement(t *testing.T) {
	output := os.Getenv("APHELIONDMM_READ_LOOP_MEASURE")
	if output == "" {
		t.Skip("set APHELIONDMM_READ_LOOP_MEASURE=<file> to record read-loop latency")
	}
	duration := 8 * time.Second
	scenarios := []struct {
		name                       string
		hosted                     bool
		auth, appendDelay, readLag time.Duration
	}{
		{"local sqlite", false, 0, 0, 0},
		{"local sqlite + 10ms append, 5ms read", false, 0, 10 * time.Millisecond, 5 * time.Millisecond},
		{"local sqlite + 40ms append, 20ms read", false, 0, 40 * time.Millisecond, 20 * time.Millisecond},
		{"hosted auth 0ms", true, 0, 0, 0},
		{"hosted auth 20ms", true, 20 * time.Millisecond, 0, 0},
		{"hosted auth 50ms", true, 50 * time.Millisecond, 0, 0},
		{"hosted auth 80ms", true, 80 * time.Millisecond, 0, 0},
		{"hosted auth 120ms", true, 120 * time.Millisecond, 0, 0},
		{"hosted auth 50ms + 10ms append, 5ms read", true, 50 * time.Millisecond, 10 * time.Millisecond, 5 * time.Millisecond},
	}
	var results []loopMeasurement
	for _, scenario := range scenarios {
		result := runReadLoopMeasurement(t, scenario.name, scenario.hosted, scenario.auth, scenario.appendDelay, scenario.readLag, duration)
		t.Logf("%+v", result)
		results = append(results, result)
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
