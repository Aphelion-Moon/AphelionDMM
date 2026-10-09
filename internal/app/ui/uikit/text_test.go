package uikit

import "testing"

// fixed measures one unit per rune.
func fixed(s string) float32 { return float32(len([]rune(s))) }

func TestMiddleTruncateKeepsBothEnds(t *testing.T) {
	path := "/obj/machinery/atmospherics/pipe/smart/manifold4w/scrubbers/hidden/layer2"
	got := MiddleTruncate(path, 30, fixed)
	if fixed(got) > 30 {
		t.Fatalf("%q is %v wide, want <= 30", got, fixed(got))
	}
	if got[:5] != "/obj/" || got[len(got)-6:] != "layer2" {
		t.Fatalf("%q lost the head or the tail", got)
	}
	if MiddleTruncate("/obj/short", 30, fixed) != "/obj/short" {
		t.Fatal("a fitting string changed")
	}
	if got := MiddleTruncate(path, 2, fixed); fixed(got) > 2 {
		t.Fatalf("tiny width gave %q", got)
	}
}

func TestEndTruncate(t *testing.T) {
	if got := EndTruncate("dir = 1; light_color = \"#d1dfff\"", 12, fixed); got != "dir = 1; li…" {
		t.Fatalf("end truncate = %q", got)
	}
	if EndTruncate("short", 12, fixed) != "short" {
		t.Fatal("a fitting string changed")
	}
}
