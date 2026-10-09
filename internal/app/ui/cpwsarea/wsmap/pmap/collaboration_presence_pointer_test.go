package pmap

import (
	"strings"
	"testing"

	collabui "sdmm/internal/aphelion/collab/ui"

	"github.com/SpaiR/imgui-go"
)

func TestPresencePointerPolygonAnchorsTipAndTriangulatesWithoutGaps(t *testing.T) {
	t.Parallel()

	tip := imgui.Vec2{X: 100, Y: 200}
	vertices := presencePointerPolygon(tip, 1)
	if vertices[0] != tip {
		t.Fatalf("tip vertex = %v, want %v", vertices[0], tip)
	}
	for index, vertex := range vertices {
		if vertex.X < tip.X || vertex.Y < tip.Y {
			t.Fatalf("vertex %d = %v extends up or left of the tip", index, vertex)
		}
	}
	doubled := presencePointerPolygon(tip, 2)
	if doubled[3].X-tip.X != 2*(vertices[3].X-tip.X) || doubled[3].Y-tip.Y != 2*(vertices[3].Y-tip.Y) {
		t.Fatal("scale does not scale offsets from the tip")
	}
	// Summed triangle areas must equal the polygon (shoelace) area: no gaps or overlaps.
	var shoelace float32
	for index := range vertices {
		next := vertices[(index+1)%len(vertices)]
		shoelace += vertices[index].X*next.Y - next.X*vertices[index].Y
	}
	if shoelace < 0 {
		shoelace = -shoelace
	}
	shoelace /= 2
	var triangles float32
	for _, triangle := range presencePointerTriangles {
		a, b, c := vertices[triangle[0]], vertices[triangle[1]], vertices[triangle[2]]
		area := ((b.X-a.X)*(c.Y-a.Y) - (c.X-a.X)*(b.Y-a.Y)) / 2
		if area < 0 {
			area = -area
		}
		triangles += area
	}
	if diff := shoelace - triangles; diff > 0.01 || diff < -0.01 {
		t.Fatalf("triangle area %.3f != polygon area %.3f", triangles, shoelace)
	}
}

func TestPresenceBadgeTextCombinesInitialsAndBoundedName(t *testing.T) {
	t.Parallel()

	if got := presenceBadgeText(collabui.PresenceOverlay{Initials: "AL", Label: "Ada Lovelace"}); got != "AL  Ada Lovelace" {
		t.Fatalf("badge = %q", got)
	}
	long := presenceBadgeText(collabui.PresenceOverlay{Initials: "X", Label: strings.Repeat("ab", 40)})
	if runes := []rune(long); len(runes) > len("X  ")+presenceBadgeMaxLabelRunes || !strings.HasSuffix(long, "...") {
		t.Fatalf("long badge = %q", long)
	}
	// Truncation must not split a multi-byte rune.
	cjk := presenceBadgeText(collabui.PresenceOverlay{Initials: "日", Label: strings.Repeat("日", 60)})
	if !strings.HasSuffix(cjk, "...") || strings.ContainsRune(cjk, 0xFFFD) {
		t.Fatalf("cjk badge = %q", cjk)
	}
}
