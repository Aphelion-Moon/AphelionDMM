package sqlite

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
	"sdmm/internal/aphelion/collab/transaction"
)

func TestV2TransactionAppendRetryRecoveryAndReopen(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "transactions.db")
	value, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(ctx, fixture.Initial); err != nil {
		t.Fatal(err)
	}
	version, err := value.TransactionVersion(ctx, fixture.Initial.DocumentID)
	if err != nil || version != collabstore.LegacyTransactionVersion {
		t.Fatalf("default transaction version = %d, %v; want %d", version, err, collabstore.LegacyTransactionVersion)
	}
	if err := value.ConfigureTransactions(ctx, fixture.Initial.DocumentID, collabstore.BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	body := makeV2TransactionBody(t, fixture.First, fixture.FirstSnapshot)
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
	if reader.Header.Version != transaction.Version || reader.Header.Kind != "accepted" || reader.Header.SessionID != "" || reader.Header.MessageID != "" {
		t.Fatalf("stored transaction header = %#v", reader.Header)
	}
	operation, err := reader.Materialize()
	closeErr := stream.Close()
	if err != nil || closeErr != nil || !reflect.DeepEqual(operation, fixture.First.Operation) {
		t.Fatalf("stored operation = %#v; want %#v; read/close errors = %v/%v", operation, fixture.First.Operation, err, closeErr)
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

	reopened, err := Open(path)
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
}

func TestV2DocumentStoresSmallAppendInlineBesideStreamedRows(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(filepath.Join(t.TempDir(), "mixed-transaction-rows.db"))
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
	if err := value.database.QueryRowContext(ctx, `SELECT storage_version, (SELECT COUNT(*) FROM transaction_chunks WHERE document_id = operations.document_id AND operation_id = operations.operation_id) FROM operations WHERE document_id = ? AND operation_id = ?`, fixture.Initial.DocumentID, fixture.First.OperationID).Scan(&storageVersion, &chunkCount); err != nil {
		t.Fatal(err)
	}
	if storageVersion != collabstore.LegacyTransactionVersion || chunkCount != 0 {
		t.Fatalf("small Append storage version/chunk count = %d/%d; want inline V1 without chunks", storageVersion, chunkCount)
	}
	if err := value.AppendTransaction(ctx, makeV2TransactionBody(t, fixture.Second, fixture.Latest)); err != nil {
		t.Fatalf("AppendTransaction(): %v", err)
	}
	if err := value.database.QueryRowContext(ctx, `SELECT storage_version, (SELECT COUNT(*) FROM transaction_chunks WHERE document_id = operations.document_id AND operation_id = operations.operation_id) FROM operations WHERE document_id = ? AND operation_id = ?`, fixture.Initial.DocumentID, fixture.Second.OperationID).Scan(&storageVersion, &chunkCount); err != nil {
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
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(filepath.Join(t.TempDir(), "inline-metadata-corruption.db"))
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
	if _, err := value.database.ExecContext(ctx, `UPDATE operations SET body_digest = ?, body_bytes = 1, change_count = 1 WHERE document_id = ? AND operation_id = ?`, strings.Repeat("a", 64), fixture.Initial.DocumentID, fixture.First.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadRecovery(ctx, fixture.Initial.DocumentID); err == nil {
		t.Fatal("inline V1 row with bulk metadata was accepted during recovery")
	}
}

func TestV2TransactionRejectsChangedBodyAndDetectsCorruption(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(filepath.Join(t.TempDir(), "transaction-integrity.db"))
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

	changedDocument, err := engine.NewDocument(fixture.Initial)
	if err != nil {
		t.Fatal(err)
	}
	changedOperation := model.CloneOperation(fixture.First.Operation)
	changedOperation.Changes[0].After.Prefabs[0].Path = "/turf/open/changed"
	changed, err := changedDocument.Apply(changedOperation, fixture.First.AcceptedAt)
	if err != nil {
		t.Fatal(err)
	}
	changedSnapshot := changedDocument.Snapshot()
	changedBody := makeV2TransactionBody(t, changed, changedSnapshot)
	if err := value.AppendTransaction(ctx, changedBody); err == nil {
		t.Fatal("same operation ID with changed body was accepted")
	}

	if _, err := value.database.ExecContext(ctx, `UPDATE transaction_chunks SET data = data || X'00' WHERE document_id = ? AND operation_id = ? AND chunk_index = 0`, fixture.Initial.DocumentID, fixture.First.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadRecovery(ctx, fixture.Initial.DocumentID); err == nil {
		t.Fatal("corrupt transaction chunk was accepted during recovery")
	}
}

func TestV2TransactionAppendRollsBackHeaderAndChunksTogether(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(filepath.Join(t.TempDir(), "transaction-rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	largeInitial, large := largeConformanceOperation(t, fixture, 20_000)
	if err := value.Create(ctx, largeInitial); err != nil {
		t.Fatal(err)
	}
	if err := value.ConfigureTransactions(ctx, largeInitial.DocumentID, collabstore.BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := value.database.ExecContext(ctx, `CREATE TRIGGER fail_transaction_chunk BEFORE INSERT ON transaction_chunks WHEN NEW.chunk_index = 1 BEGIN SELECT RAISE(ABORT, 'injected chunk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, large); err == nil {
		t.Fatal("append succeeded despite second-chunk failure")
	}
	var operationRows, chunkRows, revisionRows int
	if err := value.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM operations WHERE document_id = ? AND operation_id = ?`, fixture.Initial.DocumentID, large.OperationID).Scan(&operationRows); err != nil {
		t.Fatal(err)
	}
	if err := value.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM transaction_chunks WHERE document_id = ? AND operation_id = ?`, fixture.Initial.DocumentID, large.OperationID).Scan(&chunkRows); err != nil {
		t.Fatal(err)
	}
	if err := value.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM revision_hashes WHERE document_id = ? AND revision = ?`, fixture.Initial.DocumentID, large.Revision).Scan(&revisionRows); err != nil {
		t.Fatal(err)
	}
	if operationRows != 0 || chunkRows != 0 || revisionRows != 0 {
		t.Fatalf("failed append left rows: operation=%d chunks=%d revision=%d", operationRows, chunkRows, revisionRows)
	}
	if _, err := value.database.ExecContext(ctx, `DROP TRIGGER fail_transaction_chunk`); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, large); err != nil {
		t.Fatalf("append after injected rollback: %v", err)
	}
}

func makeV2TransactionBody(t *testing.T, accepted model.AcceptedOperation, snapshot model.Snapshot) *transaction.Body {
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

func largeConformanceOperation(t *testing.T, fixture collabstore.ConformanceFixture, count int) (model.Snapshot, model.AcceptedOperation) {
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
			After:  model.TileState{Prefabs: []model.PrefabState{{StableID: stableIDs[index], Path: "/turf/open/floor", Vars: map[string]string{}}}},
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
