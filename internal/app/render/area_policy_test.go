package render

import (
	"testing"

)

func TestAreaPolicyApply(t *testing.T) {
	p := AreaPolicy{Alpha: 0.35, HideBase: true, BaseArea: "/area/space"}
	cases := []struct {
		name    string
		path    string
		alpha   float32
		want    float32
		visible bool
	}{
		{"turf unaffected", "/turf/open/floor", 1, 1, true},
		{"obj unaffected", "/obj/area_thing", 0.5, 0.5, true},
		{"area tinted", "/area/station/hall", 1, 0.35, true},
		{"area multiplies own alpha", "/area/station/hall", 0.5, 0.175, true},
		{"base area hidden", "/area/space", 1, 0, false},
		{"base area subtype not hidden", "/area/space/nearstation", 1, 0.35, true},
	}
	for _, c := range cases {
		got, vis := p.Apply(c.path, c.alpha)
		if vis != c.visible || (vis && (got < c.want-1e-6 || got > c.want+1e-6)) {
			t.Errorf("%s: got (%v,%v) want (%v,%v)", c.name, got, vis, c.want, c.visible)
		}
	}
	p.HideBase = false
	if _, vis := p.Apply("/area/space", 1); !vis {
		t.Error("base area must be visible when HideBase is off")
	}
	p.Alpha = 5
	if a, _ := p.Apply("/area/x", 1); a != 1 {
		t.Errorf("alpha must clamp to 1, got %v", a)
	}
	p.Alpha = -1
	if a, _ := p.Apply("/area/x", 1); a != 0 {
		t.Errorf("alpha must clamp to 0, got %v", a)
	}
}

func TestSetAreaPolicyBumpsRevisionOnlyOnChange(t *testing.T) {
	defer SetAreaPolicy(DefaultAreaPolicy())
	SetAreaPolicy(DefaultAreaPolicy())
	r := AreaPolicyRevision()
	SetAreaPolicy(DefaultAreaPolicy())
	if AreaPolicyRevision() != r {
		t.Fatal("unchanged policy must not bump revision")
	}
	p := DefaultAreaPolicy()
	p.Alpha = 0.5
	SetAreaPolicy(p)
	if AreaPolicyRevision() == r {
		t.Fatal("changed policy must bump revision")
	}
}
