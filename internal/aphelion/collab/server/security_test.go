package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/protocol"
)

func TestTrustedProxyHandlerUsesForwardedAddressOnlyFromTrustedPeer(t *testing.T) {
	var observed []string
	handler, err := NewTrustedProxyHandler(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		observed = append(observed, remoteIP(request))
	}), []string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	trusted := httptest.NewRequest(http.MethodGet, "http://example.test", nil)
	trusted.RemoteAddr = "10.1.2.3:1234"
	trusted.Header.Set("X-Forwarded-For", "203.0.113.8, 10.2.3.4")
	handler.ServeHTTP(httptest.NewRecorder(), trusted)
	untrusted := httptest.NewRequest(http.MethodGet, "http://example.test", nil)
	untrusted.RemoteAddr = "192.0.2.4:1234"
	untrusted.Header.Set("X-Forwarded-For", "203.0.113.9")
	handler.ServeHTTP(httptest.NewRecorder(), untrusted)
	if !reflect.DeepEqual(observed, []string{"203.0.113.8", "192.0.2.4"}) {
		t.Fatalf("observed remote addresses = %v", observed)
	}
}

func TestServiceCopiesImmutableSecurityLimits(t *testing.T) {
	t.Parallel()

	configured := DefaultLimits()
	configured.MaxConnections = 7
	configured.DurableQueueDepth = 3
	configured.PresenceQueueDepth = 2
	service := NewService(ServiceConfig{Limits: configured})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	configured.MaxConnections = 99
	if service.limits.MaxConnections != 7 {
		t.Fatalf("service connection limit = %d, want copied value 7", service.limits.MaxConnections)
	}
	if service.limits.DurableQueueDepth != 3 || service.limits.PresenceQueueDepth != 2 {
		t.Fatalf("service queue limits = durable %d presence %d, want 3 and 2", service.limits.DurableQueueDepth, service.limits.PresenceQueueDepth)
	}
}

func TestWebSocketJoinRateLimitIsPerIP(t *testing.T) {
	t.Parallel()

	_, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"},
		Limits: Limits{
			JoinRate: RateLimit{Burst: 1, Window: time.Minute},
		},
	})
	editorToken := createTestJoinToken(t, testServer.URL, created.SessionID, created.OwnerToken, RoleEditor, "Editor")
	first := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	if err := first.Close(websocket.StatusNormalClosure, "join rate test"); err != nil {
		t.Fatal(err)
	}

	websocketURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/v1/collaboration"
	second, response, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + editorToken}, "Origin": []string{"http://127.0.0.1"}},
		Subprotocols: []string{WebSocketSubprotocol},
	})
	if second != nil {
		_ = second.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("rate-limited connection = response %#v error %v, want HTTP 429", response, err)
	}
}

func TestDurableRateLimitClosesActorWithoutMutatingSecondOperation(t *testing.T) {
	t.Parallel()

	service, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"},
		Limits: Limits{
			DurableRate: RateLimit{Burst: 1, Window: time.Minute},
		},
	})
	snapshot, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	connection := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()

	first := testOperation(t, snapshot, 1)
	submitTestOperation(t, connection, created.SessionID, "first", first)
	accepted := readServerEnvelopeType(t, connection, protocol.ServerOperationAccepted).Payload.(*protocol.OperationAcceptedPayload)
	second := testOperation(t, snapshot, 2)
	second.BaseRevision = accepted.Operation.Revision
	second.BaseMapHash = accepted.MapHash
	submitTestOperation(t, connection, created.SessionID, "second", second)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err = connection.Read(ctx)
	if websocket.CloseStatus(err) != CloseRateLimited {
		t.Fatalf("rate-limit close status = %d from %v, want %d", websocket.CloseStatus(err), err, CloseRateLimited)
	}
	current, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != accepted.Operation.Revision {
		t.Fatalf("revision after rate-limited operation = %d, want %d", current.Revision, accepted.Operation.Revision)
	}
}

func TestConfiguredOperationChangeLimitRejectsBeforeMutation(t *testing.T) {
	t.Parallel()

	service, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"},
		Limits:         Limits{MaxOperationChanges: 1},
	})
	snapshot, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	connection := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	operation := testOperation(t, snapshot, 1)
	second := testOperation(t, snapshot, 2)
	operation.Changes = append(operation.Changes, second.Changes[0])
	submitTestOperation(t, connection, created.SessionID, "too-many-changes", operation)
	rejected := readServerEnvelopeType(t, connection, protocol.ServerOperationRejected).Payload.(*protocol.OperationRejectedPayload)
	if rejected.Code != "limit_exceeded" {
		t.Fatalf("rejection code = %q, want limit_exceeded", rejected.Code)
	}
	current, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != 0 {
		t.Fatalf("revision after oversized operation = %d, want 0", current.Revision)
	}
}

func TestAuthenticatedPrincipalOverridesForgedOperationActor(t *testing.T) {
	t.Parallel()

	service, created, testServer := startHTTPTestSession(t)
	snapshot, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	connection := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	operation := testOperation(t, snapshot, 1)
	forgedActor := operation.ActorID
	submitTestOperation(t, connection, created.SessionID, "forged-actor", operation)
	accepted := readServerEnvelopeType(t, connection, protocol.ServerOperationAccepted).Payload.(*protocol.OperationAcceptedPayload)
	if accepted.Operation.ActorID == forgedActor {
		t.Fatalf("accepted forged actor %q instead of authenticated principal", forgedActor)
	}
}

func TestViewerOperationIsRejectedWithoutMutation(t *testing.T) {
	t.Parallel()

	service, created, testServer := startHTTPTestSession(t)
	viewerToken := createTestJoinToken(t, testServer.URL, created.SessionID, created.OwnerToken, RoleViewer, "Viewer")
	snapshot, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	connection := connectTestClient(t, testServer.URL, created.SessionID, viewerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	submitTestOperation(t, connection, created.SessionID, "viewer-operation", testOperation(t, snapshot, 1))
	_ = readServerEnvelopeType(t, connection, protocol.ServerOperationRejected)
	current, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != 0 {
		t.Fatalf("revision after viewer operation = %d, want 0", current.Revision)
	}
}

func TestInvalidCoordinatesCloseWithStablePolicyCode(t *testing.T) {
	t.Parallel()

	service, created, testServer := startHTTPTestSession(t)
	snapshot, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	connection := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	operation := testOperation(t, snapshot, 1)
	operation.Changes[0].Coord.X = 0
	submitTestOperation(t, connection, created.SessionID, "invalid-coordinates", operation)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err = connection.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("invalid-coordinate close status = %d from %v, want %d", websocket.CloseStatus(err), err, websocket.StatusPolicyViolation)
	}
}

func TestConfiguredWebSocketByteLimitClosesOversizedFrame(t *testing.T) {
	t.Parallel()

	_, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"},
		Limits:         Limits{MaxWebSocketMessageBytes: 512},
	})
	connection := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := connection.Write(ctx, websocket.MessageText, []byte(strings.Repeat("x", 513))); err != nil {
		t.Fatal(err)
	}
	_, _, err := connection.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
		t.Fatalf("oversized-frame close status = %d from %v, want %d", websocket.CloseStatus(err), err, websocket.StatusMessageTooBig)
	}
}

func TestConfiguredSnapshotBodyLimitRejectsBeforeDecode(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{Limits: Limits{MaxSnapshotBodyBytes: 64}})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	launchToken, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(service.Handler())
	t.Cleanup(testServer.Close)
	body, err := json.Marshal(map[string]any{"snapshot": testSnapshot(t, 1)})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, testServer.URL+"/v1/sessions", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+launchToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized HTTP request status = %d, want 413", response.StatusCode)
	}
}

func TestRateLimiterBoundsEntriesAndExpiresIdleKeys(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)
	limiter := newRateLimiter(RateLimit{Burst: 1, Window: time.Minute}, 2)
	if !limiter.Allow("first", now) || !limiter.Allow("second", now.Add(time.Second)) {
		t.Fatal("limiter rejected entries within its cardinality bound")
	}
	// A full table evicts its least recently seen key instead of refusing every
	// newcomer, so a flood of fresh addresses cannot lock other peers out.
	if !limiter.Allow("third", now.Add(2*time.Second)) {
		t.Fatal("limiter refused a new key instead of evicting its stalest entry")
	}
	if len(limiter.entries) != 2 {
		t.Fatalf("limiter entries = %d, want the cardinality bound 2", len(limiter.entries))
	}
	if _, exists := limiter.entries["first"]; exists {
		t.Fatal("limiter evicted a key other than the least recently seen")
	}
	if limiter.Allow("second", now.Add(3*time.Second)) {
		t.Fatal("eviction reset the budget of a surviving key")
	}
	if !limiter.Allow("fourth", now.Add(2*time.Minute)) {
		t.Fatal("limiter did not expire idle keys after the configured window")
	}
	if len(limiter.entries) != 1 {
		t.Fatalf("retained limiter entries = %d, want 1 after expiry", len(limiter.entries))
	}
}

func TestRateLimitKeyBucketsIPv6ByPrefix(t *testing.T) {
	t.Parallel()

	if rateLimitKey("2001:db8::1") != rateLimitKey("2001:db8::ffff:1") {
		t.Fatal("addresses in one IPv6 /64 did not share a limiter key")
	}
	if rateLimitKey("2001:db8::1") == rateLimitKey("2001:db8:0:1::1") {
		t.Fatal("addresses in different IPv6 /64s shared a limiter key")
	}
	if rateLimitKey("192.0.2.1") != "192.0.2.1" || rateLimitKey("192.0.2.1") == rateLimitKey("192.0.2.2") {
		t.Fatal("IPv4 addresses are not keyed individually")
	}
	if rateLimitKey("not-an-address") != "not-an-address" {
		t.Fatal("an unparsable address did not key as itself")
	}
}

func TestClosedDurableQueueUsesStableSlowConsumerCode(t *testing.T) {
	t.Parallel()

	service, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"},
		Limits:         Limits{DurableQueueDepth: 1},
	})
	connection := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	service.hub.mutex.RLock()
	owner := service.hub.sessions[created.SessionID].owner
	service.hub.mutex.RUnlock()
	owner.closeDurable()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := connection.Read(ctx)
	if websocket.CloseStatus(err) != CloseSlowConsumer {
		t.Fatalf("slow-consumer close status = %d from %v, want %d", websocket.CloseStatus(err), err, CloseSlowConsumer)
	}
}

func TestWebSocketConnectionLimitRejectsBeforeUpgrade(t *testing.T) {
	t.Parallel()

	service, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"},
		Limits:         Limits{MaxConnections: 1},
	})
	_ = service
	editorToken := createTestJoinToken(t, testServer.URL, created.SessionID, created.OwnerToken, RoleEditor, "Editor")
	first := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = first.CloseNow() }()

	websocketURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/v1/collaboration"
	second, response, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + editorToken}, "Origin": []string{"http://127.0.0.1"}},
		Subprotocols: []string{WebSocketSubprotocol},
	})
	if second != nil {
		_ = second.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second connection = response %#v error %v, want HTTP 429", response, err)
	}
}
