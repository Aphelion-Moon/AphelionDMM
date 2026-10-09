package wsmap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/app/command"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmvars"
)

func untitledEnvironment(t *testing.T, directory string) *dmenv.Dme {
	t.Helper()
	objects := make(map[string]*dmenv.Object)
	for _, objectPath := range []string{"/world", "/area/foo", "/turf/foo", "/obj/foo"} {
		variables := &dmvars.MutableVariables{}
		variables.Put("dir", "2")
		if objectPath == "/world" {
			variables.Put("area", "/area/foo")
			variables.Put("turf", "/turf/foo")
			variables.Put("icon_size", "32")
		}
		objects[objectPath] = &dmenv.Object{Path: objectPath, Vars: variables.ToImmutable()}
	}
	return &dmenv.Dme{RootDir: directory, Objects: objects}
}

// A joined, untitled workspace must route Save to a user-chosen path, write
// through the staged save path, become path-backed, and reparse to the same
// map contents. The pseudo path of the untitled document is never written.
func TestUntitledJoinedDocumentSavesAsAndReparsesToSameContents(t *testing.T) {
	if os.Getenv("APHELIONDMM_GL_TEST") != "1" {
		t.Skip("set APHELIONDMM_GL_TEST=1 for the real hidden-context workspace gate")
	}
	workspaceContext(t)
	imguiContext := imgui.CreateContext(nil)
	defer imguiContext.Destroy()

	directory := t.TempDir()
	source := filepath.Join(directory, "source.dmm")
	// Canonical stack order (obj, turf, area): Save normalizes order by design.
	original := []byte("\"a\" = (/turf/foo,/area/foo)\n\"b\" = (/obj/foo{dir = 4},/turf/foo,/area/foo)\n(1,1,1) = {\"\na\nb\n\"}\n")
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	environment := untitledEnvironment(t, directory)
	dmmap.PrefabStorage.Free()
	defer dmmap.PrefabStorage.Free()
	dmmap.Init(environment)
	defer dmmap.Free()
	hostData, err := dmmdata.New(source)
	if err != nil {
		t.Fatal(err)
	}
	hostMap, _ := dmmap.New(environment, hostData, "")
	environmentHash := strings.Repeat("e", 64)
	documentID, err := model.NewDocumentID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := mapadapter.Import(hostMap, documentID, environmentHash)
	if err != nil {
		t.Fatal(err)
	}

	// The joiner builds a fresh document from the received snapshot only.
	pseudo := filepath.Join(directory, ".aphelion-untitled", "Untitled-joined.dmm")
	joined := &dmmap.Dmm{Name: "Untitled-joined.dmm", Path: dmmap.DmmPath{Readable: "Untitled-joined.dmm", Absolute: pseudo}}
	if err := mapadapter.ApplyWithEnvironment(joined, snapshot, environment); err != nil {
		t.Fatal(err)
	}
	// The save pipeline needs an initial-state file; the app stages one from the snapshot.
	joined.Backup = filepath.Join(directory, "backup", "Untitled-joined.dmm")
	if err := mapadapter.ExportTGMFile(snapshot, joined.Backup); err != nil {
		t.Fatal(err)
	}
	app := &saveTestApp{environment: environment, commands: command.NewStorage(), jobs: make(chan func(), 16)}
	app.commands.SetStack(pseudo)
	ws := New(app, joined)
	ws.MarkUntitled()
	if !ws.Untitled() || !ws.HasUnsavedChanges() || !strings.Contains(ws.Name(), "Untitled") {
		t.Fatalf("untitled document presentation: untitled=%v unsaved=%v name=%q", ws.Untitled(), ws.HasUnsavedChanges(), ws.Name())
	}

	// Cancelling the file dialog saves nothing and keeps the document untitled.
	ws.pickSavePath = func(string) (string, error) { return "", errors.New("canceled") }
	if saveForTest(t, ws, app.jobs) {
		t.Fatal("canceled Save As reported success")
	}
	if !ws.Untitled() {
		t.Fatal("canceled Save As made the document path-backed")
	}
	if _, err := os.Lstat(pseudo); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untitled pseudo path was written: %v", err)
	}

	target := filepath.Join(directory, "joined-copy.dmm")
	ws.pickSavePath = func(string) (string, error) { return target, nil }
	if !saveForTest(t, ws, app.jobs) {
		t.Fatal("Save As for the untitled document failed")
	}
	if ws.Untitled() || ws.CommandStackId() != target || ws.HasUnsavedChanges() {
		t.Fatalf("after Save As: untitled=%v id=%q unsaved=%v", ws.Untitled(), ws.CommandStackId(), ws.HasUnsavedChanges())
	}
	if _, err := os.Lstat(pseudo); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untitled pseudo path was written: %v", err)
	}

	// Reparse: the file carries exactly the joined snapshot (same hash).
	data, err := dmmdata.New(target)
	if err != nil {
		t.Fatalf("saved map is unreadable: %v", err)
	}
	reparsed, _ := dmmap.New(environment, data, "")
	roundtrip, err := mapadapter.Reimport(reparsed, snapshot)
	if err != nil {
		t.Fatalf("saved map differs from the joined snapshot: %v", err)
	}
	wantHash, _ := snapshot.Hash()
	if gotHash, _ := roundtrip.Hash(); gotHash != wantHash {
		t.Fatalf("reparsed hash %s, want %s", gotHash, wantHash)
	}

	// Subsequent saves are ordinary path-backed saves: no dialog is shown.
	ws.pickSavePath = func(string) (string, error) { t.Fatal("path-backed save asked for a path"); return "", nil }
	app.commands.Push(command.Make("edit", func() {}, func() {}))
	if !saveForTest(t, ws, app.jobs) {
		t.Fatal("ordinary save after Save As failed")
	}
}
