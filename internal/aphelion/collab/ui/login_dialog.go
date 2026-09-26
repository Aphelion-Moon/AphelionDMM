package ui

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"

	w "sdmm/internal/imguiext/widget"
)

type LoginDialog struct {
	Client       *SessionClient
	Schedule     func(func())
	OpenBrowser  func(string) error
	Closed       func()
	Origin       string
	err          string
	busy, closed bool
	completed    bool
	cancel       context.CancelFunc
}

func (*LoginDialog) Name() string         { return "Sign In to Hosted Collaboration" }
func (*LoginDialog) HasCloseButton() bool { return true }
func (d *LoginDialog) OnClose() {
	d.closed = true
	if d.cancel != nil {
		d.cancel()
	}
	if d.busy {
		d.Client.CancelHostedSignIn()
	}
	if d.Closed != nil {
		d.Closed()
	}
}
func (d *LoginDialog) Process() {
	if d.completed {
		imgui.CloseCurrentPopup()
		return
	}
	if imgui.IsKeyPressed(int(glfw.KeyEscape)) {
		imgui.CloseCurrentPopup()
		return
	}
	attached := d.Client.HostedAccount().Attached
	imgui.TextWrapped("Sign in in your browser. Your login is kept only in memory and may end when the service restarts.")
	w.Disabled(attached || d.busy, w.InputTextWithHint("##hosted-origin", "https://collaboration.example", &d.Origin).Width(520)).Build()
	if attached {
		imgui.TextWrapped("Sign in to the same service and account to retain this session and its drafts.")
	}
	if d.err != "" {
		imgui.TextWrapped(d.err)
	}
	if d.busy {
		imgui.Text("Waiting for browser sign-in...")
	}
	w.Disabled(d.busy, w.Button("Sign in", func() {
		d.busy = true
		d.err = ""
		origin := d.Origin
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		d.cancel = cancel
		go func() {
			defer cancel()
			signIn, err := d.Client.BeginHostedSignIn(ctx, origin)
			if err == nil && ctx.Err() != nil {
				err = ctx.Err()
			}
			if err == nil {
				err = d.OpenBrowser(signIn.AuthorizationURL)
			}
			var result hostedSignInResult
			for err == nil {
				result, err = d.Client.exchangeHostedSignIn(ctx, signIn)
				if !errors.Is(err, ErrHostedSignInPending) {
					break
				}
				select {
				case <-ctx.Done():
					err = ctx.Err()
				case <-time.After(time.Second):
					err = nil
				}
			}
			if err == nil {
				err = d.acceptExchangedHostedSignIn(ctx, signIn, result)
			} else if result.Token != "" {
				d.Client.discardHostedLogin(signIn.baseURL, result.Token)
			}
			if err != nil {
				d.Schedule(func() {
					if d.closed {
						return
					}
					d.busy = false
					d.err = "Sign-in failed: " + err.Error()
				})
			}
		}()
	})).Build()
}

func (d *LoginDialog) acceptExchangedHostedSignIn(ctx context.Context, signIn HostedSignIn, result hostedSignInResult) error {
	var decisionMu sync.Mutex
	resolved, accepted := false, false
	var resolutionErr error
	completed := make(chan struct{}, 1)
	d.Schedule(func() {
		decisionMu.Lock()
		defer decisionMu.Unlock()
		if resolved {
			return
		}
		if err := ctx.Err(); err != nil {
			resolved, resolutionErr = true, err
			completed <- struct{}{}
			return
		}
		if d.closed {
			resolved, resolutionErr = true, context.Canceled
			completed <- struct{}{}
			return
		}
		err := d.Client.acceptHostedSignIn(ctx, signIn, result)
		resolved, accepted, resolutionErr = true, err == nil, err
		d.busy = false
		if err != nil {
			d.err = "Sign-in failed: " + err.Error()
		} else if d.Client.HostedAccount().Attached {
			if reconnectErr := d.Client.RetryReconnect(); reconnectErr != nil {
				d.err = "Signed in. Use Retry in the session panel when reconnect is ready."
				completed <- struct{}{}
				return
			}
			d.completed = true
		} else {
			d.completed = true
		}
		completed <- struct{}{}
	})

	select {
	case <-completed:
	case <-ctx.Done():
	}
	decisionMu.Lock()
	if !resolved {
		resolved, resolutionErr = true, ctx.Err()
	}
	err, revoke := resolutionErr, !accepted
	decisionMu.Unlock()
	if revoke && result.Token != "" {
		d.Client.discardHostedLogin(signIn.baseURL, result.Token)
	}
	return err
}

type SessionMetadataDialog struct {
	Submit    func(title string, community bool)
	title     string
	community bool
}

func (*SessionMetadataDialog) Name() string         { return "Start Hosted Session" }
func (*SessionMetadataDialog) HasCloseButton() bool { return true }
func (d *SessionMetadataDialog) Process() {
	if imgui.IsKeyPressed(int(glfw.KeyEscape)) {
		imgui.CloseCurrentPopup()
		return
	}
	w.InputTextWithHint("##hosted-title", "Session title (optional)", &d.title).Width(520).Build()
	imgui.Checkbox("Community", &d.community)
	imgui.TextWrapped("Private sessions use invitations. Community sessions are listed to signed-in users while someone is connected; new members join as editors.")
	if imgui.Button("Create session") {
		d.Submit(d.title, d.community)
	}
}
