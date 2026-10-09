package mapadapter

import (
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
)

// A joined snapshot written with ExportTGMFile must reparse to the identical
// snapshot hash, including content the environment does not know.
func TestExportTGMFileReparsesToSameSnapshotHash(t *testing.T) {
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	input, err := dmmdata.New(filepath.Join("testdata", "unknown-types.dmm"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Import(func() *dmmap.Dmm {
		source, _ := dmmap.New(testEnvironment(input.Filepath), input, input.Filepath)
		return source
	}(), testDocumentID, testEnvironmentHash)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"joined.dmm", "joined.tgm"} {
		path := filepath.Join(t.TempDir(), name)
		if err := ExportTGMFile(snapshot, path); err != nil {
			t.Fatal(err)
		}
		reparsed, err := dmmdata.New(path)
		if err != nil {
			t.Fatalf("exported file is unreadable: %v", err)
		}
		if !reparsed.IsTgm {
			t.Fatal("export is not TGM")
		}
		roundtrip, err := Reimport(dmmFromData(reparsed), snapshot)
		if err != nil {
			t.Fatalf("reparsed export differs from the snapshot: %v", err)
		}
		assertSameHash(t, roundtrip, snapshot)
	}
}

func TestExportTGMFileDoesNotReplaceExistingFileWhenSnapshotIsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keep.dmm")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ExportTGMFile(snapshotWithoutDimensions(), path); err == nil {
		t.Fatal("invalid snapshot exported")
	}
	if data, _ := os.ReadFile(path); string(data) != "keep" {
		t.Fatalf("failed export replaced the existing file: %q", data)
	}
}

func snapshotWithoutDimensions() model.Snapshot {
	return model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: testDocumentID, EnvironmentHash: testEnvironmentHash, MaxX: model.MaxMapDimension + 1, MaxY: 1, MaxZ: 1}
}
