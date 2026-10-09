package colorvar

import (
	"reflect"
	"testing"
)

func TestIsColorVar(t *testing.T) {
	for _, name := range []string{"color", "light_color", "bulb_colour", "pipe_color", "base_lighting_color", "nightshift_light_color"} {
		if !IsColorVar(name) {
			t.Errorf("%s is a colour variable", name)
		}
	}
	for _, name := range []string{"icon_state", "colorblind", "light_power", "name"} {
		if IsColorVar(name) {
			t.Errorf("%s is not a colour variable", name)
		}
	}
}

func TestParseFormatKeepsStyle(t *testing.T) {
	for _, tc := range []struct{ in, out string }{
		{`"#d1dfff"`, `"#d1dfff"`},
		{`"#FFA62B"`, `"#FFA62B"`},
		{`"#fa0"`, `"#ffaa00"`},
		{`"#ffaa0080"`, `"#ffaa0080"`},
	} {
		c, ok := Parse(tc.in)
		if !ok {
			t.Fatalf("Parse(%s) failed", tc.in)
		}
		if got := Format(c); got != tc.out {
			t.Errorf("Format(Parse(%s)) = %s, want %s", tc.in, got, tc.out)
		}
	}
	for _, bad := range []string{"null", `"red"`, `list(1,0,0, 0,1,0, 0,0,1)`, `"#12345"`, `"#gggggg"`} {
		if _, ok := Parse(bad); ok {
			t.Errorf("Parse(%s) accepted", bad)
		}
	}
	// Digits-only hex has no case to keep; a pick writes lower case.
	c, _ := Parse(`"#123456"`)
	if got := Format(FromFloats([3]float32{1, 0, 0}, c)); got != `"#ff0000"` {
		t.Errorf("pick over digits-only value = %s", got)
	}
	up, _ := Parse(`"#ABCDEF"`)
	if got := Format(FromFloats([3]float32{1, 0.5, 0}, up)); got != `"#FF8000"` {
		t.Errorf("pick over upper-case value = %s", got)
	}
}

func TestRememberDedupesAndCaps(t *testing.T) {
	recent := []string{`"#ff0000"`, `"#00ff00"`}
	got := Remember(recent, `"#00FF00"`)
	if want := []string{`"#00FF00"`, `"#ff0000"`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("remember = %v, want %v", got, want)
	}
	if got := Remember(recent, "null"); !reflect.DeepEqual(got, recent) {
		t.Fatalf("remembered a non-colour: %v", got)
	}
	var many []string
	for i := 0; i < 40; i++ {
		many = Remember(many, Format(Color{R: uint8(i), A: 255}))
	}
	if len(many) != MaxRecent || many[0] != `"#270000"` {
		t.Fatalf("recent = %d entries, first %s", len(many), many[0])
	}
}
