package render

import (
	"sdmm/internal/app/render/brush"
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	"sdmm/internal/aphelion/rendercache"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/render/bucket"
	"sdmm/internal/dmapi/dmmap"
	// APHELION EDIT ADDITION START - OCCURRENCE GEOMETRY
	"sdmm/internal/dmapi/dmmap/dmminstance"
	// APHELION EDIT ADDITION END
	"sdmm/internal/util"
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	"time"
	// APHELION EDIT ADDITION END

	"github.com/go-gl/gl/v3.3-core/gl"
)

type Render struct {
	Camera *Camera

	bucket *bucket.Bucket

	overlay       overlay
	unitProcessor unitProcessor
	// APHELION EDIT ADDITION START - PLACEMENT PRESENTATION
	presentation         *Presentation
	updates              renderUpdateBatch
	levelBuilds          map[int]*levelBuild
	levelReady           map[int]bool
	levelBuildDmm        *dmmap.Dmm
	levelBuildDimensions [3]int
	levelBuildGeneration uint64
	viewportWidth        float32
	viewportHeight       float32
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	retained           *rendercache.Cache
	retainedPending    []retainedPreparation
	retainedQueued     map[rendercache.Key]rendercache.Versions
	retainedRetireNext bool
	retainedView       util.Bounds
	// APHELION EDIT ADDITION END
	geometry        map[int]*geometryAllocation
	geometryWaiting bool
	geometryDrawn   time.Time
	// APHELION EDIT ADDITION END
}

func New() *Render {
	brush.TryInit()
	return &Render{
		Camera: newCamera(),
		bucket: bucket.New(),
		// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
		retained: rendercache.New(),
		// APHELION EDIT ADDITION END
	}
}

func (r *Render) SetUnitProcessor(processor unitProcessor) {
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	r.clearRetainedScene()
	// APHELION EDIT ADDITION END
	r.unitProcessor = processor
	// APHELION EDIT ADDITION START - OCCURRENCE GEOMETRY
	r.bucket.InstanceFilter = nil
	if mask, ok := processor.(interface {
		GeometryInstanceVisible(*dmminstance.Instance) bool
	}); ok {
		r.bucket.InstanceFilter = mask.GeometryInstanceVisible
	}
	if r.levelBuildDmm != nil {
		r.InvalidateLevelBuilds(r.levelBuildDmm)
	}
	// APHELION EDIT ADDITION END
}

func (r *Render) SetOverlay(state overlay) {
	r.overlay = state
}

func (r *Render) SetActiveLevel(dmm *dmmap.Dmm, activeLevel int) {
	r.Camera.Level = activeLevel
	/* APHELION EDIT REMOVAL START - OWNED MAP OPEN
	if r.bucket.Level(activeLevel) == nil { // Ensure level exists
		// APHELION EDIT CHANGE - BOUNDED COLD LEVEL - ORIGINAL: r.UpdateBucket(dmm, activeLevel)
		if r.levelBuild == nil {
			r.BeginLevelBuild(dmm, activeLevel)
		}
	}
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	r.BeginLevelBuild(dmm, activeLevel)
	// APHELION EDIT ADDITION END
}

// UpdateBucketV will update the bucket data by the provided level.
func (r *Render) UpdateBucketV(dmm *dmmap.Dmm, level int, tilesToUpdate []util.Point) {
	// APHELION EDIT ADDITION START - FRAME GEOMETRY BATCH
	r.ensureLevelBuildMap(dmm)
	if r.queueBucketUpdate(level, tilesToUpdate) {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	if !r.admitGeometry(dmm, level, tilesToUpdate, false) {
		r.evictGeometry(level)
		return
	}
	if len(tilesToUpdate) == 0 {
		tilesToUpdate = nil
	} else if r.bucket.Level(level) == nil {
		r.bucket.PrepareLevel(dmm, level)
		r.BeginLevelBuild(dmm, r.Camera.Level)
	}
	// APHELION EDIT ADDITION END
	r.bucket.UpdateLevel(dmm, level, tilesToUpdate)
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	if len(tilesToUpdate) == 0 {
		r.markLevelReady(level)
	}
	// APHELION EDIT ADDITION END
}

// UpdateBucket will ensure that the bucket has data by the provided level.
func (r *Render) UpdateBucket(dmm *dmmap.Dmm, level int) {
	r.UpdateBucketV(dmm, level, nil)
}

func (r *Render) Draw(width, height float32) {
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	// Retired GL allocations drain through the shared visual scheduler.
	r.geometryDrawn = time.Now()
	r.viewportWidth, r.viewportHeight = width, height
	r.reprioritizeRetained()
	// APHELION EDIT ADDITION END
	r.prepare()
	r.draw(width, height)
	r.cleanup()
}

// Initialize OpenGL state.
func (r *Render) prepare() {
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	gl.BlendEquation(gl.FUNC_ADD)
	gl.ActiveTexture(gl.TEXTURE0)
}

func (r *Render) draw(width, height float32) {
	r.batchBucketUnits(r.viewportBounds(width, height))
	//r.batchChunksVisuals()
	// APHELION EDIT CHANGE - BORDER CULLING - ORIGINAL: r.batchOverlayAreasBorders()
	r.batchOverlayAreasBorders(r.viewportBounds(width, height))
	r.batchOverlayAreas()
	brush.Draw(width, height, r.Camera.ShiftX, r.Camera.ShiftY, r.Camera.Scale)
}

// Clean OpenGL state after rendering.
func (r *Render) cleanup() {
	gl.Disable(gl.BLEND)
}

func (r *Render) viewportBounds(width, height float32) util.Bounds {
	// Get transformed bounds of the map, so we can ignore out of bounds units.
	w := width / r.Camera.Scale
	h := height / r.Camera.Scale

	x1 := -r.Camera.ShiftX
	y1 := -r.Camera.ShiftY
	x2 := x1 + w
	y2 := y1 + h

	return util.Bounds{
		X1: x1,
		Y1: y1,
		X2: x2,
		Y2: y2,
	}
}
