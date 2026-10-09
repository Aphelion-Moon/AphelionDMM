package lighting

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// VarLookup returns the DM source text of a variable on an atom, including
// inheritance from its type chain, as the environment parser exposes it
// (dmvars.Variables.Value): numbers as "3", strings quoted as "\"#ffaa00\"",
// null as "null". ok is false when the variable is not defined anywhere.
//
// The Rust parser expands preprocessor macros and evaluates constants before
// values reach Go, so COLOR_WHITE arrives as "#FFFFFF" and NORTH as "1".
// Anything still symbolic is reported as unparsable rather than guessed.
type VarLookup func(name string) (string, bool)

// Atom is one placed atom as seen by the extractor.
type Atom struct {
	Path string
	X, Y int
	Var  VarLookup
}

// Extraction is the outcome of looking at one atom.
type Extraction struct {
	Source Source
	// Emits is true when Source is valid.
	Emits bool
	// Unparsable is true when a light variable could not be understood. The
	// atom is skipped, never guessed. Reason is always set when Emits is false.
	Unparsable bool
	Reason     string
}

// Profile derives a light for types that configure it at runtime rather than
// through light_range/light_power/light_color (for example wall light
// fixtures, light.dm:24-28,247-306).
type Profile struct {
	PathPrefix   string // matched on a path boundary
	RangeVar     string
	RangeDefault float64
	PowerVar     string
	PowerDefault float64
	ColorVar     string
	ColorDefault string // DM text, e.g. "\"#f3fffa\""
	AngleDefault float64
	// FacingFromDir points the light opposite the atom's dir and shifts the
	// emitter by ShiftTiles along dir (light.dm:114,166-170).
	FacingFromDir bool
	ShiftTiles    float64
	// OverrideColorVar, when set and not null, replaces the colour
	// (light.dm update(): if(color) color_set = color).
	OverrideColorVar string
	// StatusVar holds the bulb state; the fixture is dark unless it equals
	// StatusOK (LIGHT_OK, __DEFINES/lights.dm:4-7). StatusOff is what
	// LightSwitch writes to turn it off (LIGHT_EMPTY), and BaseStateVar names
	// the icon_state base whose "-empty" state shows the empty fitting.
	StatusVar    string
	StatusOK     int
	StatusOff    int
	BaseStateVar string
}

// DefaultProfiles returns the built-in profiles: /obj/machinery/light
// (light.dm:14,24-28,114,166-170,241-250; LIGHT_COLOR_DEFAULT colors.dm:254).
// light_range, light_power and light_color on a fixture are overwritten by
// update() in game, so they are not read here.
func DefaultProfiles() []Profile {
	return []Profile{{
		PathPrefix: "/obj/machinery/light",
		RangeVar:   "brightness", RangeDefault: 8,
		PowerVar: "bulb_power", PowerDefault: 1,
		ColorVar: "bulb_colour", ColorDefault: `"#f3fffa"`,
		AngleDefault:     170,
		FacingFromDir:    true,
		ShiftTiles:       0.5,
		OverrideColorVar: "color",
		StatusVar:        "status", StatusOK: 0, StatusOff: 1,
		BaseStateVar: "base_state",
	}}
}

// ReasonNoBulb is the skip reason of a fixture whose status is not LIGHT_OK.
const ReasonNoBulb = "fixture has no working bulb (status)"

// IsFixture reports whether path is configured by a profile (a wall light).
func IsFixture(path string, profiles []Profile) bool {
	return profileFor(path, profiles) != nil
}

func profileFor(path string, profiles []Profile) *Profile {
	for i := range profiles {
		if pathHasPrefix(path, profiles[i].PathPrefix) {
			return &profiles[i]
		}
	}
	return nil
}

func pathHasPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// ExtractSource reads the light variables of a placed atom. Defaults follow
// the /atom declarations (_atom.dm:69-92). The first matching profile, if
// any, replaces the range/power/colour/direction inputs.
func ExtractSource(a Atom, profiles []Profile) Extraction {
	get := a.Var
	if get == nil {
		get = func(string) (string, bool) { return "", false }
	}
	prof := profileFor(a.Path, profiles)
	skip := func(format string, args ...any) Extraction {
		return Extraction{Reason: fmt.Sprintf(format, args...)}
	}
	bad := func(format string, args ...any) Extraction {
		return Extraction{Unparsable: true, Reason: fmt.Sprintf(format, args...)}
	}

	s := Source{X: a.X, Y: a.Y, Color: RGB{1, 1, 1}, Dir: DirNorth, Angle: 360, Height: 1, System: SystemComplex}

	if raw, ok := get("light_system"); ok && !isNull(raw) {
		sys, ok := parseSystem(raw)
		if !ok {
			return bad("unparsable light_system %s", raw)
		}
		s.System = sys
	}
	if s.System == SystemNone {
		return skip("light_system is NO_LIGHT_SUPPORT")
	}

	rangeVar, rangeDef := "light_range", 0.0
	powerVar, powerDef := "light_power", 1.0
	colorVar, colorDef := "light_color", `"#ffffff"`
	if prof != nil {
		rangeVar, rangeDef = prof.RangeVar, prof.RangeDefault
		powerVar, powerDef = prof.PowerVar, prof.PowerDefault
		colorVar, colorDef = prof.ColorVar, prof.ColorDefault
	}

	var err string
	if s.Range, err = numVar(get, rangeVar, rangeDef); err != "" {
		return bad("%s", err)
	}
	if s.Power, err = numVar(get, powerVar, powerDef); err != "" {
		return bad("%s", err)
	}
	if s.Range <= 0 {
		return skip("%s is not positive", rangeVar)
	}
	if s.Power == 0 {
		return skip("%s is zero", powerVar)
	}
	// light_on gates every light (lighting_atom.dm:50). Profiles model
	// fixtures as powered, so they ignore it.
	if prof == nil {
		on, e := boolVar(get, "light_on", true)
		if e != "" {
			return bad("%s", e)
		}
		if !on {
			return skip("light_on is false")
		}
	}
	if prof != nil && prof.StatusVar != "" {
		status, e := numVar(get, prof.StatusVar, float64(prof.StatusOK))
		if e != "" {
			return bad("%s", e)
		}
		if int(status) != prof.StatusOK {
			return skip("%s", ReasonNoBulb)
		}
	}

	rawColor, ok := get(colorVar)
	if !ok || isNull(rawColor) {
		rawColor = colorDef
	}
	if prof != nil && prof.OverrideColorVar != "" {
		if override, ok := get(prof.OverrideColorVar); ok && !isNull(override) {
			rawColor, colorVar = override, prof.OverrideColorVar
		}
	}
	col, ok := ParseColor(rawColor)
	if !ok {
		return bad("unparsable %s %s", colorVar, rawColor)
	}
	s.Color = col

	angleDef := 360.0
	if prof != nil {
		angleDef = prof.AngleDefault
	}
	if s.Angle, err = numVar(get, "light_angle", angleDef); err != "" {
		return bad("%s", err)
	}
	if s.Height, err = numVar(get, "light_height", 1); err != "" {
		return bad("%s", err)
	}

	switch {
	case prof != nil && prof.FacingFromDir:
		d, e := dirVar(get, "dir", DirSouth)
		if e != "" {
			return bad("%s", e)
		}
		s.Dir = d.Reverse()
		dx, dy := d.vec()
		s.ShiftX += float64(dx) * prof.ShiftTiles
		s.ShiftY += float64(dy) * prof.ShiftTiles
	case s.System == SystemOverlayDirectional || s.System == SystemOverlayBeam:
		// Overlay cones follow the holder's facing, not light_dir
		// (overlay_lighting.dm:100,296-297).
		d, e := dirVar(get, "dir", DirSouth)
		if e != "" {
			return bad("%s", e)
		}
		s.Dir = d
	default:
		d, e := dirVar(get, "light_dir", DirNorth)
		if e != "" {
			return bad("%s", e)
		}
		s.Dir = d
	}

	flags, e := numVar(get, "light_flags", 0)
	if e != "" {
		return bad("%s", e)
	}
	if int(flags)&4 == 0 { // LIGHT_IGNORE_OFFSET (__DEFINES/lighting.dm:24)
		px, e1 := numVar(get, "pixel_x", 0)
		py, e2 := numVar(get, "pixel_y", 0)
		if e1 != "" || e2 != "" {
			return bad("unparsable pixel offset")
		}
		s.ShiftX += px / 32 // lighting_atom.dm:222-223 (negated into a shift)
		s.ShiftY += py / 32
	}
	return Extraction{Source: s, Emits: true}
}

// IsOpaque reads the opacity variable. bad is true when it is present but not
// understood; callers should treat that tile as transparent and surface the
// problem.
func IsOpaque(get VarLookup) (opaque, bad bool) {
	if get == nil {
		return false, false
	}
	v, e := boolVar(get, "opacity", false)
	return v, e != ""
}

// ParseAreaBase reads base_lighting_color and base_lighting_alpha
// (lighting_area.dm:9-11; defaults alpha 0, white).
func ParseAreaBase(get VarLookup) (AreaBase, bool) {
	b := AreaBase{Color: RGB{1, 1, 1}}
	if get == nil {
		return b, true
	}
	alpha, e := numVar(get, "base_lighting_alpha", 0)
	if e != "" {
		return AreaBase{}, false
	}
	b.Alpha = alpha
	if raw, ok := get("base_lighting_color"); ok && !isNull(raw) {
		c, ok := ParseColor(raw)
		if !ok {
			return AreaBase{}, false
		}
		b.Color = c
	}
	return b, true
}

// StaticLighting reads static_lighting (default TRUE, static_lighting_area.dm:39).
// An unparsable value is treated as the default.
func StaticLighting(get VarLookup) bool {
	if get == nil {
		return true
	}
	v, e := boolVar(get, "static_lighting", true)
	if e != "" {
		return true
	}
	return v
}

func isNull(raw string) bool { return strings.TrimSpace(raw) == "null" }

func numVar(get VarLookup, name string, def float64) (float64, string) {
	raw, ok := get(name)
	if !ok || isNull(raw) {
		return def, ""
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Sprintf("unparsable %s %s", name, raw)
	}
	return v, ""
}

func boolVar(get VarLookup, name string, def bool) (bool, string) {
	raw, ok := get(name)
	if !ok || isNull(raw) {
		return def, ""
	}
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "TRUE":
		return true, ""
	case "FALSE":
		return false, ""
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(v) {
		return false, fmt.Sprintf("unparsable %s %s", name, raw)
	}
	return v != 0, ""
}

var systemNames = map[string]System{
	"NO_LIGHT_SUPPORT": SystemNone, "COMPLEX_LIGHT": SystemComplex, "OVERLAY_LIGHT": SystemOverlay,
	"OVERLAY_LIGHT_DIRECTIONAL": SystemOverlayDirectional, "OVERLAY_LIGHT_BEAM": SystemOverlayBeam,
}

func parseSystem(raw string) (System, bool) {
	raw = strings.TrimSpace(raw)
	if s, ok := systemNames[raw]; ok {
		return s, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > 4 {
		return 0, false
	}
	return System(n), true
}

var dirNames = map[string]Dir{
	"NORTH": DirNorth, "SOUTH": DirSouth, "EAST": DirEast, "WEST": DirWest,
	"NORTHEAST": DirNorth | DirEast, "NORTHWEST": DirNorth | DirWest,
	"SOUTHEAST": DirSouth | DirEast, "SOUTHWEST": DirSouth | DirWest,
}

func dirVar(get VarLookup, name string, def Dir) (Dir, string) {
	raw, ok := get(name)
	if !ok || isNull(raw) {
		return def, ""
	}
	var d Dir
	for _, part := range strings.Split(raw, "|") {
		part = strings.TrimSpace(part)
		if v, ok := dirNames[part]; ok {
			d |= v
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 15 {
			return 0, fmt.Sprintf("unparsable %s %s", name, raw)
		}
		d |= Dir(n)
	}
	return d, ""
}

var byondColors = map[string]string{
	"black": "#000000", "silver": "#c0c0c0", "gray": "#808080", "grey": "#808080", "white": "#ffffff",
	"maroon": "#800000", "red": "#ff0000", "purple": "#800080", "fuchsia": "#ff00ff", "magenta": "#ff00ff",
	"green": "#00c000", "lime": "#00ff00", "olive": "#808000", "gold": "#ffd700", "yellow": "#ffff00",
	"navy": "#000080", "blue": "#0000ff", "teal": "#008080", "aqua": "#00ffff", "cyan": "#00ffff",
}

// ParseColor reads a DM colour string such as "#ffaa00", "#fa0", "#ffaa0080"
// (alpha ignored, like rgb2num parts 1-3 in PARSE_LIGHT_COLOR,
// __DEFINES/lighting.dm:134-146), or a BYOND basic colour name. Quotes are
// optional. null yields white.
func ParseColor(raw string) (RGB, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "null" {
		return RGB{1, 1, 1}, true
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	if hex, ok := byondColors[strings.ToLower(raw)]; ok {
		raw = hex
	}
	if !strings.HasPrefix(raw, "#") {
		return RGB{}, false
	}
	h := raw[1:]
	switch len(h) {
	case 3, 4:
		var b strings.Builder
		for _, c := range h[:3] {
			b.WriteRune(c)
			b.WriteRune(c)
		}
		h = b.String()
	case 6, 8:
		h = h[:6]
	default:
		return RGB{}, false
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return RGB{}, false
	}
	return RGB{float64(n>>16&0xff) / 255, float64(n>>8&0xff) / 255, float64(n&0xff) / 255}, true
}
