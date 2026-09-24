package unit

import (
	"math"
	"testing"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

func TestParseColorAppliesAlphaWithoutColorOverride(t *testing.T) {
	for _, tc := range []struct {
		alpha string
		want  float32
	}{
		{alpha: "0", want: 0},
		{alpha: "128", want: 128.0 / 255.0},
		{alpha: "255", want: 1},
	} {
		t.Run(tc.alpha, func(t *testing.T) {
			vars := &dmvars.MutableVariables{}
			vars.Put("alpha", tc.alpha)
			prefab := dmmprefab.New(dmmprefab.IdNone, "/obj/alpha-test", vars.ToImmutable())

			r, g, b, a := parseColor(prefab)
			if r != 1 || g != 1 || b != 1 {
				t.Fatalf("default RGB = %v, %v, %v; want white", r, g, b)
			}
			if math.Abs(float64(a-tc.want)) > 1e-6 {
				t.Fatalf("alpha = %v, want %v", a, tc.want)
			}
		})
	}
}

func TestParseColorKeepsExplicitRGBAndAlpha(t *testing.T) {
	vars := &dmvars.MutableVariables{}
	vars.Put("color", `"#336699"`)
	vars.Put("alpha", "128")
	prefab := dmmprefab.New(dmmprefab.IdNone, "/obj/alpha-test", vars.ToImmutable())

	r, g, b, a := parseColor(prefab)
	want := [4]float32{0x33 / 255.0, 0x66 / 255.0, 0x99 / 255.0, 128.0 / 255.0}
	got := [4]float32{r, g, b, a}
	for index := range got {
		if math.Abs(float64(got[index]-want[index])) > 1e-6 {
			t.Fatalf("RGBA = %v, want %v", got, want)
		}
	}
}
