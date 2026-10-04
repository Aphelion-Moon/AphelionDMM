package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

var errReconnectFixtureOffline = errors.New("fixture offline")

type offlineReconnectTransport struct {
	capturingSessionTransport
	requested func(model.Revision)
}

func (transport *offlineReconnectTransport) Connect(_ context.Context, request protocol.JoinRequest, _ func(protocol.ServerEnvelope)) error {
	if transport.requested != nil {
		transport.requested(request.AcknowledgedRevision)
	}
	return errReconnectFixtureOffline
}

func reconnectCaptureFixture(t testing.TB, tiles int) *SessionClient {
	t.Helper()
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion,
		DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: strings.Repeat("a", 64), Revision: 7, MaxX: 256, MaxY: (tiles + 255) / 256, MaxZ: 1}
	for i := range tiles {
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: i%256 + 1, Y: i/256 + 1, Z: 1},
			State: model.TileState{Prefabs: []model.PrefabState{{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", i)), Path: "/obj/unknown", Vars: map[string]string{"dir": "2", "opaque": "list(1, /missing/type)"}}}}})
	}
	client := NewSessionClient(SessionClientConfig{Reconnect: collabclient.ReconnectPolicy{MaxAttempts: 8, Wait: func(context.Context, time.Duration) error { return nil }},
		NewTransport: func() SessionTransport { return &offlineReconnectTransport{} }})
	var err error
	client.network, err = collabclient.NewNetworkExecutor(&capturingSessionTransport{}, snapshot, "01890f3e-7b5c-7abc-8def-0123456789ac", "session")
	if err != nil {
		t.Fatal(err)
	}
	client.network.Suspend(errReconnectFixtureOffline)
	for _, event := range []collabclient.Event{collabclient.EventConnect, collabclient.EventConnected, collabclient.EventSynchronized, collabclient.EventConnectionLost} {
		if err := client.machine.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	client.resumptionToken, client.resumptionExpiresAt = "resume", time.Now().Add(time.Hour)
	return client
}

func TestReconnectRetriesDoNotCopyMap(t *testing.T) {
	client := reconnectCaptureFixture(t, 4096)
	allocations := testing.AllocsPerRun(3, func() {
		client.runReconnect(context.Background(), client.machine, client.network)
	})
	if !errors.Is(client.Status().Err, errReconnectFixtureOffline) {
		t.Fatal("retry did not preserve the transport failure")
	}
	if allocations > 1000 {
		t.Fatalf("eight reconnect attempts allocated map-sized data: %.0f allocations", allocations)
	}
}

func TestReconnectCapturesRevisionAgainAfterFallback(t *testing.T) {
	client := reconnectCaptureFixture(t, 256)
	client.revision = 999 // Panel metadata must never replace verified authority.
	attempts := 0
	client.newTransport = func() SessionTransport {
		return &offlineReconnectTransport{requested: func(revision model.Revision) {
			want := model.Revision(7)
			if attempts > 0 {
				want = 8
			}
			if revision != want {
				t.Fatalf("attempt %d used revision %d, want %d", attempts, revision, want)
			}
			attempts++
		}}
	}
	client.config.Reconnect.Wait = func(context.Context, time.Duration) error {
		if attempts == 1 {
			snapshot, err := client.network.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			snapshot.Revision++
			if err := client.network.ReplaceAcknowledgedSnapshot(context.Background(), snapshot); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}
	client.runReconnect(context.Background(), client.machine, client.network)
	if attempts != 8 {
		t.Fatalf("reconnect attempts = %d, want 8", attempts)
	}
}

func BenchmarkReconnectRetries(b *testing.B) {
	client := reconnectCaptureFixture(b, 65536)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.runReconnect(context.Background(), client.machine, client.network)
	}
}
