package sqlite

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

// The ordinary regression stays short. Set APHELION_SQLITE_CRASH_HISTORY=10000
// for sequential durable ingestion beyond the recovery cache's history limit.
func TestSQLiteAbruptProcessRecovery(t *testing.T) {
	history := 32
	if configured := os.Getenv("APHELION_SQLITE_CRASH_HISTORY"); configured != "" {
		parsed, err := strconv.Atoi(configured)
		if err != nil || parsed < 1 || parsed > 10000 {
			t.Fatal("APHELION_SQLITE_CRASH_HISTORY must be between 1 and 10000")
		}
		history = parsed
	}
	path := filepath.Join(t.TempDir(), "crash.db")
	initial := recoveryHistorySnapshot(0)
	reference, err := engine.NewDocument(initial)
	if err != nil {
		t.Fatal(err)
	}
	var inverted []model.OperationID
	for trial := range 5 {
		count := 1
		if trial == 0 {
			count = history
		}
		var last model.AcceptedOperation
		for range count {
			last = nextRecoveryEdit(t, reference)
		}
		ready := killSQLiteWriterAfterAppend(t, sqliteCrashConfig{Path: path, Create: trial == 0, Appends: count})
		want := reference.Snapshot()
		hash, err := want.Hash()
		if err != nil || ready.Revision != want.Revision || ready.Hash != hash || ready.WALBytes <= 32 {
			t.Fatalf("child did not reach the expected durable state with an open WAL: %+v: %v", ready, err)
		}
		storage, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = storage.Close() })
		var integrity string
		if err := storage.database.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
			t.Fatalf("database integrity after process termination: %q: %v", integrity, err)
		}
		restored := verifyRecoveryHistory(t, storage, reference)
		for _, target := range inverted {
			if _, err := restored.BuildInverse(last.ActorID, target, "01890f3e-7b5c-7abc-8def-eeeeeeeeeeee"); engine.CodeOf(err) != engine.CodeAlreadyInverted {
				t.Fatalf("process restart forgot an already-inverted operation: %v", err)
			}
		}
		// Retrying the original accepted identity after the lost process must
		// preserve its timestamp/revision and must not apply it a second time.
		if err := storage.Append(context.Background(), last); err != nil {
			t.Fatal(err)
		}
		if _, err := restored.BuildInverse("01890f3e-7b5c-7abc-8def-0123456789bb", last.OperationID, "01890f3e-7b5c-7abc-8def-eeeeeeeeeeee"); engine.CodeOf(err) != engine.CodeActorMismatch {
			t.Fatalf("process restart lost inverse actor ownership: %v", err)
		}
		inverse, err := restored.BuildInverse(last.ActorID, last.OperationID, model.OperationID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-ffffff%06x", trial)))
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := reference.Apply(inverse, time.Unix(20000+int64(trial), 0).UTC())
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := storage.Append(context.Background(), accepted); err != nil {
				t.Fatal(err)
			}
		}
		inverted = append(inverted, last.OperationID)
		verifyRecoveryHistory(t, storage, reference)
		if err := storage.Close(); err != nil {
			t.Fatal(err)
		}
		t.Logf("trial=%d killed_after_revision=%d WAL_bytes=%d recovered_hash=%s inverse_revision=%d", trial+1, ready.Revision, ready.WALBytes, ready.Hash, accepted.Revision)
	}
	// The untouched second tile can still merge against the original authentic
	// revision-zero base after every crash, checkpoint and inverse.
	baseHash, err := initial.Hash()
	if err != nil {
		t.Fatal(err)
	}
	change := model.TileChange{Coord: initial.Tiles[1].Coord, Before: initial.Tiles[1].State, After: model.CloneTileState(initial.Tiles[1].State)}
	change.After.Prefabs[0].Vars["dir"] = "4"
	accepted, err := reference.Apply(model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: initial.DocumentID,
		ActorID: "01890f3e-7b5c-7abc-8def-0123456789ba", OperationID: "01890f3e-7b5c-7abc-8def-eeeeeeeeeeee",
		BaseRevision: 0, BaseMapHash: baseHash, EnvironmentHash: initial.EnvironmentHash, Kind: model.OperationKindTileChange, Changes: []model.TileChange{change},
	}, time.Unix(30000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	storage, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = storage.Close() }()
	if err := storage.Append(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}
	verifyRecoveryHistory(t, storage, reference)
	t.Logf("all five abrupt terminations preserved exact history, inverse ownership and original base; final_revision=%d SQLite=%s", reference.Snapshot().Revision, storage.Version())
}

type sqliteCrashConfig struct {
	Path    string
	Create  bool
	Appends int
}

type sqliteCrashObservation struct {
	Kind     string
	Revision model.Revision
	Hash     string
	WALBytes int64
}

func killSQLiteWriterAfterAppend(t *testing.T, config sqliteCrashConfig) sqliteCrashObservation {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestSQLiteCrashWriterProcess$", "-test.timeout=20m")
	command.Env = append(os.Environ(), "APHELION_SQLITE_CRASH_CHILD="+string(encoded), "GORACE="+os.Getenv("GORACE")+" halt_on_error=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "sqlite-crash:") {
			t.Log(line)
			continue
		}
		var observed sqliteCrashObservation
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "sqlite-crash:")), &observed); err != nil {
			t.Fatal(err)
		}
		if observed.Kind == "progress" {
			t.Logf("child durably appended revision %d", observed.Revision)
			continue
		}
		if observed.Kind != "ready" {
			t.Fatalf("unexpected child observation: %s", line)
		}
		if err := command.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		if err := command.Wait(); err == nil || command.ProcessState == nil || command.ProcessState.Success() || ctx.Err() != nil {
			t.Fatalf("child was not abruptly terminated at the ready boundary: %v: %s", err, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("writer emitted unexpected diagnostics before termination: %s", stderr.String())
		}
		return observed
	}
	waitErr := command.Wait()
	t.Fatalf("child ended before ready: scan=%v wait=%v stderr=%s", scanner.Err(), waitErr, stderr.String())
	return sqliteCrashObservation{}
}

// Only the isolated subprocess runs this helper. It waits without closing the
// store after Append returns so the parent kills an actual live WAL connection.
func TestSQLiteCrashWriterProcess(t *testing.T) {
	encoded := os.Getenv("APHELION_SQLITE_CRASH_CHILD")
	if encoded == "" {
		t.Skip("subprocess helper")
	}
	var config sqliteCrashConfig
	if err := json.Unmarshal([]byte(encoded), &config); err != nil {
		t.Fatal(err)
	}
	storage, err := Open(config.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = storage.Close() }()
	initial := recoveryHistorySnapshot(0)
	if config.Create {
		if err := storage.Create(context.Background(), initial); err != nil {
			t.Fatal(err)
		}
	}
	state, err := storage.LoadRecovery(context.Background(), initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := state.Restore()
	if err != nil {
		t.Fatal(err)
	}
	for index := range config.Appends {
		if index == config.Appends-1 {
			if err := storage.SaveSnapshot(context.Background(), document.Snapshot()); err != nil {
				t.Fatal(err)
			}
		}
		accepted := nextRecoveryEdit(t, document)
		if err := storage.Append(context.Background(), accepted); err != nil {
			t.Fatal(err)
		}
		if accepted.Revision%1000 == 0 {
			emitSQLiteCrashObservation(t, sqliteCrashObservation{Kind: "progress", Revision: accepted.Revision})
		}
	}
	snapshot := document.Snapshot()
	hash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.Stat(config.Path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	emitSQLiteCrashObservation(t, sqliteCrashObservation{Kind: "ready", Revision: snapshot.Revision, Hash: hash, WALBytes: wal.Size()})
	<-time.After(20 * time.Minute)
	t.Fatal("parent did not terminate the ready writer")
}

func emitSQLiteCrashObservation(t *testing.T, observed sqliteCrashObservation) {
	t.Helper()
	encoded, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Printf("sqlite-crash:%s\n", encoded); err != nil {
		t.Fatal(err)
	}
}
