package ui

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
	"sdmm/internal/aphelion/collab/store/postgres"
)

type browserIdentityFlow struct{}

func (browserIdentityFlow) AuthorizationURL(state, _, _ string) string {
	return "https://identity.example/authorize?state=" + state
}
func (browserIdentityFlow) Exchange(_ context.Context, code, _, _ string) (auth.Identity, error) {
	return auth.Identity{Issuer: "https://identity.example", Subject: code, DisplayName: code, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func TestHostedBrowserPostgresClientLifecycle(t *testing.T) {
	dsn := os.Getenv("APHELION_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("APHELION_POSTGRES_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, _ := model.NewOperationID()
	schema := "aphelion_browser_" + strings.ReplaceAll(string(id), "-", "")
	database, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := database.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		if err := database.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	registry, err := postgres.Open(ctx, postgres.Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := registry.Close(); err != nil {
			t.Error(err)
		}
	}()
	directory := auth.NewRegistryDirectory(registry)
	login := auth.NewManager(browserIdentityFlow{}, directory, auth.ManagerConfig{SessionDirectory: directory})
	host := httptest.NewUnstartedServer(nil)
	origin := "http://" + host.Listener.Addr().String()
	service := server.NewService(server.ServiceConfig{Store: registry, HostedRegistry: registry, HostedAuth: login, HostedLogin: login, AllowedOrigins: []string{origin}})
	host.Config.Handler = service.Handler()
	host.Start()
	defer host.Close()
	defer func() { _ = service.Shutdown(context.Background()) }()
	signedIn := func(name string) *SessionClient {
		begin, err := login.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		session, err := login.Complete(ctx, begin.State, name)
		if err != nil {
			t.Fatal(err)
		}
		client := NewSessionClient(SessionClientConfig{})
		client.hostedCredential = session.Token
		client.hostedCredentialExpires = session.ExpiresAt
		client.hostedActorID = session.ActorID
		client.hostedBaseURL = origin
		client.hostedDisplayName = name
		t.Cleanup(func() { _ = client.Leave(context.Background()) })
		return client
	}
	owner, guest := signedIn("Owner"), signedIn("Guest")
	target, err := owner.CreateHostedWithMetadata(ctx, owner.HostedAccount(), controllerSnapshot(t), protocol.HostedSessionMetadata{Visibility: "community", Title: "Test map"})
	if err != nil {
		t.Fatal(err)
	}
	controller := NewController(nil, owner)
	if _, err = PrepareHostedSession(ctx, controller, owner, target); err != nil {
		t.Fatal(err)
	}
	var page protocol.HostedSessionsPage
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond * 10) {
		page, err = guest.ListHostedSessions(ctx, "community", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Sessions) == 1 {
			break
		}
	}
	if len(page.Sessions) != 1 || page.Sessions[0].Participants != 1 {
		t.Fatalf("active list = %#v", page)
	}
	admitted, err := guest.AdmitHostedSession(ctx, guest.HostedAccount(), target.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	guestController := NewController(nil, guest)
	guestExecution, err := PrepareHostedSession(ctx, guestController, guest, admitted)
	if err != nil {
		t.Fatal(err)
	}
	if guest.Status().Role != "editor" {
		t.Fatal("new community admission not editor")
	}
	before, err := guestExecution.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash, _ := before.Hash()
	if err = owner.UpdateHostedSession(ctx, owner.HostedAccount(), target.SessionID, protocol.HostedSessionMetadata{Visibility: "private", Title: "Private now"}); err != nil {
		t.Fatal(err)
	}
	page, err = guest.ListHostedSessions(ctx, "community", "")
	if err != nil || len(page.Sessions) != 0 {
		t.Fatalf("Private session leaked: %#v %v", page, err)
	}
	after, _ := guestExecution.Snapshot(ctx)
	afterHash, _ := after.Hash()
	if beforeHash != afterHash || before.Revision != after.Revision {
		t.Fatal("metadata changed map authority")
	}
	if err = guestController.Leave(ctx); err != nil {
		t.Fatal(err)
	}
	if err = controller.Leave(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = guest.ListHostedSessions(ctx, "mine", "")
	if err != nil || len(page.Sessions) != 1 {
		t.Fatalf("idle Private membership lost: %#v %v", page, err)
	}
	admitted, err = guest.AdmitHostedSession(ctx, guest.HostedAccount(), target.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareHostedSession(ctx, guestController, guest, admitted); err != nil {
		t.Fatal(err)
	}
	if guest.Status().Role != "editor" {
		t.Fatal("reopening changed role")
	}
	if err = guestController.Leave(ctx); err != nil {
		t.Fatal(err)
	}
}
