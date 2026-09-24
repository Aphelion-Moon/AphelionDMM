package editing

import (
	"sdmm/internal/aphelion/collab/model"
	"testing"
)

func TestCompositionPreservesAbsentAndHiddenChannels(t *testing.T) {
	before := model.TileState{Prefabs: []model.PrefabState{{StableID: "floor", Path: "/turf/open/floor"}, {StableID: "hidden", Path: "/obj/hidden"}, {StableID: "old", Path: "/obj/old"}}}
	source := TileIntent{}
	source[Objects] = ChannelIntent{Action: Set, Data: []model.PrefabState{{StableID: "new", Path: "/obj/new"}}}
	visible := func(path string) bool { return path != "/obj/hidden" }
	for _, mode := range []PasteMode{OnlyOverwriteWithData, ApplyOver, ReplaceIncludingBlanks} {
		after, err := ComposeTile(before, source, PastePolicy{Mode: mode, Channels: AllChannels}, visible)
		if err != nil {
			t.Fatal(err)
		}
		want := 3
		if mode == ApplyOver {
			want = 4
		}
		if len(after.Prefabs) != want || after.Prefabs[0].StableID != "floor" || after.Prefabs[1].StableID != "hidden" {
			t.Fatalf("mode %v lost retained values: %+v", mode, after)
		}
	}
}

func TestCompositionClearingRequiresExplicitIntentAndPolicy(t *testing.T) {
	before := model.TileState{Prefabs: []model.PrefabState{{StableID: "obj", Path: "/obj/item"}}}
	source := TileIntent{}
	source[Objects] = ChannelIntent{Action: Clear}
	for _, mode := range []PasteMode{OnlyOverwriteWithData, ApplyOver, ReplaceIncludingBlanks} {
		after, err := ComposeTile(before, source, PastePolicy{Mode: mode, Channels: AllChannels}, func(string) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		if (len(after.Prefabs) == 0) != (mode == ReplaceIncludingBlanks) {
			t.Fatalf("mode %v: %+v", mode, after)
		}
	}
	after, err := ComposeTile(before, source, PastePolicy{Mode: ReplaceIncludingBlanks}, func(string) bool { return true })
	if err != nil || !after.Equal(before) {
		t.Fatalf("disabled channel cleared: %+v %v", after, err)
	}
}

func TestCompositionSpaceIsDataAndHiddenSingletonConflictIsRejected(t *testing.T) {
	before := model.TileState{Prefabs: []model.PrefabState{{StableID: "floor", Path: "/turf/open/floor"}}}
	source := TileIntent{}
	source[Turfs] = ChannelIntent{Action: Set, Data: []model.PrefabState{{StableID: "space", Path: "/turf/open/space"}}}
	after, err := ComposeTile(before, source, PastePolicy{Mode: ApplyOver, Channels: AllChannels}, func(string) bool { return true })
	if err != nil || len(after.Prefabs) != 1 || after.Prefabs[0].Path != "/turf/open/space" {
		t.Fatalf("space was not data: %+v %v", after, err)
	}
	if _, err := ComposeTile(before, source, PastePolicy{Channels: AllChannels}, func(path string) bool { return path != "/turf/open/floor" }); err == nil {
		t.Fatal("must not append a second turf or erase a hidden turf")
	}
}
