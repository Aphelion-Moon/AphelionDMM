package diskversion

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStateCheckDetectsChangedDeletedAndReplacedTargets(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "map.dmm")
	original := []byte("original map")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	state, err := Capture(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Check(path); err != nil {
		t.Fatalf("unchanged target did not match: %v", err)
	}

	if err := os.WriteFile(path, []byte("external map"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := state.Check(path); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed target error = %v, want ErrConflict", err)
	}

	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	state, err = Capture(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(directory, "replacement.dmm")
	if err := os.WriteFile(replacement, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := state.Check(path); !errors.Is(err, ErrConflict) {
		t.Fatalf("same-content replacement error = %v, want ErrConflict", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := state.Check(path); !errors.Is(err, ErrConflict) {
		t.Fatalf("deleted target error = %v, want ErrConflict", err)
	}
}

func TestAbsentStateDetectsAFileCreatedAfterObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-map.dmm")
	state := Absent()
	if err := state.Check(path); err != nil {
		t.Fatalf("missing target did not match: %v", err)
	}
	if err := os.WriteFile(path, []byte("external map"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := state.Check(path); !errors.Is(err, ErrConflict) {
		t.Fatalf("created target error = %v, want ErrConflict", err)
	}
}

func TestStateCheckHonorsWindowsPathCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path-case behavior is covered on Windows")
	}
	path := filepath.Join(t.TempDir(), "Map.dmm")
	if err := os.WriteFile(path, []byte("map"), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := Capture(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Check(strings.ToUpper(path)); err != nil {
		t.Fatalf("case-only path spelling did not identify the same Windows file: %v", err)
	}
}
