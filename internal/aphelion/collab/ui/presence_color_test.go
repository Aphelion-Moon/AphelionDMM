package ui

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
)

func colorPointer(value int) *int { return &value }

func TestBuildPresenceOverlaysUsesCursorColorWhenValid(t *testing.T) {
	t.Parallel()
	now := time.Unix(100, 0)
	actor := model.ActorID("01890f3e-7b5c-7abc-8def-0123456789bc")
	build := func(color *int) PresenceOverlay {
		overlays := BuildPresenceOverlays([]ObservedPresence{{Presence: protocol.ParticipantPresence{ActorID: actor, DisplayName: "Ada Lovelace", Cursor: coord(1, 1, 1), CursorColor: color}, ObservedAt: now}}, 1, 32, 4, time.Minute, now)
		if len(overlays) != 1 {
			t.Fatalf("overlays = %d", len(overlays))
		}
		return overlays[0]
	}
	derived := build(nil).StyleSlot
	if derived != presenceStyleSlot(actor) || derived >= protocol.CursorColorPaletteSize {
		t.Fatalf("derived slot = %d", derived)
	}
	for index := 0; index < protocol.CursorColorPaletteSize; index++ {
		if got := build(colorPointer(index)).StyleSlot; got != uint32(index) {
			t.Fatalf("slot for cursor_color %d = %d", index, got)
		}
	}
	// Out-of-range values (for example from a newer server) fall back.
	for _, invalid := range []int{-1, protocol.CursorColorPaletteSize, 99} {
		if got := build(colorPointer(invalid)).StyleSlot; got != derived {
			t.Fatalf("slot for invalid %d = %d, want fallback %d", invalid, got, derived)
		}
	}
}

func TestPresenceInitials(t *testing.T) {
	t.Parallel()
	for label, want := range map[string]string{
		"Ada Lovelace":      "AL",
		"mapper":            "M",
		"  two   spaces  x": "TS",
		"élan vital":        "ÉV",
		"":                  "?",
		"   ":               "?",
		"日本語 名前":            "日名",
	} {
		if got := PresenceInitials(label); got != want {
			t.Errorf("PresenceInitials(%q) = %q, want %q", label, got, want)
		}
	}
	now := time.Unix(100, 0)
	overlays := BuildPresenceOverlays([]ObservedPresence{{Presence: protocol.ParticipantPresence{ActorID: "01890f3e-7b5c-7abc-8def-0123456789bc", DisplayName: "Ada Lovelace", Cursor: coord(1, 1, 1)}, ObservedAt: now}}, 1, 32, 4, time.Minute, now)
	if overlays[0].Initials != "AL" {
		t.Fatalf("overlay initials = %q", overlays[0].Initials)
	}
}

func TestSessionClientRecordsCursorColorFromPresence(t *testing.T) {
	t.Parallel()
	original := protocol.ParticipantPresence{ActorID: "a", CursorColor: colorPointer(6)}
	cloned := cloneParticipantPresence(original)
	*original.CursorColor = 1
	if cloned.CursorColor == nil || *cloned.CursorColor != 6 {
		t.Fatalf("clone shares or loses cursor colour: %v", cloned.CursorColor)
	}
}

func TestUpdateCursorColorSendsColourOnlyProfileUpdate(t *testing.T) {
	t.Parallel()
	transport := &capturingSessionTransport{}
	client := NewSessionClient(SessionClientConfig{})
	client.transport, client.sessionID = transport, "session-1"
	if err := client.UpdateCursorColor(context.Background(), protocol.CursorColorPaletteSize); err == nil {
		t.Fatal("out-of-range colour was accepted")
	}
	if len(transport.sent) != 0 {
		t.Fatal("invalid colour reached the transport")
	}
	if err := client.UpdateCursorColor(context.Background(), 9); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Type != protocol.ClientProfileUpdate {
		t.Fatalf("sent = %#v", transport.sent)
	}
	if got := string(transport.sent[0].Payload); got != `{"cursor_color":9}` {
		t.Fatalf("payload = %s", got)
	}
}

func TestCursorColorIsRememberedAndResentAfterJoin(t *testing.T) {
	t.Parallel()
	client := NewSessionClient(SessionClientConfig{})
	// Not connected: the choice is retained rather than reported as an error.
	if err := client.UpdateCursorColor(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
	if color := client.CursorColor(); color == nil || *color != 4 {
		t.Fatalf("remembered colour = %v", color)
	}
	transport := &capturingSessionTransport{}
	client.transport, client.sessionID = transport, "session-1"
	if err := client.resendCursorColor(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || string(transport.sent[0].Payload) != `{"cursor_color":4}` {
		t.Fatalf("sent = %#v", transport.sent)
	}
	fresh := NewSessionClient(SessionClientConfig{})
	fresh.transport, fresh.sessionID = transport, "session-1"
	before := len(transport.sent)
	if err := fresh.resendCursorColor(context.Background()); err != nil || len(transport.sent) != before {
		t.Fatalf("unset colour must send nothing: err=%v sent=%d", err, len(transport.sent)-before)
	}
}

// colorRecordingTransport counts cursor_color profile updates sent on one
// real WebSocket transport.
type colorRecordingTransport struct {
	SessionTransport
	colorUpdates atomic.Int32
}

func (transport *colorRecordingTransport) Send(ctx context.Context, envelope protocol.ClientEnvelope) error {
	if envelope.Type == protocol.ClientProfileUpdate && strings.Contains(string(envelope.Payload), "cursor_color") {
		transport.colorUpdates.Add(1)
	}
	return transport.SessionTransport.Send(ctx, envelope)
}

func TestCursorColorIsResentOnReconnectAndAutomaticNeverSendsIt(t *testing.T) {
	t.Parallel()
	snapshot := controllerSnapshot(t)
	embedded, err := server.StartEmbedded(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = embedded.Shutdown(context.Background()) })
	var mutex sync.Mutex
	var transports []*colorRecordingTransport
	client := NewSessionClient(SessionClientConfig{NewTransport: func() SessionTransport {
		transport := &colorRecordingTransport{SessionTransport: collabclient.NewWebSocketTransport(SessionClientConfig{}.Transport)}
		mutex.Lock()
		transports = append(transports, transport)
		mutex.Unlock()
		return transport
	}})
	t.Cleanup(func() { _ = client.Leave(context.Background()) })
	invitation, err := client.Create(context.Background(), embedded.Endpoint(), embedded.TakeLaunchToken(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Automatic colour: nothing is ever sent, on join or after reconnect.
	if err := client.Join(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	waitForTransports := func(count int) []*colorRecordingTransport {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			mutex.Lock()
			if len(transports) >= count && client.Status().State == collabclient.StateCaughtUp {
				copied := append([]*colorRecordingTransport(nil), transports...)
				mutex.Unlock()
				return copied
			}
			mutex.Unlock()
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("transport %d never connected: %#v", count, client.Status())
		return nil
	}
	forceReconnect := func() {
		client.mutex.Lock()
		current := client.transport
		client.mutex.Unlock()
		if err := current.Close(websocket.StatusInternalError, "forced reconnect"); err != nil {
			t.Fatal(err)
		}
	}
	forceReconnect()
	all := waitForTransports(2)
	time.Sleep(100 * time.Millisecond)
	for index, transport := range all {
		if got := transport.colorUpdates.Load(); got != 0 {
			t.Fatalf("automatic colour sent cursor_color %d times on transport %d", got, index)
		}
	}
	// A chosen colour is sent on the next successful reconnect.
	if err := client.UpdateCursorColor(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	forceReconnect()
	all = waitForTransports(3)
	deadline := time.Now().Add(3 * time.Second)
	for all[len(all)-1].colorUpdates.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := all[len(all)-1].colorUpdates.Load(); got != 1 {
		t.Fatalf("reconnected transport sent cursor_color %d times, want 1", got)
	}
}

func TestRecordPresenceUpdateKeepsCursorColor(t *testing.T) {
	t.Parallel()
	client := NewSessionClient(SessionClientConfig{})
	actor := model.ActorID("01890f3e-7b5c-7abc-8def-0123456789ac")
	client.recordPresenceUpdate(client.machine, &protocol.ServerPresenceUpdatePayload{ActorID: actor, DisplayName: "A", Sequence: 1, Status: "active", CursorColor: colorPointer(8)})
	client.recordPresenceSnapshot(client.machine, &protocol.PresenceSnapshotPayload{Participants: []protocol.ParticipantPresence{{ActorID: actor, DisplayName: "A", Sequence: 2, Status: "active", CursorColor: colorPointer(2)}}})
	got := client.ObservedPresence()
	if len(got) != 1 || got[0].Presence.CursorColor == nil || *got[0].Presence.CursorColor != 2 {
		t.Fatalf("observed = %#v", got)
	}
	client.recordPresenceUpdate(client.machine, &protocol.ServerPresenceUpdatePayload{ActorID: actor, DisplayName: "A", Sequence: 3, Status: "active", CursorColor: colorPointer(8)})
	got = client.ObservedPresence()
	if got[0].Presence.CursorColor == nil || *got[0].Presence.CursorColor != 8 {
		t.Fatalf("update lost colour: %#v", got)
	}
}