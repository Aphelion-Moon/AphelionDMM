package theme

import (
	"math"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
)

func luminance(c imgui.Vec4) float64 {
	ch := func(v float32) float64 {
		x := float64(v)
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.X) + 0.7152*ch(c.Y) + 0.0722*ch(c.Z)
}

// contrast is the WCAG 2 contrast ratio of two opaque colours.
func contrast(a, b imgui.Vec4) float64 {
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

func TestMeridianTextMeetsWCAG(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	s := imgui.CurrentStyle()
	ApplyColors(s)
	p := Meridian
	role := s.Color
	cases := []struct {
		name     string
		fg, bg   imgui.Vec4
		minimum  float64
		required string
	}{
		{"text on window", role(imgui.StyleColorText), role(imgui.StyleColorWindowBg), 7, "AAA body"},
		{"text on frame", role(imgui.StyleColorText), role(imgui.StyleColorFrameBg), 7, "AAA body"},
		{"text on button", role(imgui.StyleColorText), role(imgui.StyleColorButton), 7, "AAA body"},
		{"text on hovered button", role(imgui.StyleColorText), role(imgui.StyleColorButtonHovered), 4.5, "AA"},
		{"text on pressed button", role(imgui.StyleColorText), role(imgui.StyleColorButtonActive), 4.5, "AA"},
		{"text on selection", role(imgui.StyleColorText), role(imgui.StyleColorHeaderActive), 4.5, "AA"},
		{"text on popup", role(imgui.StyleColorText), Alpha(role(imgui.StyleColorPopupBg), 1), 7, "AAA body"},
		{"secondary text on window", role(imgui.StyleColorTextDisabled), role(imgui.StyleColorWindowBg), 4.5, "AA"},
		{"secondary text on frame", role(imgui.StyleColorTextDisabled), role(imgui.StyleColorFrameBg), 4.5, "AA"},
		{"dim text on window", p.TextDim, p.Bg0, 4.5, "AA"},
		{"accent ink on accent", p.AccentInk, p.Accent, 4.5, "AA"},
		{"check mark on frame", role(imgui.StyleColorCheckMark), role(imgui.StyleColorFrameBg), 3, "non-text UI"},
		{"tab text on inactive tab", role(imgui.StyleColorText), role(imgui.StyleColorTab), 7, "AAA body"},
		{"tab text on selected tab", role(imgui.StyleColorText), role(imgui.StyleColorTabActive), 7, "AAA body"},
		{"selected tab against tab bar", role(imgui.StyleColorTabActive), role(imgui.StyleColorTab), 1.3, "visible step"},
	}
	for _, c := range cases {
		if got := contrast(c.fg, c.bg); got < c.minimum {
			t.Errorf("%s: contrast %.2f < %.1f (%s)", c.name, got, c.minimum, c.required)
		}
	}
	for name, tint := range map[string]ButtonTint{"selected": SelectedButton, "positive": Positive, "negative": Negative} {
		for state, fill := range map[string]imgui.Vec4{"normal": tint.Normal, "hover": tint.Hover, "active": tint.Active} {
			if got := contrast(p.Text, fill); got < 4.5 {
				t.Errorf("text on %s button (%s): contrast %.2f < 4.5", name, state, got)
			}
		}
	}
	for state, fill := range map[string]imgui.Vec4{"normal": Primary.Normal, "hover": Primary.Hover, "active": Primary.Active} {
		if got := contrast(p.AccentInk, fill); got < 4.5 {
			t.Errorf("ink on primary button (%s): contrast %.2f < 4.5", state, got)
		}
	}
	for _, spec := range p.Spectrum() {
		if got := contrast(spec, p.Bg0); got < 4.5 {
			t.Errorf("spectrum colour %v on window: contrast %.2f < 4.5", spec, got)
		}
	}
}

func TestSelectDefaultsToMeridian(t *testing.T) {
	defer Select(NameMeridian)
	for _, name := range []string{"", "Unknown", NameMeridian} {
		Select(name)
		if Selected() != NameMeridian {
			t.Fatalf("Select(%q) = %s", name, Selected())
		}
	}
	Select(NameClassic)
	if Selected() != NameClassic || IsMeridian() {
		t.Fatal("Classic not selected")
	}
}

func TestPushMetricsScalesAndBalances(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	defer Select(NameMeridian)
	base := imgui.CurrentStyle().FramePadding()

	Select(NameClassic)
	if n := PushMetrics(1); n != 0 {
		t.Fatalf("Classic pushed %d metrics", n)
	}
	Select(NameMeridian)
	n := PushMetrics(1.5)
	if got := imgui.CurrentStyle().FramePadding(); got != (imgui.Vec2{X: 9, Y: 6}) {
		t.Fatalf("frame padding at 150%% = %v", got)
	}
	imgui.PopStyleVarV(n)
	if got := imgui.CurrentStyle().FramePadding(); got != base {
		t.Fatalf("pop left frame padding at %v, want %v", got, base)
	}

	SetCompact(true)
	defer SetCompact(false)
	n = PushMetrics(1)
	if got := imgui.CurrentStyle().FramePadding(); got != (imgui.Vec2{X: 4, Y: 2}) {
		t.Fatalf("compact frame padding = %v", got)
	}
	if got := imgui.CurrentStyle().ItemSpacing(); got.Y >= 5 {
		t.Fatalf("compact item spacing = %v, want tighter than comfortable", got)
	}
	imgui.PopStyleVarV(n)
}
