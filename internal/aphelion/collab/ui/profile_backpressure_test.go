package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/protocol"
)

// Hold the same Send boundary at which a full durable transport queue waits.
// The release is independent of Close so late success can exercise session fencing.
type blockedProfileTransport struct {
	capturingSessionTransport
	entered chan protocol.ClientEnvelope
	release chan struct{}
}

func (transport *blockedProfileTransport) Send(ctx context.Context, envelope protocol.ClientEnvelope) error {
	transport.entered <- envelope
	select {
	case <-transport.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestProfileBackpressureDoesNotLockSession(t *testing.T) {
	for _, action := range []string{"status", "leave"} {
		t.Run(action, func(t *testing.T) {
			transport := &blockedProfileTransport{entered: make(chan protocol.ClientEnvelope, 1), release: make(chan struct{})}
			defer close(transport.release)
			client := NewSessionClient(SessionClientConfig{})
			client.transport, client.sessionID = transport, "old-session"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sent := make(chan error, 1)
			go func() { sent <- client.UpdateDisplayName(ctx, "New name") }()
			select {
			case <-transport.entered:
			case <-time.After(time.Second):
				t.Fatal("profile did not reach Send")
			}
			finished := make(chan error, 1)
			go func() {
				if action == "leave" {
					finished <- client.Leave(context.Background())
				} else {
					_ = client.Status()
					finished <- nil
				}
			}()
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("blocked profile send held the session lock")
			}
			cancel()
			if err := <-sent; !errors.Is(err, context.Canceled) {
				t.Fatalf("profile cancellation = %v", err)
			}
		})
	}
}

func TestProfileCompletionIsFencedAcrossSessionChange(t *testing.T) {
	transport := &blockedProfileTransport{entered: make(chan protocol.ClientEnvelope, 2), release: make(chan struct{})}
	client := NewSessionClient(SessionClientConfig{})
	client.transport, client.sessionID = transport, "old-session"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 2)
	for range 2 {
		go func() { completed <- client.UpdateDisplayName(ctx, "Old name") }()
	}
	var messages [2]protocol.ClientEnvelope
	for i := range messages {
		select {
		case messages[i] = <-transport.entered:
		case <-time.After(time.Second):
			t.Fatal("concurrent profile sends could not reach the transport")
		}
	}
	if messages[0].MessageID == messages[1].MessageID {
		t.Fatal("concurrent profile sends reused a message ID")
	}
	for _, message := range messages {
		if message.SessionID != "old-session" {
			t.Fatal("profile send lost its original session")
		}
	}
	// Reconnection replaces the transport while retaining the session/machine.
	// A session-ID-only fence would incorrectly report old-transport success.
	client.mutex.Lock()
	client.transport = &capturingSessionTransport{}
	client.mutex.Unlock()
	close(transport.release)
	for range 2 {
		if err := <-completed; !errors.Is(err, ErrSessionChanged) {
			t.Fatalf("stale profile completion = %v, want session changed", err)
		}
	}
	if err := client.UpdateDisplayName(ctx, "Current name"); err != nil {
		t.Fatal(err)
	}
	client.mutex.Lock()
	client.transport = nil
	client.mutex.Unlock()
	if err := client.UpdateDisplayName(ctx, "Disconnected"); !errors.Is(err, collabclient.ErrTransportNotConnected) {
		t.Fatalf("disconnected profile = %v", err)
	}
}
