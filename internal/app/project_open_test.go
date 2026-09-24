package app

import (
	"bytes"
	"os"
	"path/filepath"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"testing"
)

func TestOwnedMapOpenCopiesParsedSourceAndRetainsUnknownData(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "map.dmm")
	input := []byte("// opaque original comment\r\n\"a\" = (/area/base,/turf/base,/obj/unknown{custom = \"keep\"})\r\n(1,1,1) = {\"\na\n\"}\r\n")
	if err := os.WriteFile(source, input, 0600); err != nil {
		t.Fatal(err)
	}
	result := prepareMapOpen(source, filepath.Join(root, "backup"), "project")
	if result.err != nil {
		t.Fatal(result.err)
	}
	backup, err := os.ReadFile(result.backup)
	if err != nil || !bytes.Equal(backup, input) {
		t.Fatal("backup does not contain exact parsed bytes", err)
	}
	if err := result.data.DiskState.Check(source); err != nil {
		t.Fatal("load lost external change guard", err)
	}
	for _, prefabs := range result.data.Dictionary {
		if len(prefabs) != 3 || prefabs[2].Path() != "/obj/unknown" || prefabs[2].Vars().ValueV("custom", "") != "\"keep\"" {
			t.Fatal("open changed unknown data")
		}
	}
	second := prepareMapOpen(source, filepath.Join(root, "backup"), "project")
	if second.err != nil || second.backup == result.backup {
		t.Fatal("repeated open reused a backup filename", second.err)
	}
}

func TestMapOpenLateResultCannotInstallAfterCancellationOrProjectChange(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		old := &dmenv.Dme{}
		a := &app{loadedEnvironment: old}
		req := &mapOpenRequest{environment: old, results: make(chan mapOpenResult, 1)}
		a.mapOpenActive = req
		if cancel {
			a.cancelMapOpens()
		} else {
			a.loadedEnvironment = &dmenv.Dme{}
		}
		req.results <- mapOpenResult{data: &dmmdata.DmmData{}}
		a.processMapOpen() // A stale installation would dereference the absent layout.
		if a.mapOpenActive != nil {
			t.Fatal("late request retained worker ownership")
		}
	}
}

func TestOwnedMapOpenFailureRemovesPartialBackup(t *testing.T) {
	root := t.TempDir()
	result := prepareMapOpen(filepath.Join(root, "missing.dmm"), filepath.Join(root, "backup"), "project")
	if result.err == nil || result.backup != "" {
		t.Fatal("failed open retained backup or lost error")
	}
	entries, err := os.ReadDir(filepath.Join(root, "backup", "project", "missing.dmm"))
	if err != nil || len(entries) != 0 {
		t.Fatal("partial backup was retained", err)
	}
}
