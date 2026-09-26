package mapindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMembershipAndDiscoveryPreserveProjectMapRules(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"inside.dmm", "other.tgm", "caps.DMM"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte{}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := Scan(context.Background(), root)
	if err != nil || len(paths) != 1 || paths[0] != filepath.Join(root, "inside.dmm") {
		t.Fatal("discovery changed DMM membership", paths, err)
	}
	for _, test := range []struct {
		path string
		want bool
	}{{filepath.Join(root, "inside.dmm"), true}, {filepath.Join(root, "nested", "new.dmm"), true}, {filepath.Join(root, "other.tgm"), false}, {filepath.Join(root+"-sibling", "outside.dmm"), false}, {filepath.Join(root, "..", "outside.dmm"), false}} {
		if Contains(root, test.path) != test.want {
			t.Fatal("wrong root containment", test)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, root); err == nil {
		t.Fatal("cancelled scan completed")
	}
}
