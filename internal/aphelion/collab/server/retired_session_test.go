package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

// legacyProtocolStore presents one stored document as written by protocol v1.
type legacyProtocolStore struct {
	SessionStore
	legacy model.DocumentID
}

func (*legacyProtocolStore) Close() error { return nil }

func (store *legacyProtocolStore) LoadRecovery(ctx context.Context, documentID model.DocumentID) (engine.RecoveryState, error) {
	state, err := store.SessionStore.LoadRecovery(ctx, documentID)
	if err == nil && documentID == store.legacy {
		state.Snapshot.ProtocolVersion = 1
	}
	return state, err
}

// A session stored by retired protocol v1 must neither stop startup recovery
// nor mark the service unready; joining it explains why it cannot open, and
// current sessions recover normally.
func TestRecoverHostedSessionsSkipsRetiredProtocolSessions(t *testing.T) {
	now := time.Unix(20_000, 0).UTC()
	ownerActor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	backend := newFakeHostedBackend(map[string]auth.Session{
		"owner-auth": {Token: "owner-auth", ActorID: ownerActor, Issuer: "https://issuer.example", Subject: "owner", DisplayName: "Owner", Role: auth.RoleViewer, ExpiresAt: now.Add(time.Hour)},
	})
	durable := &nonClosingSessionStore{SessionStore: NewMemoryStore()}
	first := NewService(ServiceConfig{Store: durable, HostedAuth: backend, HostedRegistry: backend, Now: func() time.Time { return now }})
	firstServer := httptest.NewServer(first.Handler())
	create := func(snapshot model.Snapshot) string {
		body, err := json.Marshal(map[string]any{"snapshot": snapshot})
		if err != nil {
			t.Fatal(err)
		}
		response := postJSON(t, firstServer.URL+"/v1/hosted/sessions", "owner-auth", body)
		defer response.Body.Close()
		var created HostedSessionResponse
		if response.StatusCode != http.StatusCreated || json.NewDecoder(response.Body).Decode(&created) != nil {
			t.Fatalf("create hosted session status = %d", response.StatusCode)
		}
		return created.SessionID
	}
	legacy, current := testSnapshot(t, 2), testSnapshot(t, 2)
	legacySession, currentSession := create(legacy), create(current)
	firstServer.Close()
	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	var retired []string
	second := NewService(ServiceConfig{Store: &legacyProtocolStore{SessionStore: durable, legacy: legacy.DocumentID}, HostedAuth: backend, HostedRegistry: backend, Now: func() time.Time { return now },
		OnRetiredSession: func(sessionID string, _ error) { retired = append(retired, sessionID) }})
	t.Cleanup(func() { _ = second.Shutdown(context.Background()) })
	if err := second.RecoverHostedSessions(context.Background()); err != nil {
		t.Fatalf("a retired session stopped recovery: %v", err)
	}
	if len(retired) != 1 || retired[0] != legacySession {
		t.Fatalf("retired sessions reported = %v, want [%s]", retired, legacySession)
	}
	secondServer := httptest.NewServer(second.Handler())
	t.Cleanup(secondServer.Close)

	ready, err := http.Get(secondServer.URL + "/v1/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	_ = ready.Body.Close()
	if ready.StatusCode != http.StatusOK {
		t.Fatalf("a retired session made the service unready: %d", ready.StatusCode)
	}

	join := postJSON(t, secondServer.URL+"/v1/hosted/sessions/"+legacySession+"/join", "owner-auth", []byte(`{}`))
	message, _ := io.ReadAll(join.Body)
	_ = join.Body.Close()
	if join.StatusCode != http.StatusGone || !strings.Contains(string(message), "protocol v1") || !strings.Contains(string(message), legacySession) {
		t.Fatalf("join retired session = %d %s", join.StatusCode, message)
	}

	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, secondServer.URL+"/v1/sessions/"+string(current.DocumentID)+"/snapshot", nil)
	request.Header.Set("Authorization", "Bearer owner-auth")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("current session %s did not recover: %d", currentSession, response.StatusCode)
	}
}
