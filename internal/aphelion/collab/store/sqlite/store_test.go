package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func TestStoreConformance(t *testing.T) {
	t.Parallel()

	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "collaboration.db")
	if err := collabstore.VerifyConformance(context.Background(), func() (collabstore.SessionStore, error) {
		return Open(path)
	}, fixture); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSQLiteVersionMeetsMinimum(t *testing.T) {
	t.Parallel()

	value, err := Open(filepath.Join(t.TempDir(), "version.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if compareVersions(value.Version(), minimumSQLiteVersion) < 0 {
		t.Fatalf("SQLite version = %s, want at least %s", value.Version(), minimumSQLiteVersion)
	}
	t.Logf("SQLite runtime version: %s", value.Version())
}

func TestStoreSurvivesCloseAndReopen(t *testing.T) {
	t.Parallel()

	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "restart.db")
	value, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Create(context.Background(), fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(context.Background(), fixture.First); err != nil {
		t.Fatal(err)
	}
	if err := value.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	_, replay, err := reopened.Load(context.Background(), fixture.Initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) != 1 || replay[0].OperationID != fixture.First.OperationID {
		t.Fatalf("replay after reopen = %#v, want operation %q", replay, fixture.First.OperationID)
	}
}

func TestExportCheckpointSurvivesRestartAndCompletion(t *testing.T) {
	t.Parallel()

	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	checkpointID, err := model.NewCheckpointID()
	if err != nil {
		t.Fatal(err)
	}
	mapHash, err := fixture.Initial.Hash()
	if err != nil {
		t.Fatal(err)
	}
	pending := model.ExportCheckpoint{
		CheckpointID: checkpointID, IdempotencyKey: "restart-export", DocumentID: fixture.Initial.DocumentID,
		SessionID: "session-1", Revision: fixture.Initial.Revision, MapHash: mapHash, RequestedBy: fixture.First.ActorID,
		CreatedAt: time.Unix(10, 0).UTC(), Status: model.ExportCheckpointPending,
	}
	path := filepath.Join(t.TempDir(), "checkpoint-restart.db")
	value, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Create(context.Background(), fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if _, created, err := value.CreateExportCheckpoint(context.Background(), pending); err != nil || !created {
		t.Fatalf("CreateExportCheckpoint() created/error = %t/%v", created, err)
	}
	if err := value.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stored, found, err := reopened.LookupExportCheckpoint(context.Background(), fixture.Initial.DocumentID, checkpointID)
	if err != nil || !found || !reflect.DeepEqual(stored, pending) {
		t.Fatalf("checkpoint after first reopen = %#v/%t/%v", stored, found, err)
	}
	completion := model.ExportCheckpointCompletion{
		Status: model.ExportCheckpointAccepted, ArtifactHash: strings.Repeat("b", 64), Verifier: "meridian-mcp", VerifierVersion: "1.0.0", CompletedAt: time.Unix(20, 0).UTC(),
	}
	completed, err := reopened.CompleteExportCheckpoint(context.Background(), fixture.Initial.DocumentID, checkpointID, completion)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedAgain, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedAgain.Close() })
	stored, found, err = reopenedAgain.LookupExportCheckpoint(context.Background(), fixture.Initial.DocumentID, checkpointID)
	if err != nil || !found || !reflect.DeepEqual(stored, completed) {
		t.Fatalf("completed checkpoint after second reopen = %#v/%t/%v", stored, found, err)
	}
}

func TestOpenMigratesRevisionHashesFromSchemaOne(t *testing.T) {
	t.Parallel()

	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "schema-one.db")
	value, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Create(context.Background(), fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(context.Background(), fixture.First); err != nil {
		t.Fatal(err)
	}
	if _, err := value.database.Exec("DROP TABLE revision_hashes"); err != nil {
		t.Fatal(err)
	}
	if _, err := value.database.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	if err := value.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	initialHash, err := fixture.Initial.Hash()
	if err != nil {
		t.Fatal(err)
	}
	firstHash, err := fixture.FirstSnapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	for revision, expected := range map[model.Revision]string{
		fixture.Initial.Revision: initialHash,
		fixture.First.Revision:   firstHash,
	} {
		actual, found, err := reopened.RevisionHash(context.Background(), fixture.Initial.DocumentID, revision)
		if err != nil || !found || actual != expected {
			t.Fatalf("RevisionHash(%d) = %q, %t, %v; want %q", revision, actual, found, err, expected)
		}
	}
}

func TestConcurrentDuplicateAppendIsIdempotent(t *testing.T) {
	t.Parallel()

	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(filepath.Join(t.TempDir(), "duplicate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(context.Background(), fixture.Initial); err != nil {
		t.Fatal(err)
	}

	const workers = 16
	errorsFound := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := value.Append(context.Background(), fixture.First); err != nil {
				errorsFound <- err
			}
		}()
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("Append() error = %v", err)
	}
	_, replay, err := value.Load(context.Background(), fixture.Initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) != 1 {
		t.Fatalf("replay length = %d, want 1", len(replay))
	}
}

func TestOpenRejectsFutureSchema(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "future.db")
	value, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := value.database.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	if err := value.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open() error = nil for future schema")
	}
}

func TestLockedDatabaseHonorsContextAndRollsBack(t *testing.T) {
	t.Parallel()

	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "locked.db")
	value, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	locker, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locker.Close() })
	connection, err := locker.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if _, err := connection.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = value.Create(ctx, fixture.Initial)
	if err == nil || (!errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "database is locked")) {
		t.Fatalf("Create() error = %v, want deadline or locked error", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("locked Create() elapsed = %v, want at most 1s", elapsed)
	}
	if _, err := connection.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if err := value.Create(context.Background(), fixture.Initial); err != nil {
		t.Fatalf("Create() after rollback: %v", err)
	}
}

func TestAppendFailureRollsBackTransaction(t *testing.T) {
	t.Parallel()

	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(filepath.Join(t.TempDir(), "rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(context.Background(), fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(context.Background(), fixture.First); err != nil {
		t.Fatal(err)
	}
	if _, err := value.database.Exec(`CREATE TRIGGER fail_operation BEFORE INSERT ON operations BEGIN SELECT RAISE(ABORT, 'injected append failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(context.Background(), fixture.Second); err == nil {
		t.Fatal("Append() error = nil with failure trigger")
	}
	if _, err := value.database.Exec("DROP TRIGGER fail_operation"); err != nil {
		t.Fatal(err)
	}
	_, replay, err := value.Load(context.Background(), fixture.Initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) != 1 || !reflect.DeepEqual(replay[0], fixture.First) {
		t.Fatalf("replay after failed append = %#v, want first operation only", replay)
	}
	// A different ID must still apply to the pre-failure state. Retrying only
	// the same ID could hide an uncommitted operation left in a mutable cache.
	retry := model.CloneAcceptedOperation(fixture.Second)
	retry.OperationID, err = model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Append(context.Background(), retry); err != nil {
		t.Fatalf("Append() after rollback: %v", err)
	}
}

func TestAppendRechecksChangedDurableHistory(t *testing.T) {
	for _, mutation := range []string{"operation", "hash", "snapshot", "missing operation"} {
		t.Run(mutation, func(t *testing.T) {
			ctx := context.Background()
			fixture, err := collabstore.NewConformanceFixture()
			if err != nil {
				t.Fatal(err)
			}
			value, err := Open(filepath.Join(t.TempDir(), "history.db"))
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
			switch mutation {
			case "operation":
				corrupt := model.CloneAcceptedOperation(fixture.First)
				corrupt.BaseMapHash = strings.Repeat("f", 64)
				encoded, err := json.Marshal(corrupt)
				if err != nil {
					t.Fatal(err)
				}
				_, err = value.database.Exec("UPDATE operations SET accepted = ? WHERE document_id = ?", encoded, fixture.Initial.DocumentID)
				if err != nil {
					t.Fatal(err)
				}
			case "hash":
				_, err = value.database.Exec("UPDATE revision_hashes SET map_hash = ? WHERE revision = 0", strings.Repeat("f", 64))
			case "snapshot":
				_, err = value.database.Exec("UPDATE documents SET snapshot_hash = ?", strings.Repeat("f", 64))
			case "missing operation":
				_, err = value.database.Exec("DELETE FROM operations")
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := value.Load(ctx, fixture.Initial.DocumentID); err == nil {
				t.Fatal("load accepted changed corrupt history after an earlier successful append")
			}
			if _, _, _, err := value.LoadReplay(ctx, fixture.Initial.DocumentID); err == nil {
				t.Fatal("replay accepted changed corrupt history")
			}
			if err := value.Append(ctx, fixture.Second); err == nil {
				t.Fatal("append accepted changed corrupt history after an earlier successful append")
			}
			if _, found, err := value.LookupOperation(ctx, fixture.Initial.DocumentID, fixture.Second.OperationID); err != nil || found {
				t.Fatalf("failed append was persisted: found=%t err=%v", found, err)
			}
		})
	}
}

func TestLoadDoesNotExposeValidatedHistory(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value, err := Open(filepath.Join(t.TempDir(), "load.db"))
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
	for range 2 {
		snapshot, replay, hashes, err := value.LoadReplay(ctx, fixture.Initial.DocumentID)
		if err != nil || !reflect.DeepEqual(snapshot, model.CloneSnapshot(fixture.Initial)) || len(replay) != 1 || !reflect.DeepEqual(replay[0], fixture.First) {
			t.Fatalf("load differs from durable history: %v", err)
		}
		wantHash, found, err := value.RevisionHash(ctx, fixture.Initial.DocumentID, fixture.First.Revision)
		if err != nil || !found || hashes[fixture.First.Revision] != wantHash {
			t.Fatalf("replay hash differs from durable history: %v", err)
		}
		hashes[fixture.First.Revision] = strings.Repeat("a", 64)
		replay[0].BaseMapHash = strings.Repeat("f", 64)
		snapshot.EnvironmentHash = strings.Repeat("e", 64)
	}
	if err := value.Append(ctx, fixture.Second); err != nil {
		t.Fatalf("append after modifying returned data: %v", err)
	}
}

func TestAppendObservesOtherStoreAndSnapshot(t *testing.T) {
	ctx := context.Background()
	fixture, err := collabstore.NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shared.db")
	value, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = value.Close() }()
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	if err := value.Create(ctx, fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, fixture.First); err != nil {
		t.Fatal(err)
	}
	if err := other.SaveSnapshot(ctx, fixture.FirstSnapshot); err != nil {
		t.Fatal(err)
	}
	if err := other.Append(ctx, fixture.Second); err != nil {
		t.Fatal(err)
	}
	state, err := other.LoadRecovery(ctx, fixture.Initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := state.Restore()
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	inverse, err := document.BuildInverse(fixture.First.ActorID, fixture.First.OperationID, id)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := document.Apply(inverse, time.Unix(3, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if err := value.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	state, err = reopened.LoadRecovery(ctx, fixture.Initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := state.Restore()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Snapshot(), document.Snapshot()) {
		t.Fatal("reopen lost external append, snapshot, or actor-scoped inverse")
	}
}
