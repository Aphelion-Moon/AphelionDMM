package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func TestHostedRegistryPersistsSessionMembershipAndOneUseInvitation(t *testing.T) {
	dsn, schema := isolatedSchema(t)
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(context.Background(), Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(context.Background(), fixture.Initial); err != nil {
		t.Fatal(err)
	}
	ownerActor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Unix(1000, 0).UTC()
	owner := collabstore.HostedMember{SessionID: "hosted-session", Issuer: "https://issuer.example", Subject: "owner", ActorID: ownerActor, DisplayName: "Owner", Role: collabstore.HostedRoleOwner}
	if err := value.CreateHostedSession(context.Background(), collabstore.HostedSession{SessionID: owner.SessionID, DocumentID: fixture.Initial.DocumentID, CreatedAt: createdAt}, owner); err != nil {
		t.Fatal(err)
	}
	sessions, err := value.ListHostedSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != owner.SessionID || sessions[0].DocumentID != fixture.Initial.DocumentID {
		t.Fatalf("hosted sessions = %#v", sessions)
	}
	loadedOwner, found, err := value.ResolveHostedMember(context.Background(), owner.SessionID, owner.Issuer, owner.Subject)
	if err != nil || !found || loadedOwner.ActorID != owner.ActorID || loadedOwner.Role != collabstore.HostedRoleOwner {
		t.Fatalf("resolved owner = %#v/%t/%v", loadedOwner, found, err)
	}
	if err := value.UpdateHostedMemberDisplayName(context.Background(), owner.SessionID, owner.ActorID, "Renamed Owner"); err != nil {
		t.Fatal(err)
	}
	loadedOwner, found, err = value.ResolveHostedMember(context.Background(), owner.SessionID, owner.Issuer, owner.Subject)
	if err != nil || !found || loadedOwner.DisplayName != "Renamed Owner" {
		t.Fatalf("renamed owner = %#v/%t/%v", loadedOwner, found, err)
	}

	tokenHash := sha256.Sum256([]byte("one-use-invitation"))
	invitation := collabstore.HostedInvitation{TokenHash: tokenHash, SessionID: owner.SessionID, Role: collabstore.HostedRoleEditor, CreatedByActorID: owner.ActorID, ExpiresAt: createdAt.Add(time.Minute)}
	if err := value.CreateHostedInvitation(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	joinedActor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	joined, err := value.RedeemHostedInvitation(context.Background(), owner.SessionID, tokenHash, collabstore.HostedIdentity{Issuer: "https://issuer.example", Subject: "joined", ActorID: joinedActor, DisplayName: "Joined"}, createdAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if joined.Role != collabstore.HostedRoleEditor || joined.SessionID != owner.SessionID {
		t.Fatalf("joined member = %#v", joined)
	}
	if _, err := value.RedeemHostedInvitation(context.Background(), owner.SessionID, tokenHash, collabstore.HostedIdentity{Issuer: "https://issuer.example", Subject: "reused", ActorID: joinedActor, DisplayName: "Reused"}, createdAt.Add(2*time.Second)); !errors.Is(err, collabstore.ErrHostedInvitationInvalid) {
		t.Fatalf("reused invitation error = %v", err)
	}
}

func TestHostedRegistryRejectsExpiredInvitationWithoutCreatingMember(t *testing.T) {
	dsn, schema := isolatedSchema(t)
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(context.Background(), Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(context.Background(), fixture.Initial); err != nil {
		t.Fatal(err)
	}
	actorID, _ := model.NewActorID()
	owner := collabstore.HostedMember{SessionID: "expired-session", Issuer: "https://issuer.example", Subject: "owner", ActorID: actorID, DisplayName: "Owner", Role: collabstore.HostedRoleOwner}
	if err := value.CreateHostedSession(context.Background(), collabstore.HostedSession{SessionID: owner.SessionID, DocumentID: fixture.Initial.DocumentID, CreatedAt: time.Unix(2000, 0).UTC()}, owner); err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte("expired"))
	if err := value.CreateHostedInvitation(context.Background(), collabstore.HostedInvitation{TokenHash: tokenHash, SessionID: owner.SessionID, Role: collabstore.HostedRoleViewer, CreatedByActorID: owner.ActorID, ExpiresAt: time.Unix(2001, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	joinedActor, _ := model.NewActorID()
	_, err = value.RedeemHostedInvitation(context.Background(), owner.SessionID, tokenHash, collabstore.HostedIdentity{Issuer: owner.Issuer, Subject: "late", ActorID: joinedActor, DisplayName: "Late"}, time.Unix(2002, 0).UTC())
	if !errors.Is(err, collabstore.ErrHostedInvitationInvalid) {
		t.Fatalf("expired invitation error = %v", err)
	}
	if _, found, err := value.ResolveHostedMember(context.Background(), owner.SessionID, owner.Issuer, "late"); err != nil || found {
		t.Fatalf("expired invitation member found = %t, error = %v", found, err)
	}
}

func TestHostedBrowserMetadataJoinAndListings(t *testing.T) {
	dsn, schema := isolatedSchema(t)
	value, fixtures := openHostedTestStore(t, dsn, schema, 2)
	ctx := context.Background()

	ownerActor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	communityOwner := collabstore.HostedMember{SessionID: "community-session", Issuer: "https://issuer.example", Subject: "community-owner", ActorID: ownerActor, DisplayName: "Community Owner", Role: collabstore.HostedRoleOwner}
	community := collabstore.HostedSession{
		SessionID: "community-session", DocumentID: fixtures[0].Initial.DocumentID, CreatedAt: time.Unix(4000, 0).UTC(),
		Visibility: collabstore.HostedVisibilityCommunity, Title: "Engineering lounge", MapLabel: "lounge.dmm", EnvironmentLabel: "station.dme",
	}
	if err := value.CreateHostedSession(ctx, community, communityOwner); err != nil {
		t.Fatal(err)
	}

	privateOwnerActor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	privateOwner := collabstore.HostedMember{SessionID: "private-session", Issuer: "https://issuer.example", Subject: "private-owner", ActorID: privateOwnerActor, DisplayName: "Private Owner", Role: collabstore.HostedRoleOwner}
	if err := value.CreateHostedSession(ctx, collabstore.HostedSession{SessionID: privateOwner.SessionID, DocumentID: fixtures[1].Initial.DocumentID, CreatedAt: community.CreatedAt}, privateOwner); err != nil {
		t.Fatal(err)
	}

	newActor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	newIdentity := collabstore.HostedIdentity{Issuer: "https://issuer.example", Subject: "new-member", ActorID: newActor, DisplayName: "New Member"}
	if _, created, err := value.JoinHostedSession(ctx, privateOwner.SessionID, newIdentity); !errors.Is(err, collabstore.ErrHostedSessionNotCommunity) || created {
		t.Fatalf("private admission = created %t, error %v; want not admitted", created, err)
	}
	member, created, err := value.JoinHostedSession(ctx, community.SessionID, newIdentity)
	if err != nil || !created || member.Role != collabstore.HostedRoleEditor || member.Disabled {
		t.Fatalf("community admission = %#v, created %t, error %v", member, created, err)
	}
	member, created, err = value.JoinHostedSession(ctx, community.SessionID, newIdentity)
	if err != nil || created || member.Role != collabstore.HostedRoleEditor {
		t.Fatalf("repeated community admission = %#v, created %t, error %v", member, created, err)
	}

	page, err := value.ListCommunityHostedSessions(ctx, newActor, []string{community.SessionID, privateOwner.SessionID}, collabstore.HostedSessionPageRequest{Limit: 10})
	if err != nil || len(page.Sessions) != 1 {
		t.Fatalf("community page = %#v, error %v", page, err)
	}
	listed := page.Sessions[0]
	if listed.Visibility != collabstore.HostedVisibilityCommunity || listed.Title != community.Title || listed.MapLabel != community.MapLabel || listed.EnvironmentLabel != community.EnvironmentLabel || listed.OwnerDisplayName != communityOwner.DisplayName {
		t.Fatalf("community session summary = %#v", listed)
	}

	myPage, err := value.ListMyHostedSessions(ctx, newActor, collabstore.HostedSessionPageRequest{Limit: 10})
	if err != nil || len(myPage.Sessions) != 1 || myPage.Sessions[0].SessionID != community.SessionID {
		t.Fatalf("member page = %#v, error %v", myPage, err)
	}
	ownerPage, err := value.ListMyHostedSessions(ctx, privateOwnerActor, collabstore.HostedSessionPageRequest{Limit: 10})
	if err != nil || len(ownerPage.Sessions) != 1 || ownerPage.Sessions[0].Visibility != collabstore.HostedVisibilityPrivate {
		t.Fatalf("default-private owner page = %#v, error %v", ownerPage, err)
	}

	updated := collabstore.HostedSessionMetadata{Visibility: collabstore.HostedVisibilityPrivate, Title: "Renamed lounge", MapLabel: "renamed.dmm", EnvironmentLabel: "station.dme"}
	if _, err := value.UpdateHostedSessionMetadata(ctx, community.SessionID, newActor, updated); !errors.Is(err, collabstore.ErrHostedSessionMissing) {
		t.Fatalf("non-owner metadata update error = %v, want hidden unauthorized result", err)
	}
	if _, err := value.UpdateHostedSessionMetadata(ctx, community.SessionID, ownerActor, collabstore.HostedSessionMetadata{Visibility: collabstore.HostedVisibilityCommunity, Title: strings.Repeat("界", 43)}); err == nil {
		t.Fatal("metadata update accepted title longer than 128 UTF-8 bytes")
	}
	if _, err := value.UpdateHostedSessionMetadata(ctx, community.SessionID, ownerActor, updated); err != nil {
		t.Fatal(err)
	}
	if _, created, err := value.JoinHostedSession(ctx, community.SessionID, newIdentity); err != nil || created {
		t.Fatalf("existing member after Private change = created %t, error %v", created, err)
	}
	lateActor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := value.JoinHostedSession(ctx, community.SessionID, collabstore.HostedIdentity{Issuer: "https://issuer.example", Subject: "late-member", ActorID: lateActor, DisplayName: "Late Member"}); !errors.Is(err, collabstore.ErrHostedSessionNotCommunity) || created {
		t.Fatalf("late private admission = created %t, error %v", created, err)
	}
	loaded, found, err := value.ResolveHostedMember(ctx, community.SessionID, newIdentity.Issuer, newIdentity.Subject)
	if err != nil || !found || loaded.Role != collabstore.HostedRoleEditor || loaded.Disabled {
		t.Fatalf("existing member after metadata update = %#v, found %t, error %v", loaded, found, err)
	}
}

func TestHostedJoinDoesNotReenableDisabledMember(t *testing.T) {
	dsn, schema := isolatedSchema(t)
	value, fixtures := openHostedTestStore(t, dsn, schema, 1)
	ctx := context.Background()
	ownerActor, _ := model.NewActorID()
	owner := collabstore.HostedMember{SessionID: "disabled-session", Issuer: "https://issuer.example", Subject: "owner", ActorID: ownerActor, DisplayName: "Owner", Role: collabstore.HostedRoleOwner}
	if err := value.CreateHostedSession(ctx, collabstore.HostedSession{SessionID: owner.SessionID, DocumentID: fixtures[0].Initial.DocumentID, CreatedAt: time.Unix(5000, 0).UTC(), Visibility: collabstore.HostedVisibilityCommunity}, owner); err != nil {
		t.Fatal(err)
	}
	actorID, _ := model.NewActorID()
	identity := collabstore.HostedIdentity{Issuer: "https://issuer.example", Subject: "disabled", ActorID: actorID, DisplayName: "Disabled"}
	if _, _, err := value.JoinHostedSession(ctx, owner.SessionID, identity); err != nil {
		t.Fatal(err)
	}
	if _, err := value.pool.Exec(ctx, `UPDATE collaboration_hosted_members SET disabled = TRUE WHERE session_id = $1 AND actor_id = $2`, owner.SessionID, actorID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := value.JoinHostedSession(ctx, owner.SessionID, identity); !errors.Is(err, collabstore.ErrHostedMemberDisabled) {
		t.Fatalf("disabled member admission error = %v", err)
	}
	page, err := value.ListCommunityHostedSessions(ctx, actorID, []string{owner.SessionID}, collabstore.HostedSessionPageRequest{Limit: 10})
	if err != nil || len(page.Sessions) != 0 {
		t.Fatalf("disabled member community listing = %#v, error %v", page, err)
	}
}

func TestHostedJoinSerializesBehindPrivateMetadataChange(t *testing.T) {
	dsn, schema := isolatedSchema(t)
	value, fixtures := openHostedTestStore(t, dsn, schema, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ownerActor, _ := model.NewActorID()
	owner := collabstore.HostedMember{SessionID: "serialized-session", Issuer: "https://issuer.example", Subject: "owner", ActorID: ownerActor, DisplayName: "Owner", Role: collabstore.HostedRoleOwner}
	if err := value.CreateHostedSession(ctx, collabstore.HostedSession{SessionID: owner.SessionID, DocumentID: fixtures[0].Initial.DocumentID, CreatedAt: time.Unix(6000, 0).UTC(), Visibility: collabstore.HostedVisibilityCommunity}, owner); err != nil {
		t.Fatal(err)
	}

	lockTransaction, err := value.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lockTransaction.Rollback(context.Background()) }()
	var sessionID string
	if err := lockTransaction.QueryRow(ctx, `SELECT session_id FROM collaboration_hosted_sessions WHERE session_id = $1 FOR UPDATE`, owner.SessionID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}

	updateResult := make(chan error, 1)
	go func() {
		_, err := value.UpdateHostedSessionMetadata(ctx, owner.SessionID, ownerActor, collabstore.HostedSessionMetadata{Visibility: collabstore.HostedVisibilityPrivate, Title: "Private now"})
		updateResult <- err
	}()
	waitForHostedRowLockWait(t, ctx, dsn, `SELECT session_id FROM collaboration_hosted_sessions`)

	actorID, _ := model.NewActorID()
	identity := collabstore.HostedIdentity{Issuer: "https://issuer.example", Subject: "after-private", ActorID: actorID, DisplayName: "After Private"}
	joinResult := make(chan error, 1)
	go func() {
		_, _, err := value.JoinHostedSession(ctx, owner.SessionID, identity)
		joinResult <- err
	}()
	waitForHostedRowLockWait(t, ctx, dsn, `SELECT visibility FROM collaboration_hosted_sessions`)

	if err := lockTransaction.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-updateResult; err != nil {
		t.Fatalf("private metadata update: %v", err)
	}
	if err := <-joinResult; !errors.Is(err, collabstore.ErrHostedSessionNotCommunity) {
		t.Fatalf("admission after queued Private update = %v", err)
	}
	if _, found, err := value.ResolveHostedMember(ctx, owner.SessionID, identity.Issuer, identity.Subject); err != nil || found {
		t.Fatalf("member after serialized private transition: found %t, error %v", found, err)
	}
}

func TestHostedMetadataMigrationDefaultsExistingSessionsToPrivate(t *testing.T) {
	dsn, schema := isolatedLegacySchema(t)
	connection, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	if _, err := connection.Exec(context.Background(), "SET search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(context.Background(), `INSERT INTO collaboration_documents(document_id, snapshot, snapshot_revision, snapshot_hash, current_revision, current_hash) VALUES('legacy-doc', '{}'::jsonb, 0, repeat('0', 64), 0, repeat('0', 64))`); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(context.Background(), `INSERT INTO collaboration_hosted_sessions(session_id, document_id, created_at) VALUES('legacy-session', 'legacy-doc', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	ownerActor, _ := model.NewActorID()
	if _, err := connection.Exec(context.Background(), `INSERT INTO collaboration_hosted_members(session_id, issuer, subject, actor_id, display_name, role) VALUES('legacy-session', 'https://issuer.example', 'owner', $1, 'Owner', 'owner')`, ownerActor); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close(context.Background())

	value, err := Open(context.Background(), Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	sessions, err := value.ListHostedSessions(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("migrated sessions = %#v, error %v", sessions, err)
	}
	if sessions[0].Visibility != collabstore.HostedVisibilityPrivate || sessions[0].Title != "" || sessions[0].MapLabel != "" || sessions[0].EnvironmentLabel != "" {
		t.Fatalf("migrated metadata = %#v; want Private and empty labels", sessions[0])
	}
}

func openHostedTestStore(t *testing.T, dsn, schema string, documentCount int) (*Store, []collabstore.ConformanceFixture) {
	t.Helper()
	value, err := Open(context.Background(), Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	fixtures := make([]collabstore.ConformanceFixture, 0, documentCount)
	for range documentCount {
		fixture, err := collabstore.NewConformanceFixture()
		if err != nil {
			t.Fatal(err)
		}
		if err := value.Create(context.Background(), fixture.Initial); err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, fixture)
	}
	return value, fixtures
}

func waitForHostedRowLockWait(t *testing.T, ctx context.Context, dsn, queryFragment string) {
	t.Helper()
	monitor, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = monitor.Close(context.Background()) }()
	for {
		var waiting bool
		if err := monitor.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%' || $1 || '%')`, queryFragment).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("timed out waiting for database row lock: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
