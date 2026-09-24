package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	collabstore "sdmm/internal/aphelion/collab/store"
)

func TestPostgresReplayPagesMixLegacyAndVersionedBodies(t *testing.T) {
	ctx := context.Background()
	dsn, schema := isolatedSchema(t)
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(ctx, Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(ctx, fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, fixture.First); err != nil {
		t.Fatal(err)
	}
	if err := value.ConfigureTransactions(ctx, fixture.Initial.DocumentID, collabstore.BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	if err := value.AppendTransaction(ctx, makePostgresV2Body(t, fixture.Second, fixture.Latest)); err != nil {
		t.Fatal(err)
	}

	first, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.SnapshotRevision != fixture.Initial.Revision || first.HeadRevision != fixture.Latest.Revision || !first.HasMore || len(first.Entries) != 1 {
		t.Fatalf("first replay page = %#v", first)
	}
	if first.Entries[0].StorageVersion != collabstore.LegacyTransactionVersion || first.Entries[0].Accepted.OperationID != fixture.First.OperationID || len(first.Entries[0].Accepted.Changes) != 1 {
		t.Fatalf("legacy replay entry = %#v", first.Entries[0])
	}

	checkpoint := first.SnapshotRevision
	second, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.First.Revision, fixture.Latest.Revision, &checkpoint, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantHash, err := fixture.Latest.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if second.SnapshotRevision != checkpoint || second.ThroughRevision != fixture.Latest.Revision || second.HasMore || len(second.Entries) != 1 {
		t.Fatalf("second replay page = %#v", second)
	}
	entry := second.Entries[0]
	if entry.StorageVersion != collabstore.BulkTransactionVersion || entry.Accepted.OperationID != fixture.Second.OperationID || entry.Accepted.Revision != fixture.Second.Revision || len(entry.Accepted.Changes) != 0 || entry.MapHash != wantHash || entry.ChangeCount != int64(len(fixture.Second.Changes)) || entry.BodyBytes <= 0 || len(entry.BodyDigest) != 64 {
		t.Fatalf("versioned replay metadata = %#v", entry)
	}

	if err := value.SaveSnapshot(ctx, fixture.FirstSnapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.First.Revision, fixture.Latest.Revision, &checkpoint, 1); !errors.Is(err, collabstore.ErrReplayCheckpointChanged) {
		t.Fatalf("page after concurrent PostgreSQL checkpoint = %v", err)
	}
	stale, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if stale.SnapshotRevision != fixture.First.Revision || len(stale.Entries) != 0 {
		t.Fatalf("stale replay page = %#v", stale)
	}

	if _, err := value.pool.Exec(ctx, `UPDATE collaboration_operations SET body_bytes = body_bytes + 1 WHERE document_id = $1 AND operation_id = $2`, fixture.Initial.DocumentID, fixture.Second.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.First.Revision, fixture.Latest.Revision, nil, 1); err == nil {
		t.Fatal("replay page accepted an invalid stored transaction body size")
	}
}

func TestPostgresReplayPagesRejectRevisionGapsAndOversizedMetadata(t *testing.T) {
	ctx := context.Background()
	dsn, schema := isolatedSchema(t)
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(ctx, Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(ctx, fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, fixture.First); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, fixture.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := value.pool.Exec(ctx, `DELETE FROM collaboration_operations WHERE document_id = $1 AND operation_id = $2`, fixture.Initial.DocumentID, fixture.First.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1); !errors.Is(err, collabstore.ErrReplayGap) {
		t.Fatalf("page with deleted revision = %v, want %v", err, collabstore.ErrReplayGap)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, collabstore.MaxReplayPageEntries+1); !errors.Is(err, collabstore.ErrReplayPageLimit) {
		t.Fatalf("page over hard metadata cap = %v, want %v", err, collabstore.ErrReplayPageLimit)
	}
	if _, err := value.pool.Exec(ctx, `UPDATE collaboration_documents SET snapshot_hash = $2 WHERE document_id = $1`, fixture.Initial.DocumentID, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1); err == nil {
		t.Fatal("page accepted damaged checkpoint hash")
	}
}

func TestPostgresReplayPageRejectsOutOfRangeThroughRevision(t *testing.T) {
	ctx := context.Background()
	dsn, schema := isolatedSchema(t)
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(ctx, Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(ctx, fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1); !errors.Is(err, collabstore.ErrReplayRevisionRange) {
		t.Fatalf("page beyond durable head = %v, want %v", err, collabstore.ErrReplayRevisionRange)
	}
}
