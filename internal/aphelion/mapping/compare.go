package mapping

import (
	"reflect"
	"strings"

	"sdmm/internal/util"
)

type ChannelDiff struct{ Turf, Area, Objects bool }
type Transform struct{ Offset util.Point }

func (t Transform) Apply(p util.Point) util.Point {
	return util.Point{X: p.X + t.Offset.X, Y: p.Y + t.Offset.Y, Z: p.Z + t.Offset.Z}
}
func (t Transform) Inverse(p util.Point) util.Point {
	return util.Point{X: p.X - t.Offset.X, Y: p.Y - t.Offset.Y, Z: p.Z - t.Offset.Z}
}
func channel(a Atom) string {
	if a.Path == "/turf" || strings.HasPrefix(a.Path, "/turf/") {
		return "turf"
	}
	if a.Path == "/area" || strings.HasPrefix(a.Path, "/area/") {
		return "area"
	}
	return "objects"
}
func atomsFor(atoms []Atom, c string) []Atom {
	var result []Atom
	for _, a := range atoms {
		if channel(a) == c {
			result = append(result, a)
		}
	}
	return result
}
func CompareCell(a, b []Atom) ChannelDiff {
	return ChannelDiff{Turf: !reflect.DeepEqual(atomsFor(a, "turf"), atomsFor(b, "turf")), Area: !reflect.DeepEqual(atomsFor(a, "area"), atomsFor(b, "area")), Objects: !reflect.DeepEqual(atomsFor(a, "objects"), atomsFor(b, "objects"))}
}
