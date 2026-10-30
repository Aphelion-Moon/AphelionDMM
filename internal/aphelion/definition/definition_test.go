package definition

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSourcePathStaysInsideEnvironmentRoot(t *testing.T) {
	root := t.TempDir()
	got, err := SourcePath(root, "code/game/area/areas.dm")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "code", "game", "area", "areas.dm"); got != want {
		t.Fatalf("SourcePath = %q, want %q", got, want)
	}
	if got, err := SourcePath(root, "code/../maps/x.dm"); err != nil || got != filepath.Join(root, "maps", "x.dm") {
		t.Fatalf("a path that stays inside the root should resolve: %q, %v", got, err)
	}
}

func TestSourcePathRejectsEscapesAndMissingInput(t *testing.T) {
	root := t.TempDir()
	for _, file := range []string{"", "../outside.dm", "code/../../outside.dm", filepath.Join(root, "abs.dm"), "/etc/passwd"} {
		if _, err := SourcePath(root, file); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("SourcePath(%q) error = %v, want ErrOutsideRoot", file, err)
		}
	}
	if _, err := SourcePath("", "code/a.dm"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("an empty root must be refused, got %v", err)
	}
}
