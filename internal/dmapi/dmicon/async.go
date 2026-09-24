// APHELION EDIT ADDITION START - ASYNC ICONS
package dmicon

import (
	"context"
	"errors"
	"github.com/rs/zerolog/log"
	"path/filepath"
	"sdmm/internal/aphelion/iconassets"
	"sdmm/internal/util"
	"time"
)

var ErrPending = errors.New("icon is loading")

type spriteKey struct {
	state string
	dir   int
}
type asyncIcons struct {
	worker              *iconassets.Worker
	ctx                 context.Context
	cancel              context.CancelFunc
	pending             map[string]bool
	waiting             []string
	sprites             map[string]map[spriteKey]*Sprite
	current             *iconBuild
	maxWidth, maxHeight int
}
type iconBuild struct {
	key                 string
	decoded             *iconassets.Decoded
	upload              *iconassets.Upload
	dmi                 *Dmi
	state, frame, index int
	uploaded            bool
}

func (i *IconsCache) request(icon string) error {
	if i.async.ctx == nil {
		i.async.ctx, i.async.cancel = context.WithCancel(context.Background())
		i.async.pending = make(map[string]bool)
		i.async.sprites = make(map[string]map[spriteKey]*Sprite)
	}
	if i.async.worker == nil {
		i.async.worker = iconassets.NewWorker()
	}
	if !i.async.pending[icon] {
		i.async.pending[icon] = true
		i.async.waiting = append(i.async.waiting, icon)
	}
	return ErrPending
}

// Pending handles belong to the graphics owner. Updating a handle after upload
// refreshes cached palette rows, chunks and ghosts without rebuilding a map.
func (i *IconsCache) pendingSprite(icon, state string, dir int) *Sprite {
	sprites := i.async.sprites[icon]
	if sprites == nil {
		sprites = make(map[spriteKey]*Sprite)
		i.async.sprites[icon] = sprites
	}
	key := spriteKey{state, dir}
	if sprite := sprites[key]; sprite != nil {
		return sprite
	}
	sprite := *SpritePlaceholder()
	sprites[key] = &sprite
	return &sprite
}

func (i *IconsCache) cancelPending() {
	if i.async.cancel != nil {
		i.async.cancel()
	}
	if current := i.async.current; current != nil {
		current.upload.Cancel()
		current.decoded.Release()
	}
	worker := i.async.worker
	i.async = asyncIcons{worker: worker}
}

// ProcessUploads is called before UI construction, never from a worker or a
// draw callback. CPU decode is serialized and graphics work yields each frame.
func (i *IconsCache) ProcessUploads() {
	if i.async.ctx == nil {
		return
	}
	deadline := time.Now().Add(2 * time.Millisecond)
	for len(i.async.waiting) > 0 {
		key := i.async.waiting[0]
		if !i.async.worker.Submit(iconassets.Request{Context: i.async.ctx, Key: key, Path: filepath.Join(i.rootDirPath, key)}) {
			break
		}
		i.async.waiting[0] = ""
		i.async.waiting = i.async.waiting[1:]
		if time.Now().After(deadline) {
			return
		}
	}
	if i.async.current == nil {
		select {
		case result := <-i.async.worker.Results:
			if result.Context != i.async.ctx {
				result.Decoded.Release()
				return
			}
			if result.Err != nil {
				log.Error().Err(result.Err).Str("icon", result.Key).Msg("Unable to load icon")
				i.icons[result.Key] = nil
				delete(i.async.pending, result.Key)
				delete(i.async.sprites, result.Key)
				result.Decoded.Release()
				return
			}
			upload, err := iconassets.NewUpload(result.Decoded.Image)
			if err != nil {
				result.Decoded.Release()
				i.icons[result.Key] = nil
				delete(i.async.pending, result.Key)
				delete(i.async.sprites, result.Key)
				log.Error().Err(err).Msg("Unable to upload icon")
				return
			}
			data := result.Decoded
			metadata := data.Metadata
			i.async.current = &iconBuild{key: result.Key, decoded: data, upload: upload, dmi: &Dmi{IconWidth: metadata.Width, IconHeight: metadata.Height, TextureWidth: data.Image.Bounds().Dx(), TextureHeight: data.Image.Bounds().Dy(), Cols: data.Image.Bounds().Dx() / metadata.Width, Rows: data.Image.Bounds().Dy() / metadata.Height, Image: data.Image, Texture: upload.Texture, States: make(map[string]*State)}}
		default:
			return
		}
	}
	current := i.async.current
	if !current.uploaded {
		var err error
		current.uploaded, err = current.upload.Step()
		if err != nil {
			current.upload.Cancel()
			current.decoded.Release()
			i.icons[current.key] = nil
			delete(i.async.pending, current.key)
			delete(i.async.sprites, current.key)
			i.async.current = nil
			log.Error().Err(err).Msg("Unable to upload icon")
		}
		return
	}
	for count := 0; count < 256 && time.Now().Before(deadline); count++ {
		if current.state == len(current.decoded.Metadata.States) {
			i.publishIcon(current)
			return
		}
		metadata := current.decoded.Metadata.States[current.state]
		state := current.dmi.States[metadata.Name]
		if current.frame == 0 {
			state = &State{Dirs: metadata.Dirs, Frames: metadata.Frames}
			current.dmi.States[metadata.Name] = state
		}
		state.Sprites = append(state.Sprites, newDmiSprite(current.dmi, current.index))
		current.index++
		current.frame++
		if current.frame == metadata.Dirs*metadata.Frames {
			current.state++
			current.frame = 0
		}
	}
}

func (i *IconsCache) publishIcon(current *iconBuild) {
	i.async.maxWidth = max(i.async.maxWidth, current.dmi.IconWidth-32)
	i.async.maxHeight = max(i.async.maxHeight, current.dmi.IconHeight-32)
	// A name can have many cached state/direction handles; publish at most 256
	// per frame, retaining the completed upload until the last handle resolves.
	count := 0
	for key, sprite := range i.async.sprites[current.key] {
		if state, err := current.dmi.State(key.state); err == nil {
			*sprite = *state.SpriteV(key.dir)
		}
		delete(i.async.sprites[current.key], key)
		count++
		if count == 256 {
			return
		}
	}
	i.icons[current.key] = current.dmi
	delete(i.async.pending, current.key)
	delete(i.async.sprites, current.key)
	current.decoded.Release()
	i.async.current = nil
}

// Late sprite dimensions only extend right/up from the existing cached origin.
// A conservative cache-wide extent avoids missing overhang during publication
// without invalidating or synchronously rebuilding all map geometry.
func (i *IconsCache) ExpandPendingBounds(bounds util.Bounds) util.Bounds {
	bounds.X2 += float32(i.async.maxWidth)
	bounds.Y2 += float32(i.async.maxHeight)
	return bounds
}

func (i *IconsCache) Loading() bool { return len(i.async.pending) != 0 }

// APHELION EDIT ADDITION END
