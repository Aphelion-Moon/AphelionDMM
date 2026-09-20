package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

func TestRecoveryCacheLimitTransitions(t *testing.T) {
	// Calibrate an ASCII variable on an unchanged tile so the first append's
	// complete encoded recovery state lands exactly at the byte boundary.
	probe, doc := seedRecoveryHistory(t, filepath.Join(t.TempDir(), "probe.db"), 0, false, 0)
	if err := probe.Append(context.Background(), nextRecoveryEdit(t, doc)); err != nil {
		t.Fatal(err)
	}
	var overhead int
	if _, err := readRecovery(context.Background(), probe.database, doc.Snapshot().DocumentID, &overhead); err != nil {
		t.Fatal(err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		history int
		padding int
	}{
		{"operations", recoveryCacheOperations - 1, 0},
		{"bytes_below", 0, recoveryCacheBytes - overhead - 1},
		{"bytes_at", 0, recoveryCacheBytes - overhead},
		{"bytes_above", 0, recoveryCacheBytes - overhead + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "boundary.db")
			storage, document := seedRecoveryHistory(t, path, test.history, false, test.padding)
			defer func() { _ = storage.Close() }()
			for step := 0; step < 3; step++ {
				accepted := nextRecoveryEdit(t, document)
				if err := storage.Append(context.Background(), accepted); err != nil {
					t.Fatal(err)
				}
				var encodedBytes int
				state, err := readRecovery(context.Background(), storage.database, accepted.DocumentID, &encodedBytes)
				if err != nil {
					t.Fatal(err)
				}
				wantRetained := step == 0 && test.name != "bytes_above"
				storage.recovery.mutex.Lock()
				entry := storage.recovery.entry
				storage.recovery.mutex.Unlock()
				if (entry != nil) != wantRetained {
					t.Fatalf("revision %d, encoded bytes %d: retained=%t, want %t", accepted.Revision, encodedBytes, entry != nil, wantRetained)
				}
				if entry != nil && !reflect.DeepEqual(entry.state, state) {
					t.Fatal("retained recovery differs from a complete durable read")
				}
				if step == 0 && test.padding != 0 && encodedBytes != overhead+test.padding {
					t.Fatalf("byte-boundary fixture differs: got %d, want %d", encodedBytes, overhead+test.padding)
				}
				t.Logf("revision=%d encoded_bytes=%d retained=%t", accepted.Revision, encodedBytes, entry != nil)
				verifyRecoveryHistory(t, storage, document)
			}
			// The uncached path must preserve inverse ownership and duplicate IDs.
			last := model.OperationID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", document.Snapshot().Revision))
			inverse, err := document.BuildInverse("01890f3e-7b5c-7abc-8def-0123456789ba", last, "01890f3e-7b5c-7abc-8def-ffffffffffff")
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := document.Apply(inverse, time.Unix(20000, 0).UTC())
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := storage.Append(context.Background(), accepted); err != nil {
					t.Fatal(err)
				}
			}
			if err := storage.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()
			restored := verifyRecoveryHistory(t, reopened, document)
			if _, err := restored.BuildInverse(accepted.ActorID, last, "01890f3e-7b5c-7abc-8def-eeeeeeeeeeee"); err == nil {
				t.Fatal("reopen forgot the already-inverted target")
			}
		})
	}
}

// Bulk setup writes engine-validated history in one transaction, outside timing.
// These are fixed-history component costs, not sequential-ingestion throughput.
// Both append modes start with identical rows and a preceding real append;
// cache_cleared_append then discards only the optional in-memory recovery entry.
// Compacted append fixtures snapshot revision history-1, leaving one replay row.
// Use -benchtime=1x and separate fresh processes for independent trials.
func BenchmarkSQLiteRecoveryHistory(b *testing.B) {
	for _, history := range []int{0, 100, 1000, 1024, 1025, 10000} {
		for _, compacted := range []bool{false, true} {
			for _, action := range []string{"cache_cleared_append", "following_append", "recovery"} {
				if history == 0 && action == "following_append" {
					continue
				}
				b.Run(fmt.Sprintf("history=%d/compacted=%t/%s", history, compacted, action), func(b *testing.B) {
					b.ReportAllocs()
					b.StopTimer()
					for iteration := 0; iteration < b.N; iteration++ {
						prefix := history
						if action != "recovery" && history > 0 {
							prefix--
						}
						path := filepath.Join(b.TempDir(), "history.db")
						storage, document := seedRecoveryHistory(b, path, prefix, compacted, 0)
						if action != "recovery" && history > 0 {
							if err := storage.Append(context.Background(), nextRecoveryEdit(b, document)); err != nil {
								b.Fatal(err)
							}
						}
						if action == "cache_cleared_append" {
							storage.recovery.clear()
						}
						if iteration == 0 {
							var encodedBytes int
							state, err := readRecovery(context.Background(), storage.database, document.Snapshot().DocumentID, &encodedBytes)
							if err != nil {
								b.Fatal(err)
							}
							b.Logf("history=%d snapshot_revision=%d encoded_bytes=%d cache_retained=%t head_hash=%s", len(state.Operations), state.Snapshot.Revision, encodedBytes, storage.recovery.entry != nil, state.HeadHash)
						}
						var accepted model.AcceptedOperation
						if action != "recovery" {
							accepted = nextRecoveryEdit(b, document)
						}
						documentID := document.Snapshot().DocumentID
						b.StartTimer()
						var err error
						if action == "recovery" {
							var state engine.RecoveryState
							state, err = storage.LoadRecovery(context.Background(), documentID)
							if err == nil {
								_, err = state.Restore()
							}
						} else {
							err = storage.Append(context.Background(), accepted)
						}
						b.StopTimer()
						if err != nil {
							b.Fatal(err)
						}
						if err := storage.Close(); err != nil {
							b.Fatal(err)
						}
						reopened, err := Open(path)
						if err != nil {
							b.Fatal(err)
						}
						verifyRecoveryHistory(b, reopened, document)
						if err := reopened.Close(); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func recoveryHistorySnapshot(padding int) model.Snapshot {
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion,
		DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: strings.Repeat("a", 64), MaxX: 2, MaxY: 1, MaxZ: 1}
	for x := 1; x <= 2; x++ {
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: x, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{
			StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", x)), Path: "/obj/unknown_fixture", Vars: map[string]string{"dir": "2"},
		}}}})
	}
	snapshot.Tiles[1].State.Prefabs[0].Vars["opaque"] = strings.Repeat("x", padding)
	return snapshot
}

func seedRecoveryHistory(tb testing.TB, path string, history int, compacted bool, padding int) (*Store, *engine.Document) {
	tb.Helper()
	snapshot := recoveryHistorySnapshot(padding)
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		tb.Fatal(err)
	}
	storage, err := Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = storage.Close() })
	ctx := context.Background()
	if err := storage.Create(ctx, snapshot); err != nil {
		tb.Fatal(err)
	}
	tx, err := storage.database.BeginTx(ctx, nil)
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	operationInsert, err := tx.PrepareContext(ctx, "INSERT INTO operations(document_id, operation_id, revision, accepted, map_hash) VALUES(?, ?, ?, ?, ?)")
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = operationInsert.Close() }()
	hashInsert, err := tx.PrepareContext(ctx, "INSERT INTO revision_hashes(document_id, revision, map_hash) VALUES(?, ?, ?)")
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = hashInsert.Close() }()
	for range history {
		accepted := nextRecoveryEdit(tb, document)
		encoded, err := json.Marshal(accepted)
		if err != nil {
			tb.Fatal(err)
		}
		hash, err := document.Snapshot().Hash()
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := operationInsert.ExecContext(ctx, accepted.DocumentID, accepted.OperationID, accepted.Revision, encoded, hash); err != nil {
			tb.Fatal(err)
		}
		if _, err := hashInsert.ExecContext(ctx, accepted.DocumentID, accepted.Revision, hash); err != nil {
			tb.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		tb.Fatal(err)
	}
	if compacted {
		if err := storage.SaveSnapshot(ctx, document.Snapshot()); err != nil {
			tb.Fatal(err)
		}
	}
	verifyRecoveryHistory(tb, storage, document)
	return storage, document
}

func nextRecoveryEdit(tb testing.TB, document *engine.Document) model.AcceptedOperation {
	tb.Helper()
	snapshot := document.Snapshot()
	hash, err := snapshot.Hash()
	if err != nil {
		tb.Fatal(err)
	}
	after := model.CloneTileState(snapshot.Tiles[0].State)
	if after.Prefabs[0].Vars["dir"] == "2" {
		after.Prefabs[0].Vars["dir"] = "4"
	} else {
		after.Prefabs[0].Vars["dir"] = "2"
	}
	accepted, err := document.Apply(model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: snapshot.DocumentID,
		ActorID: "01890f3e-7b5c-7abc-8def-0123456789ba", OperationID: model.OperationID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", snapshot.Revision+1)),
		BaseRevision: snapshot.Revision, BaseMapHash: hash, EnvironmentHash: snapshot.EnvironmentHash, Kind: model.OperationKindTileChange,
		Changes: []model.TileChange{{Coord: snapshot.Tiles[0].Coord, Before: snapshot.Tiles[0].State, After: after}},
	}, time.Unix(int64(snapshot.Revision)+1, 0).UTC())
	if err != nil {
		tb.Fatal(err)
	}
	return accepted
}

func verifyRecoveryHistory(tb testing.TB, storage *Store, expected *engine.Document) *engine.Document {
	tb.Helper()
	want := expected.Snapshot()
	state, err := storage.LoadRecovery(context.Background(), want.DocumentID)
	if err != nil {
		tb.Fatal(err)
	}
	restored, err := state.Restore()
	if err != nil {
		tb.Fatal(err)
	}
	if len(state.Operations) != int(want.Revision) || !reflect.DeepEqual(restored.Snapshot(), want) {
		tb.Fatal("durable recovery lost history or changed map content")
	}
	// Reapplying every stored ID to the independent reference must return the
	// exact original accepted record, including inverse metadata and timestamps.
	for _, stored := range state.Operations {
		original, err := expected.Apply(stored.Operation, time.Time{})
		if err != nil || !reflect.DeepEqual(original, stored) {
			tb.Fatalf("durable operation %s differs from reference: %v", stored.OperationID, err)
		}
	}
	return restored
}
