package ui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmvars"
)

func joinTestEnvironment(root string) *dmenv.Dme {
	objects := make(map[string]*dmenv.Object)
	for _, path := range []string{"/area/space", "/turf/open/space", "/turf/open/floor"} {
		objects[path] = &dmenv.Object{Path: path, Vars: (&dmvars.MutableVariables{}).ToImmutable()}
	}
	return &dmenv.Dme{RootDir: root, RootFile: filepath.Join(root, "game.dme"), Objects: objects}
}

func TestCheckJoinedEnvironmentRefusesDifferentHashWithBothLabelsAndHostDescriptor(t *testing.T) {
	snapshot := controllerSnapshot(t)
	host := &protocol.RepositoryDescriptor{DMEName: "game.dme", EnvironmentHash: snapshot.EnvironmentHash, GitBranch: "play-test", GitCommit: strings.Repeat("d", 40)}
	if err := CheckJoinedEnvironment(snapshot, snapshot.EnvironmentHash, "game.dme", host); err != nil {
		t.Fatal(err)
	}
	err := CheckJoinedEnvironment(snapshot, strings.Repeat("b", 64), "local.dme", host)
	var mismatch *EnvironmentMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("mismatch error = %v", err)
	}
	for _, want := range []string{"local.dme", strings.Repeat("b", 12), strings.Repeat("a", 12), "game.dme", "play-test", strings.Repeat("d", 12)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("mismatch dialog text lacks %q:\n%s", want, err)
		}
	}
	if strings.Contains(err.Error(), strings.Repeat("d", 40)) {
		t.Error("dialog printed the full commit instead of an abbreviation")
	}
	if err := CheckJoinedEnvironment(snapshot, "", "", nil); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("missing local environment accepted or host not described as unknown: %v", err)
	}
}

// Joins a real loopback session with no map open, edits, converges, and
// proves the Save As export reparses to the identical snapshot hash.
func TestJoinIntoNewDocumentThroughLoopbackSession(t *testing.T) {
	root := t.TempDir()
	environment := joinTestEnvironment(root)
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)

	snapshot := controllerSnapshot(t)
	stableID, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Tiles = []model.Tile{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: stableID, Path: "/turf/open/space", Vars: map[string]string{}}}}}}
	localHash := snapshot.EnvironmentHash

	embedded, err := server.StartEmbedded(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = embedded.Shutdown(context.Background()) })
	host := NewSessionClient(SessionClientConfig{HTTPTimeout: time.Second})
	invitation, err := host.Create(context.Background(), embedded.Endpoint(), embedded.TakeLaunchToken(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Join(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Leave(context.Background()) })
	created, err := host.CreateInvitation(context.Background(), InvitationRoleEditor, "Joiner")
	if err != nil {
		t.Fatal(err)
	}

	// The joiner has an environment but no map: the shipped join lifecycle
	// yields an executor and the snapshot a new document is built from.
	joiner := NewSessionClient(SessionClientConfig{HTTPTimeout: time.Second})
	controller := NewController(nil, joiner)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	execution, err := PrepareJoinedSession(ctx, controller, joiner, created)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = controller.Leave(context.Background()) })
	joined, err := execution.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckJoinedEnvironment(joined, localHash, "game.dme", nil); err != nil {
		t.Fatal(err)
	}
	if err := CheckJoinedEnvironment(joined, strings.Repeat("c", 64), "other.dme", nil); err == nil {
		t.Fatal("a different environment hash was accepted")
	}
	document := &dmmap.Dmm{Name: UntitledName(joined.DocumentID)}
	if err := mapadapter.ApplyWithEnvironment(document, joined, environment); err != nil {
		t.Fatal(err)
	}
	if got := document.Tiles[0].Instances()[0].Prefab().Path(); got != "/turf/open/space" {
		t.Fatalf("joined document tile = %q", got)
	}

	// The joiner edits through the same executor the document is attached to.
	operation := sessionReplacementOperation(t, joined, joiner.actorID, "/turf/open/floor")
	if _, err := execution.Execute(ctx, operation); err != nil {
		t.Fatal(err)
	}
	converged := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		hostSnapshot, err := host.NetworkExecutor().Snapshot(ctx)
		if err == nil && hostSnapshot.Tiles[0].State.Prefabs[0].Path == "/turf/open/floor" {
			converged = true
			break
		}
	}
	if !converged {
		t.Fatal("host never converged on the joiner's edit")
	}

	// Save As writes the authoritative state; reparsing reproduces its hash.
	final, err := execution.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "joined.dmm")
	if err := mapadapter.ExportTGMFile(final, target); err != nil {
		t.Fatal(err)
	}
	data, err := dmmdata.New(target)
	if err != nil {
		t.Fatal(err)
	}
	reparsed, _ := dmmap.New(environment, data, target)
	roundtrip, err := mapadapter.Reimport(reparsed, final)
	if err != nil {
		t.Fatalf("saved map differs from the session snapshot: %v", err)
	}
	wantHash, _ := final.Hash()
	if gotHash, _ := roundtrip.Hash(); gotHash != wantHash {
		t.Fatalf("reparsed hash %s, want %s", gotHash, wantHash)
	}
}
