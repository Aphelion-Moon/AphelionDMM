// APHELION EDIT ADDITION START - ASYNC ICONS
package dmicon

import (
	"container/heap"
	"container/list"
	"context"
	"errors"
	"path/filepath"
	"sdmm/internal/aphelion/iconassets"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/util"
	"time"

	"github.com/rs/zerolog/log"
)

var ErrPending = errors.New("icon is loading")

type RequestPriority uint8

const (
	RequestOrdinary RequestPriority = iota
	RequestVisible
)

type SpriteLoadState uint8

const (
	SpriteUnassigned SpriteLoadState = iota
	SpritePending
	SpriteReady
	SpriteFailed
)

type SpriteLoad struct {
	State    SpriteLoadState
	Category iconassets.FailureKind
	Stage    string
	Err      error
	RetryAt  time.Time
}

type spriteKey struct {
	state string
	dir   int
}

type iconPhase uint8

const (
	iconIdle iconPhase = iota
	iconQueued
	iconWorking
	iconRetryWaiting
	iconFailed
)

const (
	visibleBurstLimit   = 4
	maxAutomaticRetries = 5
)

var retryDelays = [...]time.Duration{100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}

type iconLoad struct {
	phase           iconPhase
	err             error
	category        iconassets.FailureKind
	stage           string
	priority        RequestPriority
	attempts        int
	retryAt         time.Time
	retryGeneration uint64
	queueElement    *list.Element
	spriteErrors    map[spriteKey]error
}

type waitingIcon struct {
	icon     string
	priority RequestPriority
}

type retryEntry struct {
	icon       string
	at         time.Time
	generation uint64
}

type retryHeap []retryEntry

func (h retryHeap) Len() int           { return len(h) }
func (h retryHeap) Less(a, b int) bool { return h[a].at.Before(h[b].at) }
func (h retryHeap) Swap(a, b int)      { h[a], h[b] = h[b], h[a] }
func (h *retryHeap) Push(value any)    { *h = append(*h, value.(retryEntry)) }
func (h *retryHeap) Pop() any {
	last := len(*h) - 1
	value := (*h)[last]
	(*h)[last] = retryEntry{}
	*h = (*h)[:last]
	return value
}

type asyncIcons struct {
	worker          *iconassets.Worker
	ctx             context.Context
	cancel          context.CancelFunc
	assets          map[string]*iconLoad
	waitingVisible  list.List
	waitingOrdinary list.List
	retries         retryHeap
	sprites         map[string]map[spriteKey]*Sprite
	current         *iconBuild
	visibleBurst    int
	maxWidth        int
	maxHeight       int
}

type iconBuild struct {
	key                 string
	decoded             *iconassets.Decoded
	upload              *iconassets.Upload
	dmi                 *Dmi
	state, frame, index int
	uploaded            bool
	resolved            map[spriteKey]bool
}

func (i *IconsCache) ensureAsync() {
	if i.async.ctx == nil {
		i.async.ctx, i.async.cancel = context.WithCancel(context.Background())
	}
	if i.async.assets == nil {
		i.async.assets = make(map[string]*iconLoad)
	}
	if i.async.sprites == nil {
		i.async.sprites = make(map[string]map[spriteKey]*Sprite)
	}
	if i.async.worker == nil {
		i.async.worker = iconassets.NewWorker()
	}
}

func (i *IconsCache) request(icon string, priority RequestPriority) error {
	if dmi, ok := i.icons[icon]; ok && dmi != nil {
		return nil
	}
	i.ensureAsync()
	load := i.asset(icon)
	if load.phase == iconFailed {
		return load.err
	}
	if load.phase == iconRetryWaiting && !time.Now().Before(load.retryAt) {
		load.phase = iconIdle
	}
	if load.phase == iconIdle {
		i.queue(icon, load, priority, false)
	} else if load.phase == iconQueued && priority > load.priority {
		i.queue(icon, load, priority, false)
	}
	return ErrPending
}

func (i *IconsCache) asset(icon string) *iconLoad {
	load := i.async.assets[icon]
	if load == nil {
		load = &iconLoad{}
		i.async.assets[icon] = load
	}
	return load
}

func (i *IconsCache) queue(icon string, load *iconLoad, priority RequestPriority, front bool) {
	if load.queueElement != nil {
		if load.priority == priority {
			return
		}
		if load.priority == RequestVisible {
			i.async.waitingVisible.Remove(load.queueElement)
		} else {
			i.async.waitingOrdinary.Remove(load.queueElement)
		}
		load.queueElement = nil
	}
	load.phase = iconQueued
	load.priority = priority
	entry := waitingIcon{icon: icon, priority: priority}
	queue := &i.async.waitingOrdinary
	if priority == RequestVisible {
		queue = &i.async.waitingVisible
	}
	if front {
		load.queueElement = queue.PushFront(entry)
	} else {
		load.queueElement = queue.PushBack(entry)
	}
}

func (i *IconsCache) nextWaiting() (string, *iconLoad, bool) {
	for {
		queue := &i.async.waitingVisible
		if i.async.waitingVisible.Len() == 0 || i.async.visibleBurst >= visibleBurstLimit && i.async.waitingOrdinary.Len() > 0 {
			queue = &i.async.waitingOrdinary
		}
		if queue.Len() == 0 {
			return "", nil, false
		}
		element := queue.Front()
		entry := element.Value.(waitingIcon)
		queue.Remove(element)
		load := i.async.assets[entry.icon]
		if load != nil {
			load.queueElement = nil
		}
		if load == nil || load.phase != iconQueued || load.priority != entry.priority {
			continue
		}
		return entry.icon, load, true
	}
}

func (i *IconsCache) queueDueRetries(now time.Time) {
	for len(i.async.retries) > 0 && !i.async.retries[0].at.After(now) {
		entry := heap.Pop(&i.async.retries).(retryEntry)
		load := i.async.assets[entry.icon]
		if load == nil || load.phase != iconRetryWaiting || load.retryGeneration != entry.generation {
			continue
		}
		load.phase = iconIdle
		i.queue(entry.icon, load, load.priority, false)
	}
}

func (i *IconsCache) scheduleRetry(icon string, load *iconLoad, err error) {
	load.err = err
	load.category = iconassets.FailureCategory(err)
	load.stage = iconassets.FailureStage(err)
	if load.category == iconassets.FailureTransient && load.attempts < maxAutomaticRetries {
		delay := retryDelays[load.attempts]
		load.attempts++
		load.phase = iconRetryWaiting
		load.retryAt = time.Now().Add(delay)
		load.retryGeneration++
		heap.Push(&i.async.retries, retryEntry{icon: icon, at: load.retryAt, generation: load.retryGeneration})
		return
	}
	load.phase = iconFailed
	log.Error().
		Str("icon", icon).
		Str("stage", load.stage).
		Str("reason", string(load.category)).
		Err(err).
		Msg("Icon load failed")
}

// RequestSpriteV returns a stable row handle and its current asset outcome.
// Visible requests outrank ordinary map demand while the shared decoder stays
// single-threaded and serves ordinary work after a bounded visible burst.
func (i *IconsCache) RequestSpriteV(icon, state string, dir int, priority RequestPriority) (*Sprite, SpriteLoad) {
	if icon == "" {
		return SpritePlaceholder(), SpriteLoad{State: SpriteUnassigned}
	}
	if !i.asynchronous {
		sprite, err := i.GetSpriteV(icon, state, dir)
		if err == nil {
			return sprite, SpriteLoad{State: SpriteReady}
		}
		return SpritePlaceholder(), SpriteLoad{State: SpriteFailed, Category: iconassets.FailureInvalid, Stage: "load", Err: err}
	}
	key := spriteKey{state: state, dir: dir}
	load := i.async.assets[icon]
	if dmi, ok := i.icons[icon]; ok && dmi != nil {
		if load != nil && (load.phase == iconQueued || load.phase == iconWorking || load.phase == iconRetryWaiting) {
			if sprite := i.findSprite(icon, key); sprite != nil {
				return sprite, SpriteLoad{State: SpritePending, Category: load.category, Stage: load.stage, Err: load.err, RetryAt: load.retryAt}
			}
			if dmiState, err := dmi.State(state); err == nil {
				return i.retainSprite(icon, key, dmiState.SpriteV(dir)), SpriteLoad{State: SpritePending, Category: load.category, Stage: load.stage, Err: load.err, RetryAt: load.retryAt}
			}
			return i.pendingSprite(icon, state, dir), SpriteLoad{State: SpritePending, Category: load.category, Stage: load.stage, Err: load.err, RetryAt: load.retryAt}
		}
		if dmiState, err := dmi.State(state); err == nil {
			sprite := i.retainSprite(icon, key, dmiState.SpriteV(dir))
			if load != nil && load.phase == iconFailed {
				return sprite, SpriteLoad{State: SpriteFailed, Category: load.category, Stage: load.stage, Err: load.err, RetryAt: load.retryAt}
			}
			return sprite, SpriteLoad{State: SpriteReady}
		} else {
			err = i.assetForStateError(icon, key, err)
			return i.pendingSprite(icon, state, dir), SpriteLoad{State: SpriteFailed, Category: iconassets.FailureInvalid, Stage: "state", Err: err}
		}
	}
	_ = i.request(icon, priority)
	sprite := i.pendingSprite(icon, state, dir)
	return sprite, i.spriteLoad(icon, state, dir)
}

func (i *IconsCache) retainSprite(icon string, key spriteKey, source *Sprite) *Sprite {
	sprite := i.pendingSprite(icon, key.state, key.dir)
	*sprite = *source
	return sprite
}

func (i *IconsCache) assetForStateError(icon string, key spriteKey, err error) error {
	i.ensureAsync()
	load := i.asset(icon)
	if load.spriteErrors == nil {
		load.spriteErrors = make(map[spriteKey]error)
	}
	load.spriteErrors[key] = err
	return err
}

func (i *IconsCache) spriteLoad(icon, state string, dir int) SpriteLoad {
	load := i.asset(icon)
	if err := load.spriteErrors[spriteKey{state: state, dir: dir}]; err != nil {
		return SpriteLoad{State: SpriteFailed, Category: iconassets.FailureInvalid, Stage: "state", Err: err}
	}
	switch load.phase {
	case iconFailed:
		return SpriteLoad{State: SpriteFailed, Category: load.category, Stage: load.stage, Err: load.err, RetryAt: load.retryAt}
	case iconRetryWaiting:
		return SpriteLoad{State: SpritePending, Category: load.category, Stage: load.stage, Err: load.err, RetryAt: load.retryAt}
	default:
		return SpriteLoad{State: SpritePending}
	}
}

// RetryIcon explicitly retries a visible permanent failure or a transient
// failure whose automatic retry budget has been exhausted. The existing row
// handle is retained while the same asset key is loaded again.
func (i *IconsCache) RetryIcon(icon string) bool {
	if icon == "" || !i.asynchronous {
		return false
	}
	i.ensureAsync()
	load := i.asset(icon)
	if load.phase == iconQueued || load.phase == iconWorking {
		return false
	}
	load.phase = iconIdle
	load.err = nil
	load.category = ""
	load.stage = ""
	load.attempts = 0
	load.retryAt = time.Time{}
	load.retryGeneration++
	load.spriteErrors = nil
	i.queue(icon, load, RequestVisible, false)
	return true
}

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

func (i *IconsCache) findSprite(icon string, key spriteKey) *Sprite {
	return i.async.sprites[icon][key]
}

func (i *IconsCache) cancelPending() {
	if i.async.cancel != nil {
		i.async.cancel()
	}
	if current := i.async.current; current != nil {
		if current.upload != nil {
			current.upload.Cancel()
		}
		current.decoded.Release()
	}
	worker := i.async.worker
	i.async = asyncIcons{worker: worker}
}

// ProcessUploads is called before UI construction, never from a worker or a
// draw callback. CPU decode is serialized and graphics work yields each frame.
func (i *IconsCache) ProcessUploads() {
	budget := resources.FrameWorkRemaining(2 * time.Millisecond)
	if budget == 0 {
		return
	}
	defer resources.ChargeFrameWork(time.Now())
	if i.async.ctx == nil {
		return
	}
	deadline := time.Now().Add(budget)
	i.queueDueRetries(time.Now())
	if icon, load, ok := i.nextWaiting(); ok {
		request := iconassets.Request{Context: i.async.ctx, Key: icon, Path: filepath.Join(i.rootDirPath, icon)}
		if i.async.worker.Submit(request) {
			load.phase = iconWorking
			if load.priority == RequestVisible {
				i.async.visibleBurst++
			} else {
				i.async.visibleBurst = 0
			}
		} else {
			i.queue(icon, load, load.priority, true)
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
				result.Decoded.Release()
				i.scheduleRetry(result.Key, i.asset(result.Key), result.Err)
				return
			}
			upload, err := iconassets.NewUpload(result.Decoded.Image)
			if err != nil {
				result.Decoded.Release()
				i.scheduleRetry(result.Key, i.asset(result.Key), err)
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
			i.async.current = nil
			i.scheduleRetry(current.key, i.asset(current.key), err)
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
	load := i.asset(current.key)
	count := 0
	for key, sprite := range i.async.sprites[current.key] {
		if current.resolved[key] {
			continue
		}
		state, err := current.dmi.State(key.state)
		if err == nil {
			*sprite = *state.SpriteV(key.dir)
			delete(load.spriteErrors, key)
		} else {
			if load.spriteErrors == nil {
				load.spriteErrors = make(map[spriteKey]error)
			}
			load.spriteErrors[key] = err
		}
		if current.resolved == nil {
			current.resolved = make(map[spriteKey]bool)
		}
		current.resolved[key] = true
		count++
		if count == 256 {
			i.revision++
			return
		}
	}
	if count > 0 {
		i.revision++
	}
	if previous := i.icons[current.key]; previous != nil && previous != current.dmi {
		previous.free()
	}
	i.icons[current.key] = current.dmi
	load.phase = iconIdle
	load.err = nil
	load.category = ""
	load.stage = ""
	load.attempts = 0
	load.retryGeneration++
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

func (i *IconsCache) Loading() bool {
	for _, load := range i.async.assets {
		if load.phase == iconQueued || load.phase == iconWorking || load.phase == iconRetryWaiting {
			return true
		}
	}
	return false
}

// APHELION EDIT ADDITION END
