package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sdmm/internal/aphelion/collab/model"
	"testing"
	"time"
)

func TestHostedSessionCanReauthenticateAfterServerLosesUnexpiredCredential(t *testing.T) {
	now := time.Now()
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	const oldToken = "old-hosted-token"
	const newToken = "reauthenticated-hosted-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/hosted/sessions":
			if r.Header.Get("Authorization") != "Bearer "+oldToken {
				t.Errorf("browser request authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "unauthorized"})
		case "/v1/auth/desktop/begin":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorization_url": "https://discord.com/oauth2/authorize?state=opaque",
				"handoff_id":        "handoff-id",
			})
		case "/v1/auth/desktop/exchange":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"actor_id":     actorID,
				"token":        newToken,
				"display_name": "Mapper",
				"expires_at":   now.Add(12 * time.Hour),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewSessionClient(SessionClientConfig{Now: func() time.Time { return now }})
	client.hostedCredential = oldToken
	client.hostedCredentialExpires = now.Add(12 * time.Hour)
	client.hostedBaseURL = server.URL
	client.hostedActorID = actorID
	client.hostedSession = true
	client.baseURL = server.URL
	client.actorID = actorID
	client.resumptionToken = oldToken
	client.resumptionExpiresAt = now.Add(12 * time.Hour)
	client.administrationToken = oldToken
	client.administrationExpiresAt = now.Add(12 * time.Hour)

	if _, err := client.ListHostedSessions(context.Background(), "community", ""); err == nil {
		t.Fatal("browser request with the server-lost credential succeeded")
	}
	if account := client.HostedAccount(); account.SignedIn || !account.Attached {
		t.Fatalf("local account state after server 401 = %#v", account)
	}

	signIn, err := client.BeginHostedSignIn(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("begin reauthentication with an unexpired local credential: %v", err)
	}
	if err := client.CompleteHostedSignIn(context.Background(), signIn); err != nil {
		t.Fatalf("complete same-actor reauthentication: %v", err)
	}
	if client.hostedCredential != newToken || client.resumptionToken != newToken || client.administrationToken != newToken || client.hostedActorID != actorID {
		t.Fatalf("reauthentication did not replace hosted credentials for the attached actor")
	}
}
