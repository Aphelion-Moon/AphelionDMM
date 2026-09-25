package dmicon

import (
	"errors"
	"image"
	"testing"
	"time"

	"sdmm/internal/aphelion/iconassets"
)

func TestVisibleIconQueuePrioritizesRowsAndServesOrdinaryWork(t *testing.T) {
	cache := &IconsCache{asynchronous: true}
	cache.async.assets = make(map[string]*iconLoad)
	for index := range 6 {
		key := "visible-" + string(rune('a'+index))
		cache.queue(key, cache.asset(key), RequestVisible, false)
	}
	cache.queue("map", cache.asset("map"), RequestOrdinary, false)

	want := []string{"visible-a", "visible-b", "visible-c", "visible-d", "map", "visible-e", "visible-f"}
	for _, expected := range want {
		key, load, ok := cache.nextWaiting()
		if !ok || key != expected {
			t.Fatalf("next request = %q, %t; want %q", key, ok, expected)
		}
		if load.priority == RequestVisible {
			cache.async.visibleBurst++
		} else {
			cache.async.visibleBurst = 0
		}
		load.phase = iconIdle
	}
}

func TestVisibleDemandPromotesAnOrdinaryQueuedIcon(t *testing.T) {
	cache := &IconsCache{asynchronous: true}
	cache.async.assets = make(map[string]*iconLoad)
	cache.queue("map", cache.asset("map"), RequestOrdinary, false)
	cache.queue("row", cache.asset("row"), RequestVisible, false)
	cache.queue("map", cache.asset("map"), RequestVisible, false)

	for _, expected := range []string{"row", "map"} {
		key, load, ok := cache.nextWaiting()
		if !ok || key != expected || load.priority != RequestVisible {
			t.Fatalf("next request = %q (%d), %t; want promoted visible request %q", key, load.priority, ok, expected)
		}
		load.phase = iconIdle
	}
	if _, _, ok := cache.nextWaiting(); ok {
		t.Fatal("promotion left a duplicate ordinary request queued")
	}
}

func TestTransientIconFailureRetriesWithoutReplacingHandle(t *testing.T) {
	cache := &IconsCache{asynchronous: true}
	cache.async.assets = make(map[string]*iconLoad)
	cache.async.sprites = make(map[string]map[spriteKey]*Sprite)
	key := spriteKey{state: "idle", dir: 0}
	stable := &Sprite{}
	cache.async.sprites["fixture.dmi"] = map[spriteKey]*Sprite{key: stable}
	load := cache.asset("fixture.dmi")
	load.priority = RequestVisible
	load.phase = iconWorking
	cache.scheduleRetry("fixture.dmi", load, &iconassets.AssetError{
		Kind:      iconassets.FailureTransient,
		StageName: "memory admission",
		Err:       errors.New("budget temporarily full"),
	})

	status := cache.spriteLoad("fixture.dmi", "idle", 0)
	if status.State != SpritePending || status.Category != iconassets.FailureTransient || status.RetryAt.IsZero() {
		t.Fatalf("transient result = state %d category %q retry %v, want delayed retry", status.State, status.Category, status.RetryAt)
	}
	if got := cache.findSprite("fixture.dmi", key); got != stable {
		t.Fatal("transient retry replaced the pending sprite handle")
	}

	cache.queueDueRetries(time.Now().Add(time.Hour))
	queuedIcon, gotLoad, ok := cache.nextWaiting()
	if !ok {
		t.Fatal("due retry was not queued")
	}
	if queuedIcon != "fixture.dmi" || gotLoad.priority != RequestVisible || gotLoad.phase != iconQueued {
		t.Fatalf("due retry = %q (priority %d, phase %d), want visible queued retry", queuedIcon, gotLoad.priority, gotLoad.phase)
	}
	if got := cache.findSprite("fixture.dmi", key); got != stable {
		t.Fatal("queued retry replaced the pending sprite handle")
	}
}

func TestReadyAsyncIconUsesRetainedHandleAcrossSuccessfulRetry(t *testing.T) {
	previousPlaceholder := spritePlaceholder
	placeholderDmi := &Dmi{IconWidth: 32, IconHeight: 32, TextureWidth: 32, TextureHeight: 32, Cols: 1, Rows: 1, Image: image.NewNRGBA(image.Rect(0, 0, 32, 32)), Texture: 99}
	spritePlaceholder = &Sprite{dmi: placeholderDmi}
	t.Cleanup(func() { spritePlaceholder = previousPlaceholder })

	oldSprite := iconTestSprite(32, 0)
	cache := &IconsCache{
		asynchronous: true,
		icons:        map[string]*Dmi{"fixture.dmi": oldSprite.dmi},
		async: asyncIcons{
			assets:  map[string]*iconLoad{},
			sprites: map[string]map[spriteKey]*Sprite{},
		},
	}

	stable := cache.GetSpriteOrPlaceholderV("fixture.dmi", "", 0)
	if stable == oldSprite || stable.Dmi() != oldSprite.dmi {
		t.Fatal("ready async wrapper returned a raw DMI sprite instead of a retained handle")
	}
	load := cache.asset("fixture.dmi")
	load.phase = iconWorking
	loading, status := cache.RequestSpriteV("fixture.dmi", "", 0, RequestVisible)
	if loading != stable || status.State != SpritePending {
		t.Fatal("refresh did not keep the retained handle while loading")
	}

	newSprite := iconTestSprite(64, 22)
	cache.publishIcon(&iconBuild{key: "fixture.dmi", decoded: &iconassets.Decoded{}, dmi: newSprite.dmi})
	if ready, status := cache.RequestSpriteV("fixture.dmi", "", 0, RequestVisible); ready != stable || status.State != SpriteReady || ready.Dmi() != newSprite.dmi || ready.Texture() != 22 || ready.IconWidth() != 64 {
		t.Fatalf("successful retry failed to update retained handle: same=%t state=%d dmi=%t texture=%d width=%d", ready == stable, status.State, ready.Dmi() == newSprite.dmi, ready.Texture(), ready.IconWidth())
	}
}

func iconTestSprite(width int, texture uint32) *Sprite {
	dmi := &Dmi{IconWidth: width, IconHeight: width, TextureWidth: width, TextureHeight: width, Cols: 1, Rows: 1, Texture: texture, States: map[string]*State{}}
	sprite := &Sprite{dmi: dmi, X2: width, Y2: width, U2: 1, V2: 1}
	dmi.States[""] = &State{Dirs: 1, Frames: 1, Sprites: []*Sprite{sprite}}
	return sprite
}
