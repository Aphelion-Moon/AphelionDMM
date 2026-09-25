package dmicon

import (
	"errors"
	"fmt"

	"sdmm/internal/dmapi/dm"

	"github.com/rs/zerolog/log"
)

// APHELION EDIT CHANGE - ASYNC ICONS - ORIGINAL: var Cache = &IconsCache{icons: make(map[string]*Dmi)}
var Cache = &IconsCache{icons: make(map[string]*Dmi), asynchronous: true}

type IconsCache struct {
	rootDirPath string
	icons       map[string]*Dmi
	// APHELION EDIT ADDITION START - ASYNC ICONS
	asynchronous bool
	async        asyncIcons
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	revision uint64
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION END
}

func (i *IconsCache) Free() {
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	i.revision++
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - ASYNC ICONS
	i.cancelPending()
	// APHELION EDIT ADDITION END
	for _, dmi := range i.icons {
		dmi.free()
	}
	log.Printf("cache free; [%d] icons disposed", len(i.icons))
	i.rootDirPath = ""
	i.icons = make(map[string]*Dmi)
}

// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
// Revision changes when cached textures or pending sprite handles change.
func (i *IconsCache) Revision() uint64 { return i.revision }

// APHELION EDIT ADDITION END

func (i *IconsCache) SetRootDirPath(rootDirPath string) {
	// APHELION EDIT ADDITION START - ASYNC ICONS
	if i.rootDirPath != rootDirPath {
		i.Free()
	}
	// APHELION EDIT ADDITION END
	i.rootDirPath = rootDirPath
	log.Print("cache root dir:", rootDirPath)
}

func (i *IconsCache) Get(icon string) (*Dmi, error) {
	if len(icon) == 0 {
		return nil, errors.New("dmi icon is empty")
	}

	if dmi, ok := i.icons[icon]; ok {
		// APHELION EDIT CHANGE - ICON RECOVERY - ORIGINAL: if dmi == nil { return nil, fmt.Errorf("dmi [%s] is nil", icon) }
		if dmi != nil {
			return dmi, nil
		}
		if !i.asynchronous {
			return nil, fmt.Errorf("dmi [%s] is nil", icon)
		}
	}

	// APHELION EDIT ADDITION START - ASYNC ICONS
	if i.asynchronous {
		return nil, i.request(icon, RequestOrdinary)
	}
	// APHELION EDIT ADDITION END
	dmi, err := New(i.rootDirPath + "/" + icon)
	i.icons[icon] = dmi
	return dmi, err
}

func (i *IconsCache) GetState(icon, state string) (*State, error) {
	dmi, err := i.Get(icon)
	if err != nil {
		return nil, err
	}
	return dmi.State(state)
}

func (i *IconsCache) GetSpriteV(icon, state string, dir int) (*Sprite, error) {
	dmiState, err := i.GetState(icon, state)
	if err != nil {
		return nil, err
	}
	return dmiState.SpriteV(dir), nil
}

func (i *IconsCache) GetSprite(icon, state string) (*Sprite, error) {
	return i.GetSpriteV(icon, state, dm.DirDefault)
}

func (i *IconsCache) GetSpriteOrPlaceholder(icon, state string) *Sprite {
	return i.GetSpriteOrPlaceholderV(icon, state, dm.DirDefault)
}

func (i *IconsCache) GetSpriteOrPlaceholderV(icon, state string, dir int) *Sprite {
	// APHELION EDIT ADDITION START - ICON RECOVERY
	if i.asynchronous {
		sprite, _ := i.RequestSpriteV(icon, state, dir, RequestOrdinary)
		return sprite
	}
	// APHELION EDIT ADDITION END
	if s, err := i.GetSpriteV(icon, state, dir); err == nil {
		return s
	}
	/* APHELION EDIT REMOVAL START - ICON RECOVERY
	if i.asynchronous && i.async.pending[icon] {
		return i.pendingSprite(icon, state, dir)
	}
	APHELION EDIT REMOVAL END */
	return SpritePlaceholder()
}
