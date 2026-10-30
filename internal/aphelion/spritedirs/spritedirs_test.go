package spritedirs

import (
	"errors"
	"image"
	"image/color"
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

// bedSheet draws a four-direction state with two looks: SOUTH and WEST share
// one frame, NORTH and EAST the other.
func bedSheetImage(looks [4]int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 2*4, 2))
	// Frame k of the strip is dir k in DMI order: SOUTH, NORTH, EAST, WEST.
	for k, look := range looks {
		img.Set(k*2+look, 0, color.NRGBA{R: 255, A: 255})
	}
	return img
}

func TestHorizontalTwinsComparesDirectionFrames(t *testing.T) {
	x := New(`C:\env`)
	x.read = func(string) (*sdmmparser.IconMetadata, error) {
		return &sdmmparser.IconMetadata{Width: 2, Height: 2, States: []*sdmmparser.IconState{
			{Name: "sheet", Dirs: 4, Frames: 1},
		}}, nil
	}
	cases := map[string]struct {
		looks        [4]int
		north, south int
		ok           bool
	}{
		"bedsheet":   {[4]int{0, 1, 1, 0}, 4, 8, true},
		"mirrored":   {[4]int{1, 0, 0, 1}, 4, 8, true},
		"swapped":    {[4]int{0, 1, 0, 1}, 8, 4, true},
		"four looks": {[4]int{0, 1, 1, 0}, 0, 0, false},
		"one look":   {[4]int{0, 0, 0, 0}, 0, 0, false},
	}
	for name, tc := range cases {
		x.icons = map[string]*entry{}
		looks := tc.looks
		x.image = func(string) (image.Image, error) {
			img := bedSheetImage(looks)
			if name == "four looks" { // Distinct pixels on a second row.
				img.(*image.NRGBA).Set(2*2, 1, color.NRGBA{G: 255, A: 255})
			}
			return img, nil
		}
		north, south, ok := x.HorizontalTwins("icons/obj/bedsheets.dmi", "sheet")
		if ok != tc.ok || ok && (north != tc.north || south != tc.south) {
			t.Fatalf("%s: got %d %d %v want %d %d %v", name, north, south, ok, tc.north, tc.south, tc.ok)
		}
	}
	if _, _, ok := x.HorizontalTwins("icons/obj/bedsheets.dmi", "absent"); ok {
		t.Fatal("unknown state reported")
	}
}
