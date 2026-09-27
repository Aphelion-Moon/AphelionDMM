package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func TestHostedNotificationSummaryBoundsRowsAndAggregatesPrivateActivity(t *testing.T) {
	dsn, schema := isolatedSchema(t)
	ctx := context.Background()
	value, err := Open(ctx, Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = value.Close() }()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 14; i++ {
		snapshot := fixture.Initial
		snapshot.DocumentID, err = model.NewDocumentID()
		if err != nil {
			t.Fatal(err)
		}
		if err := value.Create(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
		visibility := collabstore.HostedVisibilityCommunity
		if i >= 12 {
			visibility = collabstore.HostedVisibilityPrivate
		}
		id := fmt.Sprintf("notification-%02d", i)
		session := collabstore.HostedSession{SessionID: id, DocumentID: snapshot.DocumentID, CreatedAt: time.Now(), Visibility: visibility, Title: id}
		member := collabstore.HostedMember{SessionID: id, Issuer: "issuer", Subject: "owner", ActorID: actor, DisplayName: "Owner", Role: collabstore.HostedRoleOwner}
		if err := value.CreateHostedSession(ctx, session, member); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := value.HostedNotificationSummary(ctx, []string{"notification-00", "notification-12", "notification-12"})
	if err != nil {
		t.Fatal(err)
	}
	if summary.CommunityCount != 12 || summary.PrivateCount != 2 || summary.ActivePrivateCount != 1 || len(summary.Community) != 10 {
		t.Fatalf("unexpected bounded summary: %#v", summary)
	}
	for i, session := range summary.Community {
		if session.Title != fmt.Sprintf("notification-%02d", i) || session.Visibility != collabstore.HostedVisibilityCommunity {
			t.Fatalf("unexpected public row: %#v", session)
		}
	}
}
