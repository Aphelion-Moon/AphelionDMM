// APHELION EDIT ADDITION START - LIGHTING PREVIEW
package render

import (
	"math"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/platform"
	"sdmm/internal/util"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/rs/zerolog/log"
)

// LightingFrame is the immutable light data of one level: 12 floats per tile in
// row-major order, the RGB multiplier of the SW, SE, NW and NE corners. It is
// produced off the UI thread and only read here.
type LightingFrame interface {
	Dimensions() (level, w, h int)
	Tiles() []float32
	Seq() uint64
	// RowsSince bounds the tile rows that differ from the frame with sequence
	// seq; full requests a complete upload.
	RowsSince(seq uint64) (y0, y1 int, full bool)
}

// SetTileObserver registers fn to learn which one-based tiles of a level the
// display changed. Empty points mean the whole level. level 0 means every level.
// The slice is only valid during the call. The lighting preview uses this to
// find dirty regions without touching the editing code.
func (r *Render) SetTileObserver(fn func(level int, points []util.Point)) { r.tileObserver = fn }

func (r *Render) notifyTiles(level int, points []util.Point) {
	if r.tileObserver != nil {
		r.tileObserver(level, points)
	}
}

// SetLighting installs the frame multiplied over the active level. nil disables
// the pass. strength is the darkness strength in 0..1:
// final = 1 - strength*(1 - light).
func (r *Render) SetLighting(f LightingFrame, strength float32) {
	if f == nil {
		r.lighting.frame = nil
		return
	}
	r.lighting.frame = f
	r.lighting.strength = min(1, max(0, strength))
}

func (r *Render) lightingActive() bool {
	f := r.lighting.frame
	if f == nil || r.lighting.strength <= 0 {
		return false
	}
	level, w, h := f.Dimensions()
	return level == r.Camera.Level && w > 0 && h > 0 && len(f.Tiles()) >= w*h*12
}

// DisposeLighting releases the GL objects. Call on the graphics thread.
func (r *Render) DisposeLighting() {
	l := &r.lighting
	l.frame = nil
	if l.vao != 0 {
		gl.DeleteVertexArrays(1, &l.vao)
		gl.DeleteBuffers(1, &l.vbo)
		gl.DeleteBuffers(1, &l.ebo)
		l.vao, l.vbo, l.ebo = 0, 0, 0
	}
	if l.program != 0 {
		gl.DeleteProgram(l.program)
		l.program = 0
	}
	l.w, l.h, l.seq = 0, 0, 0
}

type lightingState struct {
	frame    LightingFrame
	strength float32
	failed   bool

	program                       uint32
	vao, vbo, ebo                 uint32
	uTransform, uStrength, uWidth int32
	uTileSize                     int32
	w, h                          int
	seq                           uint64
	uploadedLevel                 int
}

const lightingVertexShader = `
#version 330 core
uniform mat4 Transform;
uniform int Width;
uniform float TileSize;
layout (location = 0) in vec3 in_light;
out vec3 light;
void main() {
	int tile = gl_VertexID / 4;
	int corner = gl_VertexID % 4;
	float x = float(tile % Width + (corner & 1));
	float y = float(tile / Width + (corner >> 1));
	light = in_light;
	gl_Position = Transform * vec4(vec2(x, y) * TileSize, 0.0, 1.0);
}
` + "\x00"

const lightingFragmentShader = `
#version 330 core
uniform float Strength;
in vec3 light;
out vec4 outputColor;
void main() {
	outputColor = vec4(mix(vec3(1.0), light, Strength), 1.0);
}
` + "\x00"

func (l *lightingState) init() bool {
	if l.program != 0 {
		return true
	}
	if l.failed {
		return false
	}
	program, err := platform.NewShaderProgram(lightingVertexShader, lightingFragmentShader)
	if err != nil {
		log.Error().Err(err).Msg("lighting preview shader unavailable")
		l.failed = true
		return false
	}
	l.program = program
	l.uTransform = gl.GetUniformLocation(program, gl.Str("Transform\x00"))
	l.uStrength = gl.GetUniformLocation(program, gl.Str("Strength\x00"))
	l.uWidth = gl.GetUniformLocation(program, gl.Str("Width\x00"))
	l.uTileSize = gl.GetUniformLocation(program, gl.Str("TileSize\x00"))
	gl.GenVertexArrays(1, &l.vao)
	gl.GenBuffers(1, &l.vbo)
	gl.GenBuffers(1, &l.ebo)
	gl.BindVertexArray(l.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, l.vbo)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 3, gl.FLOAT, false, 3*platform.FloatSize, 0)
	gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, l.ebo)
	gl.BindVertexArray(0)
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	return true
}

// upload brings the vertex buffer up to date with the frame. A level or size
// change, or a history gap, replaces the whole buffer; otherwise only the
// changed rows are sent.
func (l *lightingState) upload(f LightingFrame) {
	level, w, h := f.Dimensions()
	tiles := f.Tiles()
	if l.seq == f.Seq() && l.w == w && l.h == h && l.uploadedLevel == level {
		return
	}
	gl.BindVertexArray(l.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, l.vbo)
	sizeChanged := l.w != w || l.h != h || l.seq == 0
	y0, y1, full := f.RowsSince(l.seq)
	if sizeChanged || full || l.uploadedLevel != level {
		gl.BufferData(gl.ARRAY_BUFFER, w*h*12*platform.FloatSize, gl.Ptr(tiles[:w*h*12]), gl.DYNAMIC_DRAW)
		if sizeChanged {
			indices := make([]uint32, 0, w*h*6)
			for t := 0; t < w*h; t++ {
				b := uint32(t * 4)
				indices = append(indices, b, b+1, b+2, b+1, b+3, b+2)
			}
			gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, l.ebo)
			gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(indices)*4, gl.Ptr(indices), gl.STATIC_DRAW)
		}
	} else if y1 > y0 {
		y0, y1 = max(0, y0), min(h, y1)
		from := y0 * w * 12
		to := y1 * w * 12
		gl.BufferSubData(gl.ARRAY_BUFFER, from*platform.FloatSize, (to-from)*platform.FloatSize, gl.Ptr(tiles[from:to]))
	}
	gl.BindVertexArray(0)
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	l.w, l.h, l.seq, l.uploadedLevel = w, h, f.Seq(), level
}

// lightingVisibleRows returns the half-open range of tile rows that intersect
// the view's vertical extent.
func lightingVisibleRows(view util.Bounds, tileSize, h int) (int, int) {
	if tileSize <= 0 || h <= 0 {
		return 0, 0
	}
	a := int(math.Floor(float64(view.Y1) / float64(tileSize)))
	b := int(math.Ceil(float64(view.Y2) / float64(tileSize)))
	a, b = max(0, a), min(h, b)
	if b <= a {
		return 0, 0
	}
	return a, b
}

// drawLighting multiplies the framebuffer by the level's light. It owns GL
// state for its duration and restores the standard alpha blend.
func (r *Render) drawLighting(width, height float32, view util.Bounds) {
	l := &r.lighting
	if !r.lightingActive() || !l.init() {
		return
	}
	f := l.frame
	l.upload(f)
	_, w, h := f.Dimensions()
	rowA, rowB := lightingVisibleRows(view, dmmap.WorldIconSize, h)
	if rowB <= rowA {
		return
	}
	transform := mgl32.Ortho(0, width, 0, height, -1, 1).
		Mul4(mgl32.Scale2D(r.Camera.Scale, r.Camera.Scale).Mat4()).
		Mul4(mgl32.Translate2D(r.Camera.ShiftX, r.Camera.ShiftY).Mat4())

	gl.UseProgram(l.program)
	gl.UniformMatrix4fv(l.uTransform, 1, false, &transform[0])
	gl.Uniform1f(l.uStrength, l.strength)
	gl.Uniform1i(l.uWidth, int32(w))
	gl.Uniform1f(l.uTileSize, float32(dmmap.WorldIconSize))
	// Multiply the colour already in the framebuffer; keep its alpha.
	gl.BlendFuncSeparate(gl.DST_COLOR, gl.ZERO, gl.ZERO, gl.ONE)
	gl.BindVertexArray(l.vao)
	first := rowA * w * 6
	count := (rowB - rowA) * w * 6
	gl.DrawElementsWithOffset(gl.TRIANGLES, int32(count), gl.UNSIGNED_INT, uintptr(first*4))
	gl.BindVertexArray(0)
	gl.UseProgram(0)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
}

// APHELION EDIT ADDITION END
