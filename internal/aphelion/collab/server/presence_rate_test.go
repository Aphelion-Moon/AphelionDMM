package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

// steppedClock lets tests place processing-time presence bursts inside one
// limiter window, as happens when a stalled read loop drains its backlog.
type steppedClock struct {
	mutex sync.Mutex
	now   time.Time
}

func newSteppedClock() *steppedClock { return &steppedClock{now: time.Now()} }

func (clock *steppedClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

func (clock *steppedClock) Advance(duration time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(duration)
}

func sendPresenceBurst(t *testing.T, connection *websocket.Conn, sessionID, prefix string, first, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		sequence := uint64(first + index)
		writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: fmt.Sprintf("%s-%d", prefix, sequence), SessionID: sessionID, Type: protocol.ClientPresenceUpdate}, protocol.PresenceUpdatePayload{Sequence: sequence, Status: "active"})
	}
}

// requireAlive pings the connection; the pong proves every earlier message was
// processed and the socket was not closed with 4429.
func requireAlive(t *testing.T, connection *websocket.Conn, sessionID, nonce string) {
	t.Helper()
	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: nonce, SessionID: sessionID, Type: protocol.ClientPing}, protocol.PingPayload{Nonce: nonce})
	_ = readServerEnvelopeType(t, connection, protocol.ServerPong)
}

// A drained backlog is processed at wire speed regardless of how slowly the
// client produced it, so the excess is dropped instead of closing the socket.
func TestPresenceBacklogBurstIsDroppedWithoutClosing(t *testing.T) {
	t.Parallel()

	clock := newSteppedClock()
	service, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"},
		Now:            clock.Now,
		Limits:         Limits{PresenceRate: RateLimit{Burst: 3, Window: time.Second}},
	})
	connection := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()

	sendPresenceBurst(t, connection, created.SessionID, "backlog", 1, 40)
	requireAlive(t, connection, created.SessionID, "after-backlog")

	// The next window starts clean and presence is applied again.
	clock.Advance(2 * time.Second)
	sendPresenceBurst(t, connection, created.SessionID, "later", 100, 1)
	requireAlive(t, connection, created.SessionID, "after-later")
	current, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != 0 {
		t.Fatalf("durable revision after presence backlog = %d, want 0", current.Revision)
	}
}

// A reconnect for the same actor must not inherit presence budget consumed by
// the connection it replaces (the server supersedes the older socket).
func TestPresenceBudgetIsPerConnection(t *testing.T) {
	t.Parallel()

	clock := newSteppedClock()
	_, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"},
		Now:            clock.Now,
		Limits:         Limits{PresenceRate: RateLimit{Burst: 3, Window: time.Second}, PresenceAbuseWindows: 1},
	})
	first, resumption := connectTestClientWithCredential(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = first.CloseNow() }()
	sendPresenceBurst(t, first, created.SessionID, "first", 1, 3)
	requireAlive(t, first, created.SessionID, "first-alive")

	second, _ := connectTestClientWithCredential(t, testServer.URL, created.SessionID, resumption, 0)
	defer func() { _ = second.CloseNow() }()
	sendPresenceBurst(t, second, created.SessionID, "second", 100, 3)
	requireAlive(t, second, created.SessionID, "second-alive")
}
func TestPresenceRateLimitClosesOnlyAfterSustainedAbuse(t *testing.T) {
	t.Parallel()

	clock := newSteppedClock()
	service, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"},
		Now:            clock.Now,
		Limits:         Limits{PresenceRate: RateLimit{Burst: 1, Window: time.Second}, PresenceAbuseWindows: 3},
	})
	connection := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()

	// Two consecutive over-limit windows are tolerated, and a compliant window
	// resets the count.
	sequence := 1
	for window := 0; window < 2; window++ {
		sendPresenceBurst(t, connection, created.SessionID, "over", sequence, 3)
		sequence += 3
		requireAlive(t, connection, created.SessionID, fmt.Sprintf("alive-%d", window))
		clock.Advance(time.Second)
	}
	sendPresenceBurst(t, connection, created.SessionID, "calm", sequence, 1)
	sequence++
	requireAlive(t, connection, created.SessionID, "alive-calm")
	clock.Advance(time.Second)
	for window := 0; window < 2; window++ {
		sendPresenceBurst(t, connection, created.SessionID, "again", sequence, 3)
		sequence += 3
		requireAlive(t, connection, created.SessionID, fmt.Sprintf("alive-again-%d", window))
		clock.Advance(time.Second)
	}

	// The third consecutive over-limit window is sustained abuse.
	sendPresenceBurst(t, connection, created.SessionID, "abuse", sequence, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, _, err := connection.Read(ctx)
		if err == nil {
			continue
		}
		if websocket.CloseStatus(err) != CloseRateLimited {
			t.Fatalf("sustained presence close status = %d from %v, want %d", websocket.CloseStatus(err), err, CloseRateLimited)
		}
		break
	}
	current, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != 0 {
		t.Fatalf("durable revision after presence flood = %d, want 0", current.Revision)
	}
}

func TestPresenceGateWindows(t *testing.T) {
	t.Parallel()

	base := time.Unix(1000, 0)
	gate := newPresenceGate(RateLimit{Burst: 2, Window: time.Second}, 2)
	for index, want := range []struct{ allowed, abuse bool }{{true, false}, {true, false}, {false, false}} {
		allowed, abuse := gate.Allow(base)
		if allowed != want.allowed || abuse != want.abuse {
			t.Fatalf("first window message %d = (%v,%v), want (%v,%v)", index, allowed, abuse, want.allowed, want.abuse)
		}
	}
	// Second consecutive over-limit window reaches the abuse threshold.
	gate.Allow(base.Add(time.Second))
	gate.Allow(base.Add(time.Second))
	if allowed, abuse := gate.Allow(base.Add(time.Second)); allowed || !abuse {
		t.Fatalf("second over-limit window = (%v,%v), want (false,true)", allowed, abuse)
	}

	// An idle gap longer than one window forgives earlier abuse.
	gate = newPresenceGate(RateLimit{Burst: 1, Window: time.Second}, 2)
	gate.Allow(base)
	gate.Allow(base)
	if allowed, abuse := gate.Allow(base.Add(5 * time.Second)); !allowed || abuse {
		t.Fatalf("after idle gap = (%v,%v), want (true,false)", allowed, abuse)
	}
	if allowed, abuse := gate.Allow(base.Add(5 * time.Second)); allowed || abuse {
		t.Fatalf("first over-limit after gap = (%v,%v), want (false,false)", allowed, abuse)
	}
}
