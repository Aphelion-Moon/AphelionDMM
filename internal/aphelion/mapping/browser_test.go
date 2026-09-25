package mapping

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectDiscoveryDoesNotParseOrAdoptSources(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one.dmm", "two.tgm", "unrelated.dm"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("deliberately invalid source"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := ScanProject(context.Background(), root)
	if err != nil || len(paths) != 2 {
		t.Fatal(paths, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ScanProject(ctx, root); err == nil {
		t.Fatal("cancelled scan continued")
	}
}
