package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/app/ui/dialog"
)

func TestBackupFailureIsAnOpenRequestError(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{backupDir: blocked}
	_, err := a.backupMap(filepath.Join("..", "..", "testdata", "collaboration", "convergence.dmm"))
	var pathError *os.PathError
	if !errors.As(err, &pathError) {
		t.Fatalf("backup error = %v, want wrapped filesystem error", err)
	}
	// No layout or environment is installed: reaching map installation after
	// this recoverable failure would panic, rather than preserve the session.
	a.loadMap(filepath.Join("..", "..", "testdata", "collaboration", "convergence.dmm"), nil)
	dialog.Close(dialog.TypeInformation{Title: "Unable to back up map"})
	got, err := os.ReadFile(blocked)
	if err != nil || string(got) != "keep" {
		t.Fatalf("backup failure changed existing file: %q, %v", got, err)
	}
}

func TestBackupReadFailureAndSuccess(t *testing.T) {
	root := t.TempDir()
	a := &app{backupDir: filepath.Join(root, "backups")}
	if _, err := a.backupMap(filepath.Join(root, "missing.dmm")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read error = %v", err)
	}
	source := filepath.Join(root, "source.dmm")
	if err := os.WriteFile(source, []byte("opaque map bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	backup, err := a.backupMap(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(backup)
	if err != nil || string(got) != "opaque map bytes" {
		t.Fatalf("backup contents = %q, %v", got, err)
	}
}
