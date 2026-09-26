package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sdmm/internal/aphelion/collab/model"
	"strings"
	"testing"
	"time"
)

func TestHostedSignInExchangeWaitsForScheduledAcceptance(t *testing.T) {
	client, server, _ := newHostedLoginAcceptanceFixture(t, false)
	defer server.Close()

	signIn, err := client.BeginHostedSignIn(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.exchangeHostedSignIn(context.Background(), signIn)
	if err != nil {
		t.Fatal(err)
	}
	if client.hostedCredential != "old-hosted-token" {
		t.Fatal("exchange replaced the prior credential before UI acceptance")
	}

	jobs := make(chan func(), 1)
	dialog := &LoginDialog{Client: client, Schedule: func(job func()) { jobs <- job }, Origin: server.URL, busy: true}
	completed := make(chan error, 1)
	go func() { completed <- dialog.acceptExchangedHostedSignIn(context.Background(), signIn, result) }()
	job := <-jobs
	if client.hostedCredential != "old-hosted-token" {
		t.Fatal("queued UI work replaced the prior credential before it ran")
	}
	job()
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if client.hostedCredential != "new-hosted-token" || !dialog.completed {
		t.Fatalf("accepted credential = %q; dialog completed = %t", client.hostedCredential, dialog.completed)
	}
}

func TestClosedHostedLoginRevokesExchangedCredentialAndKeepsAttachedSession(t *testing.T) {
	client, server, loggedOut := newHostedLoginAcceptanceFixture(t, true)
	defer server.Close()

	signIn, err := client.BeginHostedSignIn(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.exchangeHostedSignIn(context.Background(), signIn)
	if err != nil {
		t.Fatal(err)
	}
	if result.Token != "new-hosted-token" || client.hostedCredential != "old-hosted-token" {
		t.Fatal("exchange did not remain separate from credential acceptance")
	}

	ctx, cancel := context.WithCancel(context.Background())
	jobs := make(chan func(), 1)
	dialog := &LoginDialog{Client: client, Schedule: func(job func()) { jobs <- job }, Origin: server.URL, busy: true, cancel: cancel}
	completed := make(chan error, 1)
	go func() { completed <- dialog.acceptExchangedHostedSignIn(ctx, signIn, result) }()
	job := <-jobs

	dialog.OnClose()
	if err := <-completed; !errors.Is(err, context.Canceled) {
		t.Fatalf("closed-login completion error = %v", err)
	}
	select {
	case token := <-loggedOut:
		if token != "new-hosted-token" {
			t.Fatalf("revoked token = %q", token)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled exchange result was not revoked")
	}

	job()
	if client.hostedCredential != "old-hosted-token" || client.resumptionToken != "old-hosted-token" || client.administrationToken != "old-hosted-token" || client.hostedActorID != client.actorID {
		t.Fatal("closing reauthentication replaced credentials for the attached actor")
	}
}

func newHostedLoginAcceptanceFixture(t *testing.T, attached bool) (*SessionClient, *httptest.Server, <-chan string) {
	t.Helper()
	now := time.Now()
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	loggedOut := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/auth/desktop/begin":
			_ = json.NewEncoder(w).Encode(map[string]string{"authorization_url": "https://discord.com/oauth2/authorize", "handoff_id": "handoff-id"})
		case "/v1/auth/desktop/exchange":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"actor_id": actorID, "token": "new-hosted-token", "display_name": "Mapper", "expires_at": now.Add(time.Hour),
			})
		case "/v1/auth/logout":
			loggedOut <- strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	client := NewSessionClient(SessionClientConfig{Now: func() time.Time { return now }})
	client.hostedCredential = "old-hosted-token"
	client.hostedCredentialExpires = now.Add(time.Hour)
	client.hostedDisplayName = "Mapper"
	client.hostedBaseURL = server.URL
	client.hostedActorID = actorID
	client.hostedSession = attached
	client.baseURL = server.URL
	client.actorID = actorID
	client.resumptionToken = "old-hosted-token"
	client.resumptionExpiresAt = now.Add(time.Hour)
	client.administrationToken = "old-hosted-token"
	client.administrationExpiresAt = now.Add(time.Hour)
	return client, server, loggedOut
}
