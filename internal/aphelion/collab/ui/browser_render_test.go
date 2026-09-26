package ui

import (
	"context"
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
	browser := &Browser{Client: client, Schedule: func(f func()) { f() }, CanJoin: func() string { return "Load a local map." }, scope: "community", cancel: cancel}
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
	browser.OnClose()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("closing browser left work running")
	}
}
