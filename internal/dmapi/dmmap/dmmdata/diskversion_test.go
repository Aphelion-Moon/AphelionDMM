package dmmdata

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/aphelion/diskversion"
)

func TestNewRetainsTheVersionOfBytesConsumedByTheParser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map.dmm")
	loaded := []byte("\"a\" = (/area/foo,/turf/foo)\n(1,1,1) = {\"a\"}\n")
	if err := os.WriteFile(path, loaded, 0600); err != nil {
		t.Fatal(err)
	}
	data, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if !data.DiskState.Exists() {
		t.Fatal("parsed map did not retain a disk version")
	}

	external := []byte("\"b\" = (/area/bar,/turf/bar)\n(1,1,1) = {\"b\"}\n")
	if err := os.WriteFile(path, external, 0600); err != nil {
		t.Fatal(err)
	}
	if err := data.DiskState.Check(path); !errors.Is(err, diskversion.ErrConflict) {
		t.Fatalf("loaded disk version accepted a later rewrite: %v", err)
	}
}
