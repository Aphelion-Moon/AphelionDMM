package lighting

import "testing"

func lookup(m map[string]string) VarLookup {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

func TestExtractBasicComplexLight(t *testing.T) {
	a := Atom{Path: "/obj/structure/statue", X: 3, Y: 4, Var: lookup(map[string]string{
		"light_range": "3", "light_power": "0.7", "light_color": `"#78FF78"`,
	})}
	e := ExtractSource(a, nil)
	if !e.Emits {
		t.Fatalf("should emit: %+v", e)
	}
	s := e.Source
	if s.X != 3 || s.Y != 4 || s.Range != 3 || s.Power != 0.7 || s.System != SystemComplex {
		t.Fatalf("bad source %+v", s)
	}
	near(t, "g", s.Color.G, 1, 1e-12)
	near(t, "r", s.Color.R, 0x78/255.0, 1e-12)
	// Defaults from _atom.dm:69-92.
	if s.Angle != 360 || s.Height != 1 || s.Dir != DirNorth {
		t.Fatalf("defaults wrong %+v", s)
	}
}

func TestExtractNonEmitters(t *testing.T) {
	cases := map[string]map[string]string{
		"range zero":     {"light_range": "0"},
		"no vars":        {},
		"power zero":     {"light_range": "3", "light_power": "0"},
		"light_on false": {"light_range": "3", "light_on": "0"},
		"no support":     {"light_range": "3", "light_system": "0"},
		"null range":     {"light_range": "null"},
	}
	for name, vars := range cases {
		e := ExtractSource(Atom{Path: "/obj/x", Var: lookup(vars)}, nil)
		if e.Emits {
			t.Fatalf("%s: should not emit", name)
		}
		if e.Unparsable {
			t.Fatalf("%s: not a parse failure: %q", name, e.Reason)
		}
		if e.Reason == "" {
			t.Fatalf("%s: reason required", name)
		}
	}
}

func TestExtractUnparsableIsSkippedWithReason(t *testing.T) {
	cases := map[string]map[string]string{
		"range expr":   {"light_range": "SOME_MACRO + 1"},
		"power text":   {"light_range": "3", "light_power": `"bright"`},
		"colour macro": {"light_range": "3", "light_color": "LIGHT_COLOR_UNKNOWN"},
		"colour junk":  {"light_range": "3", "light_color": `"#zzz"`},
		"angle junk":   {"light_range": "3", "light_angle": "wide"},
		"dir junk":     {"light_range": "3", "light_dir": "UP"},
	}
	for name, vars := range cases {
		e := ExtractSource(Atom{Path: "/obj/x", Var: lookup(vars)}, nil)
		if e.Emits || !e.Unparsable || e.Reason == "" {
			t.Fatalf("%s: got %+v", name, e)
		}
	}
}

func TestExtractColorForms(t *testing.T) {
	cases := map[string]RGB{
		`"#ffffff"`:   {1, 1, 1},
		`"#FFAA00"`:   {1, 170.0 / 255, 0},
		`"#fa0"`:      {1, 170.0 / 255, 0},
		`"#ffaa0080"`: {1, 170.0 / 255, 0}, // alpha ignored: rgb2num parts 1-3 only
		`"red"`:       {1, 0, 0},
		`#ffaa00`:     {1, 170.0 / 255, 0},
		`null`:        {1, 1, 1},
	}
	for in, want := range cases {
		e := ExtractSource(Atom{Path: "/obj/x", Var: lookup(map[string]string{"light_range": "2", "light_color": in})}, nil)
		if !e.Emits {
			t.Fatalf("%s: %+v", in, e)
		}
		near(t, in+" r", e.Source.Color.R, want.R, 1e-9)
		near(t, in+" g", e.Source.Color.G, want.G, 1e-9)
		near(t, in+" b", e.Source.Color.B, want.B, 1e-9)
	}
}

func TestExtractLightSystems(t *testing.T) {
	cases := map[string]System{"1": SystemComplex, "2": SystemOverlay, "3": SystemOverlayDirectional, "4": SystemOverlayBeam,
		"COMPLEX_LIGHT": SystemComplex, "OVERLAY_LIGHT": SystemOverlay}
	for in, want := range cases {
		e := ExtractSource(Atom{Path: "/obj/x", Var: lookup(map[string]string{"light_range": "2", "light_system": in})}, nil)
		if !e.Emits || e.Source.System != want {
			t.Fatalf("%s: %+v", in, e)
		}
	}
}

func TestExtractDirNamesAndNumbers(t *testing.T) {
	cases := map[string]Dir{"1": DirNorth, "2": DirSouth, "4": DirEast, "8": DirWest, "5": DirNorth | DirEast, "NORTH": DirNorth, "SOUTHWEST": DirSouth | DirWest}
	for in, want := range cases {
		e := ExtractSource(Atom{Path: "/obj/x", Var: lookup(map[string]string{"light_range": "2", "light_dir": in})}, nil)
		if !e.Emits || e.Source.Dir != want {
			t.Fatalf("%s: %+v", in, e)
		}
	}
}

func TestExtractPixelShift(t *testing.T) {
	a := Atom{Path: "/obj/x", Var: lookup(map[string]string{"light_range": "2", "pixel_x": "16", "pixel_y": "-32"})}
	e := ExtractSource(a, nil)
	near(t, "shift x", e.Source.ShiftX, 0.5, 1e-12)
	near(t, "shift y", e.Source.ShiftY, -1, 1e-12)
	a = Atom{Path: "/obj/x", Var: lookup(map[string]string{"light_range": "2", "pixel_x": "16", "light_flags": "4"})}
	e = ExtractSource(a, nil)
	if e.Source.ShiftX != 0 {
		t.Fatalf("LIGHT_IGNORE_OFFSET must zero the shift")
	}
}

func TestFixtureProfileDerivesLightFromBulbVars(t *testing.T) {
	// /obj/machinery/light has light_range 0 and sets its light at runtime from
	// brightness/bulb_power/bulb_colour (light.dm:24-28,302-306), with
	// light_angle 170 (light.dm:14) and light_dir reversed (light.dm:114).
	a := Atom{Path: "/obj/machinery/light/directional/north", X: 1, Y: 1, Var: lookup(map[string]string{
		"dir": "1", "light_angle": "170", "brightness": "8", "bulb_power": "1", "bulb_colour": `"#f3fffa"`,
	})}
	e := ExtractSource(a, DefaultProfiles())
	if !e.Emits {
		t.Fatalf("fixture should emit: %+v", e)
	}
	s := e.Source
	if s.Range != 8 || s.Power != 1 || s.Angle != 170 || s.Dir != DirSouth {
		t.Fatalf("fixture %+v", s)
	}
	near(t, "shift y", s.ShiftY, 0.5, 1e-12) // light.dm:166-170
	near(t, "shift x", s.ShiftX, 0, 1e-12)

	// Defaults when only the type is known.
	e = ExtractSource(Atom{Path: "/obj/machinery/light", Var: lookup(map[string]string{})}, DefaultProfiles())
	if !e.Emits || e.Source.Range != 8 || e.Source.Dir != DirNorth {
		t.Fatalf("defaults %+v", e)
	}

	// Path boundary: light switches are not fixtures.
	e = ExtractSource(Atom{Path: "/obj/machinery/light_switch", Var: lookup(map[string]string{})}, DefaultProfiles())
	if e.Emits {
		t.Fatalf("light_switch must not match the fixture profile")
	}
	// Without profiles the fixture does not emit (static light_range is 0).
	e = ExtractSource(a, nil)
	if e.Emits {
		t.Fatalf("no profile -> no light")
	}
}

func TestIsOpaque(t *testing.T) {
	yes := []string{"1", "TRUE", "true"}
	for _, v := range yes {
		if ok, bad := IsOpaque(lookup(map[string]string{"opacity": v})); !ok || bad {
			t.Fatalf("%q", v)
		}
	}
	for _, v := range []string{"0", "FALSE", "null"} {
		if ok, bad := IsOpaque(lookup(map[string]string{"opacity": v})); ok || bad {
			t.Fatalf("%q", v)
		}
	}
	if ok, bad := IsOpaque(lookup(map[string]string{})); ok || bad {
		t.Fatalf("missing is transparent")
	}
	if _, bad := IsOpaque(lookup(map[string]string{"opacity": "maybe"})); !bad {
		t.Fatalf("unparsable must be flagged")
	}
}

func TestParseAreaBase(t *testing.T) {
	b, ok := ParseAreaBase(lookup(map[string]string{"base_lighting_alpha": "255", "base_lighting_color": `"#8589fa"`}))
	if !ok || b.Alpha != 255 || b.Color.B <= b.Color.R {
		t.Fatalf("%+v %v", b, ok)
	}
	b, ok = ParseAreaBase(lookup(map[string]string{}))
	if !ok || b.Alpha != 0 {
		t.Fatalf("defaults %+v", b)
	}
	if _, ok := ParseAreaBase(lookup(map[string]string{"base_lighting_alpha": "lots"})); ok {
		t.Fatalf("junk alpha must fail")
	}
	if got := StaticLighting(lookup(map[string]string{})); !got {
		t.Fatalf("static_lighting defaults TRUE (static_lighting_area.dm:39)")
	}
	if got := StaticLighting(lookup(map[string]string{"static_lighting": "0"})); got {
		t.Fatalf("static_lighting 0")
	}
}
