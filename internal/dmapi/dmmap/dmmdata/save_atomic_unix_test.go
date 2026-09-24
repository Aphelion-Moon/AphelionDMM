//go:build !windows

package dmmdata

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAtomicNewFileDoesNotGrantGroupOrOtherAccess(t *testing.T) {
	target := filepath.Join(t.TempDir(), "new-map.dmm")
	if err := SaveAtomic(target, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "map")
		return err
	}, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions&0o077 != 0 {
		t.Fatalf("new map permissions are %04o; group and other access must remain disabled", permissions)
	}
}
