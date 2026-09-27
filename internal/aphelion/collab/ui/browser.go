package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"

	"sdmm/internal/aphelion/collab/protocol"
	w "sdmm/internal/imguiext/widget"
)

// Browser state belongs to the UI thread; workers publish only through Schedule.
type Browser struct {
	Client     *SessionClient
	Schedule   func(func())
	CanJoin    func() string
	Join       func(context.Context, string, func(error))
	SignIn     func()
	Closed     func()
	account    HostedAccount
	scope      string
	page       protocol.HostedSessionsPage
	selected   string
	cursor     string
	err        string
	busy       bool
	closed     bool
	generation uint64
	cancel     context.CancelFunc
	title      string
	community  bool
}

func (b *Browser) Name() string         { return "Browse Hosted Sessions" }
func (b *Browser) HasCloseButton() bool { return true }
func (b *Browser) OnClose() {
	b.closed = true
	b.generation++
	if b.cancel != nil {
		b.cancel()
	}
	if b.Closed != nil {
		b.Closed()
	}
}
func (b *Browser) Refresh() { b.refresh("") }

func (b *Browser) refresh(cursor string) {
	if b.closed {
		return
	}
	if b.cancel != nil {
		b.cancel()
	}
	b.generation++
	generation := b.generation
	b.account = b.Client.HostedAccount()
	b.cursor = cursor
	b.busy = true
	b.err = ""
	if b.scope == "" {
		b.scope = "community"
	}
	scope := b.scope
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	b.cancel = cancel
	go func() {
		defer cancel()
		_, err := b.Client.HostedCapabilities(ctx)
		var page protocol.HostedSessionsPage
		if err == nil {
			page, err = b.Client.ListHostedSessions(ctx, scope, cursor)
		}
		b.Schedule(func() {
			if b.closed || b.generation != generation || !b.Client.HostedAccountCurrent(b.account) {
				return
			}
			b.busy = false
			if err != nil {
				b.err = err.Error()
				return
			}
			b.page = page
			found := false
			for _, row := range page.Sessions {
				if row.SessionID == b.selected {
					found = true
					b.title = row.Title
					b.community = row.Visibility == "community"
				}
			}
			if !found {
				b.selected = ""
			}
		})
	}()
}

func (b *Browser) Process() {
	if imgui.IsKeyPressed(int(glfw.KeyEscape)) {
		imgui.CloseCurrentPopup()
		return
	}
	account := b.Client.HostedAccount()
	if b.scope == "" {
		b.Refresh()
	}
	if account.Generation != b.account.Generation || account.Origin != b.account.Origin {
		b.page = protocol.HostedSessionsPage{}
		b.selected = ""
		b.Refresh()
	}
	imgui.Text("Service: " + account.Origin)
	if !account.SignedIn {
		imgui.TextWrapped("Sign in to browse sessions. If the service restarted or your login expired, sign in again.")
		if imgui.Button("Sign in") && b.SignIn != nil {
			b.SignIn()
		}
		return
	}
	imgui.Text("Signed in as " + account.DisplayName)
	if imgui.Button("Community") {
		b.scope = "community"
		b.Refresh()
	}
	imgui.SameLine()
	if imgui.Button("My sessions") {
		b.scope = "mine"
		b.Refresh()
	}
	imgui.SameLine()
	w.Disabled(b.busy, w.Button("Refresh", b.Refresh)).Build()
	if b.busy {
		imgui.Text("Loading...")
	} else if b.err != "" {
		imgui.TextWrapped("Unable to refresh: " + b.err)
	} else if len(b.page.Sessions) == 0 {
		imgui.Text("No sessions in this view.")
	}
	imgui.BeginChildV("sessions", imgui.Vec2{X: 650, Y: 250}, true, imgui.WindowFlagsNone)
	for _, row := range b.page.Sessions {
		title := row.Title
		if title == "" {
			title = "Session " + row.SessionID[:min(8, len(row.SessionID))]
		}
		label := fmt.Sprintf("%s | %s | %d connected", strings.ReplaceAll(title, "##", "# #"), strings.ReplaceAll(row.OwnerDisplayName, "##", "# #"), row.Participants)
		if !row.Available {
			label += " | unavailable"
		}
		if imgui.SelectableV(label+"##"+row.SessionID, row.SessionID == b.selected, imgui.SelectableFlagsNone, imgui.Vec2{}) {
			b.selected = row.SessionID
			b.title = row.Title
			b.community = row.Visibility == "community"
		}
	}
	imgui.EndChild()
	if b.cursor != "" {
		w.Button("First page", b.Refresh).Build()
		imgui.SameLine()
	}
	if b.page.NextCursor != "" {
		w.Disabled(b.busy, w.Button("Next page", func() { b.refresh(b.page.NextCursor) })).Build()
	}
	reason := "Select a session."
	var selected *protocol.HostedSessionSummary
	for i := range b.page.Sessions {
		if b.page.Sessions[i].SessionID == b.selected {
			selected = &b.page.Sessions[i]
		}
	}
	if selected != nil {
		imgui.TextWrapped("Map: " + selected.MapLabel + "   Environment: " + selected.EnvironmentLabel)
		imgui.TextWrapped("Compatibility is checked when opening. Load a compatible local environment and map first.")
		reason = b.CanJoin()
		if !selected.Available {
			reason = "The session document is unavailable."
		}
	}
	if b.err != "" {
		reason = "Refresh successfully before joining."
	}
	w.Disabled(b.busy || reason != "", w.Button("Open session", func() {
		if b.cancel != nil {
			b.cancel()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		b.cancel = cancel
		b.busy = true
		b.generation++
		generation := b.generation
		b.Join(ctx, b.selected, func(err error) {
			cancel()
			if b.closed || b.generation != generation {
				return
			}
			b.busy = false
			if err != nil {
				b.err = err.Error()
			} else {
				b.Refresh()
			}
		})
	})).Build()
	if reason != "" {
		imgui.TextWrapped(reason)
	}
	status := b.Client.Status()
	if selected != nil && account.Attached && status.SessionID == selected.SessionID && status.Role == "owner" {
		imgui.Separator()
		imgui.Text("Session settings")
		w.InputTextWithHint("##session-title", "Session title", &b.title).Width(-1).Build()
		imgui.Checkbox("Community", &b.community)
		imgui.TextWrapped("Listed to signed-in users; new members join as editors. Switching to Private keeps previously admitted members. Downloaded map content cannot be recalled.")
		w.Disabled(b.busy, w.Button("Save settings", func() {
			metadata := selected.HostedSessionMetadata
			metadata.Title = b.title
			metadata.Visibility = "private"
			if b.community {
				metadata.Visibility = "community"
			}
			id := selected.SessionID
			b.busy = true
			b.generation++
			generation := b.generation
			account := b.account
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			b.cancel = cancel
			go func() {
				defer cancel()
				err := b.Client.UpdateHostedSession(ctx, account, id, metadata)
				b.Schedule(func() {
					if b.closed || b.generation != generation || !b.Client.HostedAccountCurrent(account) {
						return
					}
					b.busy = false
					if err != nil {
						b.err = err.Error()
					} else {
						b.Refresh()
					}
				})
			}()
		})).Build()
	}
}
