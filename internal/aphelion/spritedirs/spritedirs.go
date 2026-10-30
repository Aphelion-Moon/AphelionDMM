// Package spritedirs answers how many directions a DMI icon state has. It
// reads DMI metadata and, only for HorizontalTwins, decoded PNG pixels; never
// GL state, so any goroutine may use it; rotation workers and the lint scanner
// both do.
package spritedirs

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"sdmm/third_party/sdmmparser"
)

// Index caches per-icon state directions for one environment root.
type Index struct {
	root  string
	read  func(path string) (*sdmmparser.IconMetadata, error)
	image func(path string) (image.Image, error)
	mu    sync.Mutex
	icons map[string]*entry
}

type entry struct {
	once     sync.Once
	states   map[string]int
	metadata *sdmmparser.IconMetadata
	ok       bool

	twinsOnce sync.Once
	twins     map[string][2]int
}

// New indexes icons below root, the environment's directory.
func New(root string) *Index {
	return &Index{root: root, read: sdmmparser.ParseIconMetadata, image: decodePNG, icons: map[string]*entry{}}
}

// Dirs returns the direction count of icon's state. ok is false when the
// icon cannot be read or has no such state; callers then keep their previous
// behaviour rather than guess. icon is the environment-relative path DM uses.
func (x *Index) Dirs(icon, state string) (dirs int, ok bool) {
	e := x.load(icon)
	if e == nil {
		return 0, false
	}
	dirs, ok = e.states[state]
	return dirs, ok
}

func (x *Index) load(icon string) *entry {
	if x == nil || icon == "" {
		return nil
	}
	x.mu.Lock()
	e := x.icons[icon]
	if e == nil {
		e = &entry{}
		x.icons[icon] = e
	}
	x.mu.Unlock()
	e.once.Do(func() {
		metadata, err := x.read(x.resolve(icon))
		if err != nil || metadata == nil {
			return
		}
		e.states = make(map[string]int, len(metadata.States))
		for _, s := range metadata.States {
			if _, seen := e.states[s.Name]; !seen { // DM uses the first duplicate
				e.states[s.Name] = s.Dirs
			}
		}
		e.metadata = metadata
		e.ok = true
	})
	if !e.ok {
		return nil
	}
	return e
}

// DMI direction order within one frame.
const (
	dirIndexSouth = iota
	dirIndexNorth
	dirIndexEast
	dirIndexWest
)

// HorizontalTwins reports states that draw four directions with only two
// side-facing looks, as beds and bedsheets do: NORTH and SOUTH each repeat,
// frame for frame, exactly one of the distinct EAST and WEST looks. north and
// south are the DM dir (EAST 4 or WEST 8) each repeats. Pixels are decoded
// once per icon and only the per-state answer is retained.
func (x *Index) HorizontalTwins(icon, state string) (north, south int, ok bool) {
	e := x.load(icon)
	if e == nil {
		return 0, 0, false
	}
	e.twinsOnce.Do(func() { e.twins = x.horizontalTwins(icon, e.metadata) })
	twins, ok := e.twins[state]
	return twins[0], twins[1], ok
}

func (x *Index) horizontalTwins(icon string, metadata *sdmmparser.IconMetadata) map[string][2]int {
	twins := map[string][2]int{}
	if x.image == nil || metadata == nil || metadata.Width <= 0 || metadata.Height <= 0 {
		return twins
	}
	candidate := false
	for _, s := range metadata.States {
		candidate = candidate || s.Dirs >= 4
	}
	if !candidate {
		return twins
	}
	img, err := x.image(x.resolve(icon))
	if err != nil || img == nil {
		return twins
	}
	columns := img.Bounds().Dx() / metadata.Width
	if columns <= 0 {
		return twins
	}
	seen := map[string]bool{}
	offset := 0
	for _, s := range metadata.States {
		frames := max(s.Frames, 1)
		start := offset
		offset += s.Dirs * frames
		if seen[s.Name] || s.Dirs < 4 {
			seen[s.Name] = true
			continue
		}
		seen[s.Name] = true
		same := func(a, b int) bool {
			for f := range frames {
				if !sameFrame(img, metadata.Width, metadata.Height, columns, start+f*s.Dirs+a, start+f*s.Dirs+b) {
					return false
				}
			}
			return true
		}
		if same(dirIndexEast, dirIndexWest) {
			continue // One look, or a symmetric sprite: nothing to flip.
		}
		twin := func(dir int) int {
			east, west := same(dir, dirIndexEast), same(dir, dirIndexWest)
			switch {
			case east && !west:
				return 4
			case west && !east:
				return 8
			}
			return 0
		}
		north, south := twin(dirIndexNorth), twin(dirIndexSouth)
		if north != 0 && south != 0 && north != south {
			twins[s.Name] = [2]int{north, south}
		}
	}
	return twins
}

func sameFrame(img image.Image, width, height, columns, a, b int) bool {
	origin := img.Bounds().Min
	ax, ay := origin.X+(a%columns)*width, origin.Y+(a/columns)*height
	bx, by := origin.X+(b%columns)*width, origin.Y+(b/columns)*height
	bounds := img.Bounds()
	if !image.Pt(ax+width-1, ay+height-1).In(bounds) || !image.Pt(bx+width-1, by+height-1).In(bounds) {
		return false
	}
	for y := range height {
		for x := range width {
			r1, g1, b1, a1 := img.At(ax+x, ay+y).RGBA()
			r2, g2, b2, a2 := img.At(bx+x, by+y).RGBA()
			if a1 == 0 && a2 == 0 {
				continue
			}
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				return false
			}
		}
	}
	return true
}

func decodePNG(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return png.Decode(file)
}

var active atomic.Pointer[Index]

// Activate makes x the loaded environment's index; nil clears it.
func Activate(x *Index) { active.Store(x) }

// Active is the loaded environment's index, or nil.
func Active() *Index { return active.Load() }

// ActiveDirs is Dirs on the active index; it reports unknown without one.
func ActiveDirs(icon, state string) (int, bool) { return Active().Dirs(icon, state) }

// ActiveHorizontalTwins is HorizontalTwins on the active index.
func ActiveHorizontalTwins(icon, state string) (int, int, bool) {
	return Active().HorizontalTwins(icon, state)
}

func (x *Index) resolve(icon string) string {
	icon = filepath.FromSlash(strings.Trim(icon, "'\""))
	if filepath.IsAbs(icon) {
		return icon
	}
	return filepath.Join(x.root, icon)
}
