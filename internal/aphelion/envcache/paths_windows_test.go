package envcache

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPathValidatorAcceptsShortDirectoryNames(t *testing.T) {
	root := t.TempDir()
	name, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	n, err := windows.GetShortPathName(name, &buffer[0], uint32(len(buffer)))
	if err != nil {
		t.Fatal(err)
	}
	short := windows.UTF16ToString(buffer[:n])
	if sameLexical(short, root) {
		t.Skip("volume does not provide short directory names")
	}
	path := filepath.Join(short, "fixture.dme")
	if err := os.WriteFile(path, []byte("/obj/example"), 0600); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := newPathValidator(cwd, short).check(path, path, false); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateStorageAcceptsProfileDirectory(t *testing.T) {
	if err := privateStorage(privateTestDirectory(t), true); err != nil {
		t.Fatal(err)
	}
}
