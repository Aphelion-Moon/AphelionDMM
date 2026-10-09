package pmap

import (
	"strings"
	"testing"

	collabui "sdmm/internal/aphelion/collab/ui"

	"github.com/SpaiR/imgui-go"
)

// Presence is tile-granular, so no pointer is drawn inside the highlighted
// tile. The badge hangs off the tile's lower-right corner, clear of the tile.
func TestPresenceBadgeAnchorsOutsideTheTile(t *testing.T) {
	t.Parallel()

	min, max := imgui.Vec2{X: 100, Y: 200}, imgui.Vec2{X: 132, Y: 232}
	anchor := presenceBadgeAnchor(max)
	if anchor.X < max.X || anchor.Y < max.Y {
		t.Fatalf("badge anchor %v overlaps tile %v..%v", anchor, min, max)
	}
	if anchor.X-max.X > 4 || anchor.Y-max.Y > 4 {
		t.Fatalf("badge anchor %v drifts away from tile corner %v", anchor, max)
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
