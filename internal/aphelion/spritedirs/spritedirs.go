// Package spritedirs answers how many directions a DMI icon state has. It
// reads only DMI metadata, never pixels or GL state, so any goroutine may use
// it; rotation workers and the lint scanner both do.
package spritedirs

import (
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
	mu    sync.Mutex
	icons map[string]*entry
}

type entry struct {
	once   sync.Once
	states map[string]int
	ok     bool
}

// New indexes icons below root, the environment's directory.
func New(root string) *Index {
	return &Index{root: root, read: sdmmparser.ParseIconMetadata, icons: map[string]*entry{}}
}

// Dirs returns the direction count of icon's state. ok is false when the
// icon cannot be read or has no such state; callers then keep their previous
// behaviour rather than guess. icon is the environment-relative path DM uses.
func (x *Index) Dirs(icon, state string) (dirs int, ok bool) {
	if x == nil || icon == "" {
		return 0, false
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
		e.ok = true
	})
	if !e.ok {
		return 0, false
	}
	dirs, ok = e.states[state]
	return dirs, ok
}

var active atomic.Pointer[Index]

// Activate makes x the loaded environment's index; nil clears it.
func Activate(x *Index) { active.Store(x) }

// Active is the loaded environment's index, or nil.
func Active() *Index { return active.Load() }

// ActiveDirs is Dirs on the active index; it reports unknown without one.
func ActiveDirs(icon, state string) (int, bool) { return Active().Dirs(icon, state) }

func (x *Index) resolve(icon string) string {
	icon = filepath.FromSlash(strings.Trim(icon, "'\""))
	if filepath.IsAbs(icon) {
		return icon
	}
	return filepath.Join(x.root, icon)
}
