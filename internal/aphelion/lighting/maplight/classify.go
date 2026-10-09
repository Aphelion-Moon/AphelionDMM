package maplight

import (
	"strings"

	"sdmm/internal/aphelion/lighting"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
)

// skipKey identifies one aggregated skipped-atom row.
type skipKey struct {
	Path       string
	Reason     string
	Unparsable bool
}

const (
	reasonLightOff     = "light_on is false"
	reasonOverlayOff   = "overlay light excluded by preference"
	reasonOpacity      = "unparsable opacity (treated as transparent)"
	reasonBaseLighting = "unparsable base lighting (treated as none)"
	maxClassifierSize  = 1 << 16
)

// atomInfo is everything the preview needs from one prefab. Prefabs are
// immutable and shared (dmmap.PrefabStorage), so one classification serves every
// instance of the prefab.
type atomInfo struct {
	area, space bool
	opaque      bool
	base        lighting.AreaBase
	static      bool
	emits       bool
	source      lighting.Source // X and Y are filled per instance
	skips       []skipKey
}

// Classifier caches per-prefab classification for one environment and
// settings. UI thread only; build a new one when either changes.
type Classifier struct {
	set      Settings
	profiles []lighting.Profile
	cache    map[*dmmprefab.Prefab]*atomInfo
}

// NewClassifier returns a classifier using the built-in profiles (N13).
func NewClassifier(set Settings) *Classifier {
	return &Classifier{set: set, profiles: lighting.DefaultProfiles(), cache: make(map[*dmmprefab.Prefab]*atomInfo)}
}

func (c *Classifier) size() int { return len(c.cache) }

func (c *Classifier) info(p *dmmprefab.Prefab) *atomInfo {
	if info, ok := c.cache[p]; ok {
		return info
	}
	if len(c.cache) >= maxClassifierSize {
		c.cache = make(map[*dmmprefab.Prefab]*atomInfo)
	}
	info := c.classify(p)
	c.cache[p] = info
	return info
}

func pathIs(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

func (c *Classifier) classify(p *dmmprefab.Prefab) *atomInfo {
	info := &atomInfo{static: true, base: lighting.AreaBase{Color: lighting.RGB{R: 1, G: 1, B: 1}}}
	path := p.Path()
	var get lighting.VarLookup
	if vars := p.Vars(); vars != nil {
		get = vars.Value
	}
	if pathIs(path, "/area") {
		info.area = true
		base, ok := lighting.ParseAreaBase(get)
		if ok {
			info.base = base
		} else {
			info.skips = append(info.skips, skipKey{Path: path, Reason: reasonBaseLighting, Unparsable: true})
		}
		info.static = lighting.StaticLighting(get)
		return info
	}
	opaque, bad := lighting.IsOpaque(get)
	info.opaque = opaque && !bad
	if bad {
		info.skips = append(info.skips, skipKey{Path: path, Reason: reasonOpacity, Unparsable: true})
	}
	if pathIs(path, "/turf/open/space") {
		// Space emits through derived starlight, not through its own light vars
		// (light_on is FALSE until the game enables it, space.dm:64-69).
		info.space = true
		return info
	}
	ex := lighting.ExtractSource(lighting.Atom{Path: path, Var: get}, c.profiles)
	switch {
	case ex.Emits:
		overlay := ex.Source.System == lighting.SystemOverlay || ex.Source.System == lighting.SystemOverlayDirectional || ex.Source.System == lighting.SystemOverlayBeam
		if overlay && !c.set.OverlayLights {
			info.skips = append(info.skips, skipKey{Path: path, Reason: reasonOverlayOff})
		} else {
			info.emits = true
			info.source = ex.Source
		}
	case ex.Unparsable:
		info.skips = append(info.skips, skipKey{Path: path, Reason: ex.Reason, Unparsable: true})
	case ex.Reason == reasonLightOff:
		// A configured light that is switched off is worth listing. Atoms with
		// no light at all are the common case and are not reported.
		info.skips = append(info.skips, skipKey{Path: path, Reason: ex.Reason})
	}
	return info
}
