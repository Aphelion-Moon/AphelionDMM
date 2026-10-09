package spritedirs

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"sdmm/third_party/sdmmparser"
)

func TestDirsReadsEachIconOnceFromAnyGoroutine(t *testing.T) {
	var reads atomic.Int32
	x := New(`C:\env`)
	x.read = func(path string) (*sdmmparser.IconMetadata, error) {
		reads.Add(1)
		if path != filepath.Join(`C:\env`, "icons", "turf", "walls.dmi") {
			return nil, errors.New("missing")
		}
		return &sdmmparser.IconMetadata{States: []*sdmmparser.IconState{{Name: "wall", Dirs: 1}, {Name: "pipe", Dirs: 4}, {Name: "wall", Dirs: 8}}}, nil
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if d, ok := x.Dirs("icons/turf/walls.dmi", "wall"); !ok || d != 1 {
				t.Errorf("wall = %d %v", d, ok)
			}
		}()
	}
	wg.Wait()
	if d, ok := x.Dirs("icons/turf/walls.dmi", "pipe"); !ok || d != 4 {
		t.Fatalf("pipe = %d %v", d, ok)
	}
	if _, ok := x.Dirs("icons/turf/walls.dmi", "absent"); ok {
		t.Fatal("unknown state reported")
	}
	if _, ok := x.Dirs("icons/missing.dmi", "x"); ok {
		t.Fatal("unreadable icon reported")
	}
	if reads.Load() != 2 {
		t.Fatalf("metadata read %d times, want once per icon", reads.Load())
	}
}
