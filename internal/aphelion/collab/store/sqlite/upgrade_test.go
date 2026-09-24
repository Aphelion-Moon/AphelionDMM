package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	collabstore "sdmm/internal/aphelion/collab/store"
)

func TestUpgradeTransactionsCreatesDistinctV4AndPreservesV3Source(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "legacy-v3.db")
	createV3Fixture(t, sourcePath, fixture)
	before, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	sourceHash := sha256.Sum256(before)
	destinationPath := filepath.Join(t.TempDir(), "upgraded-v4.db")
	if err := UpgradeTransactions(ctx, sourcePath, destinationPath); err != nil {
		t.Fatalf("UpgradeTransactions(): %v", err)
	}
	after, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != sourceHash {
		t.Fatal("explicit staged upgrade changed the retained V3 source file")
	}
	legacy, err := sql.Open("sqlite", "file:"+filepath.ToSlash(sourcePath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = legacy.Close() }()
	var legacyAccepted []byte
	if err := legacy.QueryRowContext(ctx, "SELECT accepted FROM operations WHERE document_id = ? AND operation_id = ?", fixture.Initial.DocumentID, fixture.First.OperationID).Scan(&legacyAccepted); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })
	if upgraded.schemaVersion != 4 {
		t.Fatalf("upgraded schema version = %d; want 4", upgraded.schemaVersion)
	}
	version, err := upgraded.TransactionVersion(ctx, fixture.Initial.DocumentID)
	if err != nil || version != collabstore.LegacyTransactionVersion {
		t.Fatalf("copied document transaction version = %d, %v; want V1", version, err)
	}
	var copiedAccepted []byte
	if err := upgraded.database.QueryRowContext(ctx, "SELECT accepted FROM operations WHERE document_id = ? AND operation_id = ?", fixture.Initial.DocumentID, fixture.First.OperationID).Scan(&copiedAccepted); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copiedAccepted, legacyAccepted) {
		t.Fatal("explicit upgrade rewrote a retained V1 accepted-operation payload")
	}
	if err := upgraded.ConfigureTransactions(ctx, fixture.Initial.DocumentID, collabstore.BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.Append(ctx, fixture.Second); err != nil {
		t.Fatalf("append V2 after staged upgrade: %v", err)
	}
	if _, err := os.Stat(destinationPath); err != nil {
		t.Fatalf("published upgraded database is missing: %v", err)
	}
	if err := UpgradeTransactions(ctx, sourcePath, destinationPath); err == nil {
		t.Fatal("explicit upgrade overwrote an existing destination")
	}
}

func TestOpenLegacyV3DoesNotApplyV4Migration(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "legacy-open.db")
	createV3Fixture(t, path, fixture)
	value, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = value.Close() }()
	if value.schemaVersion != 3 {
		t.Fatalf("ordinary Open silently changed schema version to %d", value.schemaVersion)
	}
	if err := value.ConfigureTransactions(ctx, fixture.Initial.DocumentID, collabstore.BulkTransactionVersion); !errors.Is(err, collabstore.ErrTransactionUpgradeRequired) {
		t.Fatalf("V2 configuration error = %v; want explicit upgrade requirement", err)
	}
	page, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.First.Revision, nil, 1)
	if err != nil {
		t.Fatalf("legacy V3 replay page: %v", err)
	}
	if page.SnapshotRevision != fixture.Initial.Revision || page.HeadRevision != fixture.First.Revision || len(page.Entries) != 1 || page.Entries[0].StorageVersion != collabstore.LegacyTransactionVersion || page.Entries[0].Accepted.OperationID != fixture.First.OperationID {
		t.Fatalf("legacy V3 replay page = %#v", page)
	}
	if _, err := value.LoadRecovery(ctx, fixture.Initial.DocumentID); err != nil {
		t.Fatalf("legacy V1 recovery no longer works: %v", err)
	}
}

func createV3Fixture(t *testing.T, path string, fixture collabstore.ConformanceFixture) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	defer func() { _ = database.Close() }()
	for _, migration := range []string{initialSchema, revisionHashesSchema, exportCheckpointsSchema} {
		if _, err := database.ExecContext(context.Background(), migration); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := json.Marshal(fixture.Initial)
	if err != nil {
		t.Fatal(err)
	}
	mapHash, err := fixture.Initial.Hash()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), "INSERT INTO documents(document_id, snapshot, snapshot_revision, snapshot_hash) VALUES(?, ?, ?, ?)", fixture.Initial.DocumentID, snapshot, fixture.Initial.Revision, mapHash); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	accepted, err := json.Marshal(fixture.First)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	firstHash, err := fixture.FirstSnapshot.Hash()
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), "INSERT INTO operations(document_id, operation_id, revision, accepted, map_hash) VALUES(?, ?, ?, ?, ?)", fixture.Initial.DocumentID, fixture.First.OperationID, fixture.First.Revision, accepted, firstHash); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), "INSERT INTO revision_hashes(document_id, revision, map_hash) VALUES(?, ?, ?)", fixture.Initial.DocumentID, fixture.Initial.Revision, mapHash); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), "INSERT INTO revision_hashes(document_id, revision, map_hash) VALUES(?, ?, ?)", fixture.Initial.DocumentID, fixture.First.Revision, firstHash); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
