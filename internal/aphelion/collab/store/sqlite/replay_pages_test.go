package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	collabstore "sdmm/internal/aphelion/collab/store"
	"sdmm/internal/aphelion/collab/transaction"
)

func TestSQLiteReplayPagesMixLegacyAndVersionedBodies(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(filepath.Join(t.TempDir(), "replay-pages.db"))
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
	if err := value.AppendTransaction(ctx, makeV2TransactionBody(t, fixture.Second, fixture.Latest)); err != nil {
		t.Fatal(err)
	}

	first, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.SnapshotRevision != fixture.Initial.Revision || first.HeadRevision != fixture.Latest.Revision || !first.HasMore || len(first.Entries) != 1 {
		t.Fatalf("first replay page = %#v", first)
	}
	if first.Entries[0].StorageVersion != collabstore.LegacyTransactionVersion || !reflect.DeepEqual(first.Entries[0].Accepted, fixture.First) || first.Entries[0].ChangeCount != int64(len(fixture.First.Changes)) {
		t.Fatalf("legacy replay entry = %#v", first.Entries[0])
	}

	checkpoint := first.SnapshotRevision
	second, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.First.Revision, fixture.Latest.Revision, &checkpoint, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.SnapshotRevision != checkpoint || second.ThroughRevision != fixture.Latest.Revision || second.HasMore || len(second.Entries) != 1 {
		t.Fatalf("second replay page = %#v", second)
	}
	entry := second.Entries[0]
	wantHash, err := fixture.Latest.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if entry.StorageVersion != collabstore.BulkTransactionVersion || entry.Accepted.OperationID != fixture.Second.OperationID || entry.Accepted.Revision != fixture.Second.Revision || !entry.Accepted.AcceptedAt.Equal(fixture.Second.AcceptedAt) || len(entry.Accepted.Changes) != 0 || entry.MapHash != wantHash || entry.ChangeCount != int64(len(fixture.Second.Changes)) || entry.BodyBytes <= 0 || len(entry.BodyDigest) != 64 {
		t.Fatalf("versioned replay metadata = %#v", entry)
	}

	stream, found, err := value.OpenTransaction(ctx, fixture.Initial.DocumentID, fixture.Second.OperationID)
	if err != nil || !found {
		t.Fatalf("open replay stream = %t, %v", found, err)
	}
	body, readErr := io.ReadAll(stream)
	closeErr := stream.Close()
	sum := sha256.Sum256(body)
	if readErr != nil || closeErr != nil || int64(len(body)) != entry.BodyBytes || hex.EncodeToString(sum[:]) != entry.BodyDigest {
		t.Fatalf("replay stream does not match indexed digest/size: read=%v close=%v bytes=%d digest=%s", readErr, closeErr, len(body), hex.EncodeToString(sum[:]))
	}

	if err := value.SaveSnapshot(ctx, fixture.FirstSnapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.First.Revision, fixture.Latest.Revision, &checkpoint, 1); !errors.Is(err, collabstore.ErrReplayCheckpointChanged) {
		t.Fatalf("page after concurrent SQLite checkpoint = %v", err)
	}
	stale, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if stale.SnapshotRevision != fixture.First.Revision || len(stale.Entries) != 0 {
		t.Fatalf("stale replay page = %#v", stale)
	}

	var encoded []byte
	if err := value.database.QueryRowContext(ctx, "SELECT accepted FROM operations WHERE document_id = ? AND operation_id = ?", fixture.Initial.DocumentID, fixture.Second.OperationID).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var header transaction.Header
	if err := json.Unmarshal(encoded, &header); err != nil {
		t.Fatal(err)
	}
	header.MapHash = strings.Repeat("f", 64)
	encoded, err = json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := value.database.ExecContext(ctx, "UPDATE operations SET accepted = ? WHERE document_id = ? AND operation_id = ?", encoded, fixture.Initial.DocumentID, fixture.Second.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.First.Revision, fixture.Latest.Revision, nil, 1); err == nil {
		t.Fatal("replay page accepted a header whose hash differs from its indexed row")
	}
}

func TestSQLiteReplayPagesRejectRevisionGapsAndOversizedPageMetadata(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(filepath.Join(t.TempDir(), "replay-gap.db"))
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
	if _, err := value.database.ExecContext(ctx, "DELETE FROM operations WHERE document_id = ? AND operation_id = ?", fixture.Initial.DocumentID, fixture.First.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1); !errors.Is(err, collabstore.ErrReplayGap) {
		t.Fatalf("page with deleted revision = %v, want %v", err, collabstore.ErrReplayGap)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, collabstore.MaxReplayPageEntries+1); !errors.Is(err, collabstore.ErrReplayPageLimit) {
		t.Fatalf("page over hard metadata cap = %v, want %v", err, collabstore.ErrReplayPageLimit)
	}
}

func TestSQLiteReplayPageChecksBodyIndexFields(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(filepath.Join(t.TempDir(), "replay-index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(ctx, fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if err := value.ConfigureTransactions(ctx, fixture.Initial.DocumentID, collabstore.BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	if err := value.AppendTransaction(ctx, makeV2TransactionBody(t, fixture.First, fixture.FirstSnapshot)); err != nil {
		t.Fatal(err)
	}
	if _, err := value.database.ExecContext(ctx, "UPDATE operations SET body_bytes = body_bytes + 1 WHERE document_id = ? AND operation_id = ?", fixture.Initial.DocumentID, fixture.First.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.First.Revision, nil, 1); err == nil {
		t.Fatal("replay page accepted an invalid stored transaction body size")
	}
}
