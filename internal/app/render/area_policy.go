package render

import (
	"strings"
	"sync"

	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmmap"
)

// DefaultAreaAlpha is the default translucency applied to drawn areas.
const DefaultAreaAlpha float32 = 0.35

// fallbackBaseArea is used when the environment does not expose world.area.
const fallbackBaseArea = "/area/space"

// AreaPolicy is a render-only presentation policy for /area units. It never
// touches map data; areas keep their sort position (topmost) and are drawn as
// a translucent overlay so they cannot hide turfs.
type AreaPolicy struct {
	// Alpha multiplies an area unit's own alpha. Clamped to 0..1.
	Alpha float32
	// HideBase hides the environment's base area.
	HideBase bool
	// BaseArea overrides the base area path. Empty resolves world.area, then
	// falls back to /area/space.
	BaseArea string
}

// DefaultAreaPolicy returns the shipped defaults.
func DefaultAreaPolicy() AreaPolicy {
	return AreaPolicy{Alpha: DefaultAreaAlpha, HideBase: true}
}

func isAreaPath(path string) bool {
	return path == "/area" || strings.HasPrefix(path, "/area/")
}

func (p AreaPolicy) baseAreaPath() string {
	if p.BaseArea != "" {
		return p.BaseArea
	}
	if dmmap.BaseArea != nil {
		if path := dmmap.BaseArea.Path(); path != "" {
			return path
		}
	}
	return fallbackBaseArea
}

// Apply returns the drawn alpha and visibility of a unit with the given prefab
// path and own alpha. Non-area units are returned unchanged.
func (p AreaPolicy) Apply(path string, alpha float32) (float32, bool) {
	if !isAreaPath(path) {
		return alpha, true
	}
	if p.HideBase && path == p.baseAreaPath() {
		return 0, false
	}
	factor := p.Alpha
	if factor < 0 {
		factor = 0
	} else if factor > 1 {
		factor = 1
	}
	return alpha * factor, true
}

// ApplyUnit applies the policy to a unit.
func (p AreaPolicy) ApplyUnit(u unit.Unit) (float32, bool) {
	// Fast path: non-area units are never altered.
	path := u.Instance().Prefab().Path()
	if !isAreaPath(path) {
		return u.A(), true
	}
	return p.Apply(path, u.A())
}

var areaPolicyState = struct {
	sync.RWMutex
	policy   AreaPolicy
	revision uint64
}{policy: DefaultAreaPolicy()}

// SetAreaPolicy installs the policy and bumps the revision (invalidating
// retained submissions) only when it changed.
func SetAreaPolicy(p AreaPolicy) {
	areaPolicyState.Lock()
	defer areaPolicyState.Unlock()
	if areaPolicyState.policy == p {
		return
	}
	areaPolicyState.policy = p
	areaPolicyState.revision++
}

// CurrentAreaPolicy returns the installed policy.
func CurrentAreaPolicy() AreaPolicy {
	areaPolicyState.RLock()
	defer areaPolicyState.RUnlock()
	return areaPolicyState.policy
}

// AreaPolicyRevision changes whenever the installed policy changes.
func AreaPolicyRevision() uint64 {
	areaPolicyState.RLock()
	defer areaPolicyState.RUnlock()
	return areaPolicyState.revision
}
