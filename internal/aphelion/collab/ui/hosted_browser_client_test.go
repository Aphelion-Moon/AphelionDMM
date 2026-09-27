package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestHostedBrowserPreservesOriginAndRejectsStalePage(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Query().Get("scope") != "mine" {
			t.Error("wrong browsing credential or scope")
		}
		close(entered)
		<-release
		if err := json.NewEncoder(w).Encode(protocol.HostedSessionsPage{Sessions: []protocol.HostedSessionSummary{{SessionID: "old"}}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := NewSessionClient(SessionClientConfig{})
	client.hostedCredential = "secret"
	client.hostedBaseURL = server.URL
	client.hostedCredentialExpires = time.Now().Add(time.Hour)
	result := make(chan error, 1)
	go func() { _, err := client.ListHostedSessions(context.Background(), "mine", ""); result <- err }()
	<-entered
	client.CancelHostedSignIn()
	close(release)
	if err := <-result; !errors.Is(err, ErrSessionChanged) {
		t.Fatalf("stale browser result: %v", err)
	}
}

func TestHostedMutationDoesNotUseChangedAccount(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
	defer server.Close()
	client := NewSessionClient(SessionClientConfig{})
	client.hostedBaseURL = server.URL
	client.hostedCredential = "first"
	client.hostedCredentialExpires = time.Now().Add(time.Hour)
	selected := client.HostedAccount()
	client.hostedGeneration++
	client.hostedCredential = "second"
	err := client.hostedBrowserRequest(context.Background(), selected, "POST", "/v1/hosted/sessions/old/join", nil, nil, nil)
	if !errors.Is(err, ErrSessionChanged) || called {
		t.Fatalf("old selection sent under changed account: called %v error %v", called, err)
	}
	if err = client.UpdateHostedSession(context.Background(), selected, "old", protocol.HostedSessionMetadata{Visibility: "private"}); !errors.Is(err, ErrSessionChanged) || called {
		t.Fatalf("queued settings used a changed account: called %v error %v", called, err)
	}
	if _, err = client.AdmitHostedSession(context.Background(), selected, "old"); !errors.Is(err, ErrSessionChanged) || called {
		t.Fatalf("queued admission used a changed account: called %v error %v", called, err)
	}
	if _, err = client.CreateHostedWithMetadata(context.Background(), selected, model.Snapshot{}, protocol.HostedSessionMetadata{}); !errors.Is(err, ErrSessionChanged) || called {
		t.Fatalf("queued creation used a changed account: called %v error %v", called, err)
	}
	target := HostedConnection{BaseURL: server.URL, SessionID: "old", generation: selected.Generation}
	if err = client.JoinHosted(context.Background(), target); !errors.Is(err, ErrSessionChanged) {
		t.Fatal("old target joined with changed account")
	}
}

func TestHostedDelayedSignInCannotReplaceNewerAccount(t *testing.T) {
	actor, _ := model.NewActorID()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/desktop/begin" {
			if err := json.NewEncoder(w).Encode(map[string]string{"authorization_url": "https://provider.example/auth", "handoff_id": "id"}); err != nil {
				t.Error(err)
			}
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"actor_id": actor, "token": "secret", "display_name": "Mapper", "expires_at": time.Now().Add(time.Hour)}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := NewSessionClient(SessionClientConfig{})
	old, err := client.BeginHostedSignIn(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := client.BeginHostedSignIn(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.CompleteHostedSignIn(context.Background(), old); !errors.Is(err, ErrSessionChanged) {
		t.Fatalf("stale sign-in accepted: %v", err)
	}
	if err = client.CompleteHostedSignIn(context.Background(), newer); err != nil {
		t.Fatal(err)
	}
}

func TestHostedReauthenticationRejectsDifferentActor(t *testing.T) {
	oldActor, _ := model.NewActorID()
	otherActor, _ := model.NewActorID()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/desktop/begin" {
			if err := json.NewEncoder(w).Encode(map[string]string{"authorization_url": "https://provider.example/auth", "handoff_id": "id"}); err != nil {
				t.Error(err)
			}
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"actor_id": otherActor, "token": "wrong-account", "display_name": "Other", "expires_at": time.Now().Add(time.Hour)}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := NewSessionClient(SessionClientConfig{})
	client.hostedSession = true
	client.baseURL = server.URL
	client.actorID = oldActor
	signIn, err := client.BeginHostedSignIn(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.CompleteHostedSignIn(context.Background(), signIn); err == nil {
		t.Fatal("different actor inherited attachment")
	}
	if client.hostedCredential != "" || client.actorID != oldActor {
		t.Fatal("rejected account changed attachment")
	}
}
