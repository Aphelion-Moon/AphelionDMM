// Package maplight adapts a displayed dmmap.Dmm level to the pure lighting
// model and schedules its computation. It is editor-only derived state: it never
// writes map data, history, collaboration messages or hashes.
//
// Threading: the Classifier, level capture and Controller are UI-thread only
// (they read live prefab pointers). The worker receives an immutable snapshot
// (a cloned lighting.Level) and publishes a new result that the UI thread polls.
package maplight

import (
	"sync"

	"sdmm/internal/aphelion/lighting"
)

const (
	// MaxRange is the approved cap for light_range (decision N7).
	MaxRange = 16
	// SourceCap bounds the explicit source list handed to the model.
	SourceCap = 5000
	// DefaultDarkness is the approved default darkness strength in percent (N2).
	DefaultDarkness = 70
	// Cutoff is the game's lighting plane floor (N8).
	Cutoff = 0.10
)

// Settings are the user preferences for the preview.
type Settings struct {
	Enabled       bool
	Darkness      int  // 0..100, how much of the light multiplies the map
	Starlight     bool // derive starlight from space tiles (N6)
	OverlayLights bool // approximate OVERLAY_LIGHT* sources (N5)
	ShowSources   bool // draw markers for emitters
}

// DefaultSettings returns the approved defaults. The preview itself is off.
func DefaultSettings() Settings {
	return Settings{Darkness: DefaultDarkness, Starlight: true, OverlayLights: true}
}

// Normalize clamps out-of-range values from a hand-edited config.
func (s Settings) Normalize() Settings {
	s.Darkness = max(0, min(100, s.Darkness))
	return s
}

// Strength is the darkness strength s in 0..1: final = 1 - s*(1 - light).
func (s Settings) Strength() float32 { return float32(s.Normalize().Darkness) / 100 }

func (s Settings) options() lighting.Options {
	return lighting.Options{MaxRange: MaxRange, Cutoff: Cutoff, IncludeOverlayLights: s.OverlayLights}
}

func (s Settings) starlight() lighting.Starlight {
	st := lighting.DefaultStarlight()
	st.Enabled = s.Starlight
	return st
}

// computeKey is the part of Settings that changes the computed light.
type computeKey struct{ starlight, overlay bool }

func (s Settings) computeKey() computeKey { return computeKey{s.Starlight, s.OverlayLights} }

var store struct {
	mu      sync.Mutex
	current Settings
	persist func(enabled bool)
}

func init() { store.current = DefaultSettings() }

// Current returns the live settings.
func Current() Settings {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.current
}

// Apply replaces the live settings (preferences changed).
func Apply(s Settings) {
	store.mu.Lock()
	store.current = s.Normalize()
	store.mu.Unlock()
}

// SetEnabled toggles the preview from the View menu and persists the choice
// through the hook installed by SetPersist. It does not touch any document.
func SetEnabled(enabled bool) {
	store.mu.Lock()
	store.current.Enabled = enabled
	persist := store.persist
	store.mu.Unlock()
	if persist != nil {
		persist(enabled)
	}
}

// SetPersist installs the callback that stores the enabled flag in preferences.
func SetPersist(fn func(enabled bool)) {
	store.mu.Lock()
	store.persist = fn
	store.mu.Unlock()
}
