package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
	"sdmm/internal/aphelion/collab/transaction"
)

func TestV2TransactionAppendRetryRecoveryAndReopen(t *testing.T) {
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
	version, err := value.TransactionVersion(ctx, fixture.Initial.DocumentID)
	if err != nil || version != collabstore.LegacyTransactionVersion {
		t.Fatalf("default transaction version = %d, %v", version, err)
	}
	if err := value.ConfigureTransactions(ctx, fixture.Initial.DocumentID, collabstore.BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	body := makePostgresV2Body(t, fixture.First, fixture.FirstSnapshot)
	if err := value.AppendTransaction(ctx, body); err != nil {
		t.Fatalf("AppendTransaction(): %v", err)
	}
	if err := value.AppendTransaction(ctx, body); err != nil {
		t.Fatalf("exact AppendTransaction retry: %v", err)
	}
	stream, found, err := value.OpenTransaction(ctx, fixture.Initial.DocumentID, fixture.First.OperationID)
	if err != nil || !found {
		t.Fatalf("OpenTransaction() found/error = %t/%v", found, err)
	}
	reader, err := transaction.OpenReader(ctx, stream)
	if err != nil {
		_ = stream.Close()
		t.Fatal(err)
	}
	operation, readErr := reader.Materialize()
	closeErr := stream.Close()
	if readErr != nil || closeErr != nil || !reflect.DeepEqual(operation, fixture.First.Operation) {
		t.Fatalf("streamed operation differs: read/close error = %v/%v", readErr, closeErr)
	}
	state, err := value.LoadRecovery(ctx, fixture.Initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := state.Restore()
	if err != nil || !reflect.DeepEqual(recovered.Snapshot(), fixture.FirstSnapshot) {
		t.Fatalf("recovered snapshot differs: %v", err)
	}
	inverseID, err := model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	inverse, err := recovered.BuildInverse(fixture.First.ActorID, fixture.First.OperationID, inverseID)
	if err != nil {
		t.Fatal(err)
	}
	inverseAccepted, err := recovered.Apply(inverse, fixture.First.AcceptedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, inverseAccepted); err != nil {
		t.Fatalf("append inverse operation through V2 storage: %v", err)
	}
	if err := value.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	version, err = reopened.TransactionVersion(ctx, fixture.Initial.DocumentID)
	if err != nil || version != collabstore.BulkTransactionVersion {
		t.Fatalf("transaction version after reopen = %d, %v", version, err)
	}
	accepted, found, err := reopened.LookupOperation(ctx, fixture.Initial.DocumentID, fixture.First.OperationID)
	if err != nil || !found || !reflect.DeepEqual(accepted, fixture.First) {
		t.Fatalf("accepted operation after reopen = %#v/%t/%v", accepted, found, err)
	}
	accepted, found, err = reopened.LookupOperation(ctx, fixture.Initial.DocumentID, inverseID)
	if err != nil || !found || !reflect.DeepEqual(accepted, inverseAccepted) || accepted.InverseOf == nil || *accepted.InverseOf != fixture.First.OperationID {
		t.Fatalf("inverse operation identity after reopen = %#v/%t/%v", accepted, found, err)
	}
	changed := model.CloneOperation(fixture.First.Operation)
	changed.Changes[0].After.Prefabs[0].Path = "/turf/open/changed"
	document, err := engine.NewDocument(fixture.Initial)
	if err != nil {
		t.Fatal(err)
	}
	changedAccepted, err := document.Apply(changed, fixture.First.AcceptedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.AppendTransaction(ctx, makePostgresV2Body(t, changedAccepted, document.Snapshot())); err == nil {
		t.Fatal("same operation ID with a changed body was accepted")
	}
	if _, err := reopened.pool.Exec(ctx, `UPDATE collaboration_transaction_chunks SET data = data || decode('00', 'hex') WHERE operation_id = $1 AND chunk_index = 0`, fixture.First.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.LoadRecovery(ctx, fixture.Initial.DocumentID); err == nil {
		t.Fatal("corrupt transaction chunk was accepted during recovery")
	}
}

func TestV2DocumentStoresSmallAppendInlineBesideStreamedRows(t *testing.T) {
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
	if err := value.ConfigureTransactions(ctx, fixture.Initial.DocumentID, collabstore.BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, fixture.First); err != nil {
		t.Fatal(err)
	}
	var storageVersion, chunkCount int
	if err := value.pool.QueryRow(ctx, `SELECT storage_version, (SELECT COUNT(*) FROM collaboration_transaction_chunks WHERE operation_id = o.operation_id) FROM collaboration_operations o WHERE document_id = $1 AND operation_id = $2`, fixture.Initial.DocumentID, fixture.First.OperationID).Scan(&storageVersion, &chunkCount); err != nil {
		t.Fatal(err)
	}
	if storageVersion != collabstore.LegacyTransactionVersion || chunkCount != 0 {
		t.Fatalf("small Append storage version/chunk count = %d/%d; want inline V1 without chunks", storageVersion, chunkCount)
	}
	if err := value.AppendTransaction(ctx, makePostgresV2Body(t, fixture.Second, fixture.Latest)); err != nil {
		t.Fatalf("AppendTransaction(): %v", err)
	}
	if err := value.pool.QueryRow(ctx, `SELECT storage_version, (SELECT COUNT(*) FROM collaboration_transaction_chunks WHERE operation_id = o.operation_id) FROM collaboration_operations o WHERE document_id = $1 AND operation_id = $2`, fixture.Initial.DocumentID, fixture.Second.OperationID).Scan(&storageVersion, &chunkCount); err != nil {
		t.Fatal(err)
	}
	if storageVersion != collabstore.BulkTransactionVersion || chunkCount == 0 {
		t.Fatalf("explicit transaction storage version/chunk count = %d/%d; want V2 chunks", storageVersion, chunkCount)
	}
	recovery, err := value.LoadRecovery(ctx, fixture.Initial.DocumentID)
	if err != nil || len(recovery.Operations) != 2 || !reflect.DeepEqual(recovery.Operations[0], fixture.First) || !reflect.DeepEqual(recovery.Operations[1], fixture.Second) {
		t.Fatalf("mixed V1/V2 recovery differs: operations=%#v error=%v", recovery.Operations, err)
	}
	page, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 128)
	if err != nil || len(page.Entries) != 2 || page.Entries[0].StorageVersion != collabstore.LegacyTransactionVersion || page.Entries[1].StorageVersion != collabstore.BulkTransactionVersion {
		t.Fatalf("mixed V1/V2 replay page versions = %#v/%v", page.Entries, err)
	}
}

func TestRecoveryRejectsBulkMetadataOnInlineV1Row(t *testing.T) {
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
	if _, err := value.pool.Exec(ctx, `UPDATE collaboration_operations SET body_digest = $1, body_bytes = 1, change_count = 1 WHERE document_id = $2 AND operation_id = $3`, strings.Repeat("a", 64), fixture.Initial.DocumentID, fixture.First.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadRecovery(ctx, fixture.Initial.DocumentID); err == nil {
		t.Fatal("inline V1 row with bulk metadata was accepted during recovery")
	}
}

func TestV2PostgresAppendRollsBackHeaderAndChunksTogether(t *testing.T) {
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
	initial, accepted := largePostgresOperation(t, fixture, 5000)
	if err := value.Create(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if err := value.ConfigureTransactions(ctx, initial.DocumentID, collabstore.BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := value.pool.Exec(ctx, `CREATE FUNCTION aphelion_fail_transaction_chunk() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.chunk_index = 1 THEN RAISE EXCEPTION 'injected chunk failure'; END IF; RETURN NEW; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := value.pool.Exec(ctx, `CREATE TRIGGER aphelion_fail_transaction_chunk BEFORE INSERT ON collaboration_transaction_chunks FOR EACH ROW EXECUTE FUNCTION aphelion_fail_transaction_chunk()`); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, accepted); err == nil {
		t.Fatal("append succeeded despite injected second-chunk failure")
	}
	var operationRows, chunkRows, revisionRows int
	if err := value.pool.QueryRow(ctx, `SELECT COUNT(*) FROM collaboration_operations WHERE document_id = $1 AND operation_id = $2`, initial.DocumentID, accepted.OperationID).Scan(&operationRows); err != nil {
		t.Fatal(err)
	}
	if err := value.pool.QueryRow(ctx, `SELECT COUNT(*) FROM collaboration_transaction_chunks WHERE operation_id = $1`, accepted.OperationID).Scan(&chunkRows); err != nil {
		t.Fatal(err)
	}
	if err := value.pool.QueryRow(ctx, `SELECT COUNT(*) FROM collaboration_revision_hashes WHERE document_id = $1 AND revision = $2`, initial.DocumentID, accepted.Revision).Scan(&revisionRows); err != nil {
		t.Fatal(err)
	}
	if operationRows != 0 || chunkRows != 0 || revisionRows != 0 {
		t.Fatalf("failed append left rows: operation=%d chunks=%d revision=%d", operationRows, chunkRows, revisionRows)
	}
	if _, err := value.pool.Exec(ctx, `DROP TRIGGER aphelion_fail_transaction_chunk ON collaboration_transaction_chunks`); err != nil {
		t.Fatal(err)
	}
	if _, err := value.pool.Exec(ctx, `DROP FUNCTION aphelion_fail_transaction_chunk()`); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, accepted); err != nil {
		t.Fatalf("append after injected rollback: %v", err)
	}
}

func TestUpgradeTransactionsCopiesV3ToDistinctV4Schema(t *testing.T) {
	ctx := context.Background()
	dsn, sourceSchema := isolatedLegacySchema(t)
	destinationSchema := uniqueSchemaName(t, "aphelion_v4_stage_")
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	source, err := Open(ctx, Config{DSN: dsn, Schema: sourceSchema})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Create(ctx, fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if err := source.Append(ctx, fixture.First); err != nil {
		t.Fatal(err)
	}
	ownerActor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Unix(1_800_000_000, 0).UTC()
	owner := collabstore.HostedMember{SessionID: "legacy-hosted", Issuer: "https://issuer.example", Subject: "owner", ActorID: ownerActor, DisplayName: "Owner", Role: collabstore.HostedRoleOwner}
	if err := source.CreateHostedSession(ctx, collabstore.HostedSession{SessionID: owner.SessionID, DocumentID: fixture.Initial.DocumentID, CreatedAt: createdAt}, owner); err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte("retained-upgrade-invitation"))
	if err := source.CreateHostedInvitation(ctx, collabstore.HostedInvitation{TokenHash: tokenHash, SessionID: owner.SessionID, Role: collabstore.HostedRoleEditor, CreatedByActorID: owner.ActorID, ExpiresAt: createdAt.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	checkpointID, err := model.NewCheckpointID()
	if err != nil {
		t.Fatal(err)
	}
	firstHash, err := fixture.FirstSnapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := model.ExportCheckpoint{CheckpointID: checkpointID, IdempotencyKey: "legacy-upgrade-checkpoint", DocumentID: fixture.Initial.DocumentID, SessionID: owner.SessionID, Revision: fixture.First.Revision, MapHash: firstHash, RequestedBy: owner.ActorID, CreatedAt: createdAt, Status: model.ExportCheckpointPending}
	if _, created, err := source.CreateExportCheckpoint(ctx, checkpoint); err != nil || !created {
		t.Fatalf("create source checkpoint = %t, %v", created, err)
	}
	if source.schemaVersion != 3 {
		t.Fatalf("source schema version = %d; want V3", source.schemaVersion)
	}
	if err := source.ConfigureTransactions(ctx, fixture.Initial.DocumentID, collabstore.BulkTransactionVersion); !errors.Is(err, collabstore.ErrTransactionUpgradeRequired) {
		t.Fatalf("V2 configuration on an existing V3 source = %v; want explicit upgrade requirement", err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := UpgradeTransactions(ctx, Config{DSN: dsn, Schema: sourceSchema}, Config{DSN: dsn, Schema: destinationSchema}); err != nil {
		t.Fatalf("UpgradeTransactions(): %v", err)
	}
	t.Cleanup(func() {
		admin, err := pgx.Connect(ctx, dsn)
		if err == nil {
			_, _ = admin.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{destinationSchema}.Sanitize()+` CASCADE`)
			_ = admin.Close(ctx)
		}
	})
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{destinationSchema}.Sanitize()+` CASCADE`)
		_ = admin.Close(ctx)
	})
	legacy, err := Open(ctx, Config{DSN: dsn, Schema: sourceSchema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	if legacy.schemaVersion != 3 {
		t.Fatalf("source schema changed to version %d", legacy.schemaVersion)
	}
	legacyState, err := legacy.LoadRecovery(ctx, fixture.Initial.DocumentID)
	if err != nil || len(legacyState.Operations) != 1 || !reflect.DeepEqual(legacyState.Operations[0], fixture.First) {
		t.Fatalf("retained V3 source changed: operations=%#v error=%v", legacyState.Operations, err)
	}
	upgraded, err := Open(ctx, Config{DSN: dsn, Schema: destinationSchema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })
	if upgraded.schemaVersion != 4 {
		t.Fatalf("destination schema version = %d; want V4", upgraded.schemaVersion)
	}
	version, err := upgraded.TransactionVersion(ctx, fixture.Initial.DocumentID)
	if err != nil || version != collabstore.LegacyTransactionVersion {
		t.Fatalf("copied transaction version = %d, %v; want V1", version, err)
	}
	upgradedState, err := upgraded.LoadRecovery(ctx, fixture.Initial.DocumentID)
	if err != nil || !reflect.DeepEqual(upgradedState, legacyState) {
		t.Fatalf("copied recovery differs: %v", err)
	}
	loadedCheckpoint, found, err := upgraded.LookupExportCheckpoint(ctx, fixture.Initial.DocumentID, checkpointID)
	if err != nil || !found || loadedCheckpoint.CheckpointID != checkpoint.CheckpointID || loadedCheckpoint.MapHash != checkpoint.MapHash || loadedCheckpoint.Status != model.ExportCheckpointPending {
		t.Fatalf("copied checkpoint = %#v/%t/%v", loadedCheckpoint, found, err)
	}
	sessions, err := upgraded.ListHostedSessions(ctx)
	if err != nil || len(sessions) != 1 || sessions[0].SessionID != owner.SessionID {
		t.Fatalf("copied hosted sessions = %#v/%v", sessions, err)
	}
	members, err := upgraded.ListHostedMembers(ctx, owner.SessionID)
	if err != nil || len(members) != 1 || members[0].ActorID != owner.ActorID {
		t.Fatalf("copied hosted members = %#v/%v", members, err)
	}
	joinedActor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	joined, err := upgraded.RedeemHostedInvitation(ctx, owner.SessionID, tokenHash, collabstore.HostedIdentity{Issuer: "https://issuer.example", Subject: "joined", ActorID: joinedActor, DisplayName: "Joined"}, createdAt.Add(time.Minute))
	if err != nil || joined.Role != collabstore.HostedRoleEditor {
		t.Fatalf("redeem copied invitation = %#v/%v", joined, err)
	}
	if err := upgraded.ConfigureTransactions(ctx, fixture.Initial.DocumentID, collabstore.BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.Append(ctx, fixture.Second); err != nil {
		t.Fatalf("append to explicit V4 destination: %v", err)
	}
	legacyState, err = legacy.LoadRecovery(ctx, fixture.Initial.DocumentID)
	if err != nil || len(legacyState.Operations) != 1 {
		t.Fatalf("source changed after destination append: operations=%#v error=%v", legacyState.Operations, err)
	}
}

func isolatedLegacySchema(t *testing.T) (string, string) {
	t.Helper()
	dsn := testingPostgresDSN(t)
	schema := uniqueSchemaName(t, "aphelion_v3_source_")
	connection, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(context.Background(), `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		_ = connection.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = connection.Exec(context.Background(), `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		_ = connection.Close(context.Background())
	})
	if _, err := connection.Exec(context.Background(), `SET search_path TO `+pgx.Identifier{schema}.Sanitize()); err != nil {
		_ = connection.Close(context.Background())
		t.Fatal(err)
	}
	for _, migration := range []string{initialSchema, hostedRegistrySchema, exportCheckpointsSchema} {
		if _, err := connection.Exec(context.Background(), migration); err != nil {
			_ = connection.Close(context.Background())
			t.Fatal(err)
		}
	}
	if _, err := connection.Exec(context.Background(), `CREATE TABLE collaboration_schema_migrations (version INTEGER PRIMARY KEY CHECK (version > 0), applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP); INSERT INTO collaboration_schema_migrations(version) VALUES (1), (2), (3)`); err != nil {
		_ = connection.Close(context.Background())
		t.Fatal(err)
	}
	return dsn, schema
}

func testingPostgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("APHELION_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("APHELION_POSTGRES_TEST_DSN is not configured")
	}
	return dsn
}

func uniqueSchemaName(t *testing.T, prefix string) string {
	t.Helper()
	id, err := model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	return prefix + strings.ReplaceAll(string(id), "-", "")
}

func makePostgresV2Body(t *testing.T, accepted model.AcceptedOperation, snapshot model.Snapshot) *transaction.Body {
	t.Helper()
	mapHash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	operation := model.CloneOperation(accepted.Operation)
	count := int64(len(operation.Changes))
	operation.Changes = nil
	body, err := transaction.Prepare(context.Background(), t.TempDir(), transaction.NewBudget(64<<20), transaction.Header{
		Version: transaction.Version, Count: count, Kind: "accepted", Operation: operation,
		Revision: accepted.Revision, AcceptedAt: accepted.AcceptedAt, MapHash: mapHash,
	}, accepted.Changes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = body.Close() })
	return body
}

func largePostgresOperation(t *testing.T, fixture collabstore.ConformanceFixture, count int) (model.Snapshot, model.AcceptedOperation) {
	t.Helper()
	snapshot := model.CloneSnapshot(fixture.Initial)
	snapshot.MaxX = 256
	snapshot.MaxY = 256
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	operation := model.CloneOperation(fixture.First.Operation)
	operation.Changes = make([]model.TileChange, count)
	stableIDs := make([]model.StableID, count)
	for index := range stableIDs {
		stableIDs[index], err = model.NewStableID()
		if err != nil {
			t.Fatal(err)
		}
	}
	for index := range operation.Changes {
		coord := model.Coord{X: index%int(snapshot.MaxX) + 1, Y: index/int(snapshot.MaxX) + 1, Z: 1}
		operation.Changes[index] = model.TileChange{
			Coord:  coord,
			Before: model.TileState{},
			After:  model.TileState{Prefabs: []model.PrefabState{{StableID: stableIDs[index], Path: "/turf/open/floor", Vars: map[string]string{"payload": strings.Repeat("x", 16)}}}},
		}
	}
	operation.DocumentID = snapshot.DocumentID
	operation.EnvironmentHash = snapshot.EnvironmentHash
	operation.BaseRevision = snapshot.Revision
	operation.BaseMapHash, err = snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := document.Apply(operation, fixture.First.AcceptedAt)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, accepted
}
