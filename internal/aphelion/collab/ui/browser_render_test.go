package ui

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/collab/protocol"
)

func TestHostedBrowserRendersLifecycleStatesAndCancelsOnClose(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	native := imgui.CreateContext(nil)
	defer native.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 700})
	io.SetDeltaTime(1.0 / 60)
	io.Fonts().TextureDataRGBA32()
	client := NewSessionClient(SessionClientConfig{})
	client.hostedBaseURL = "https://maps.example"
	client.hostedDisplayName = "Mapper"
	ctx, cancel := context.WithCancel(context.Background())
	joinChecks := 0
	joinReason := "Load a local map."
	browser := &Browser{Client: client, Schedule: func(f func()) { f() }, CanJoin: func() string { joinChecks++; return joinReason }, scope: "community", cancel: cancel}
	states := []string{"signed out", "loading", "empty", "error", "selected"}
	for _, state := range states {
		if state != "signed out" {
			client.hostedCredential = "secret"
			client.hostedCredentialExpires = time.Now().Add(time.Hour)
		}
		browser.account = client.HostedAccount()
		browser.busy = state == "loading"
		browser.err = ""
		if state == "error" {
			browser.err = "Service unavailable"
		}
		if state == "selected" {
			browser.page = protocol.HostedSessionsPage{Sessions: []protocol.HostedSessionSummary{{SessionID: "session", HostedSessionMetadata: protocol.HostedSessionMetadata{Title: "Map", Visibility: "community"}, Available: true, Participants: 2}}}
			browser.selected = "session"
		}
		imgui.NewFrame()
		imgui.Begin("Browser fixture")
		browser.Process()
		imgui.End()
		imgui.Render()
	}
	if joinChecks != 0 {
		t.Fatalf("rendering ran %d potentially map-sized join checks", joinChecks)
	}
	joinCalls := 0
	var joining context.Context
	var complete func(error)
	browser.Join = func(ctx context.Context, id string, done func(error)) {
		if id != "session" {
			t.Fatal("join used a different selection")
		}
		joinCalls++
		joining, complete = ctx, done
	}
	browser.joinSelected()
	if joinChecks != 1 || joinCalls != 0 || browser.busy || browser.joinErr != joinReason || browser.err != "" {
		t.Fatal("failed preflight started work or invalidated session list")
	}
	joinReason = ""
	browser.joinSelected()
	if joinChecks != 2 || joinCalls != 1 || !browser.busy || browser.joinErr != "" {
		t.Fatal("corrected workspace could not retry without refreshing")
	}
	complete(errors.New("connection refused"))
	if browser.busy || browser.err != "" || browser.joinErr == "" {
		t.Fatal("join failure was confused with a list refresh failure")
	}
	browser.joinSelected()
	if joinCalls != 2 || joining.Err() != nil {
		t.Fatal("retry did not start a fresh join")
	}
	browser.OnClose()
	if joining.Err() == nil {
		t.Fatal("closing browser left the latest join running")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("closing browser left work running")
	}
}
