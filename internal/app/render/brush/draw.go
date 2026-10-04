package brush

import (
	// APHELION EDIT ADDITION START - RETAINED ADMISSION
	"sdmm/internal/aphelion/resources"
	// APHELION EDIT ADDITION END
	"sdmm/internal/platform"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/mathgl/mgl32"
)

// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
// Submission stores ordered brush geometry, immutable while retained for drawing.
type Submission struct {
	vao, vbo, ebo uint32
	calls         []batchCall
	bytes         int
	reservation   *resources.Reservation
}

// CaptureAdmittedSubmission accounts for simultaneous CPU staging, GPU storage
// and selection metadata before graphics allocation. The owner supplies a
// conservative estimate and keeps it charged until graphics disposal.
func CaptureAdmittedSubmission(estimate uint64, build func()) (*Submission, error) {
	return CaptureAdmittedReplacement(nil, estimate, build)
}

// CaptureAdmittedReplacement takes exclusive ownership of a retired submission
// and reuses its graphics objects. Both estimates stay charged through upload.
// Empty, denied, or interrupted captures dispose the retired owner.
func CaptureAdmittedReplacement(retired *Submission, estimate uint64, build func()) (*Submission, error) {
	reservation, err := resources.DefaultBudget().Reserve(estimate)
	if err != nil {
		retired.Dispose()
		return nil, err
	}
	var submission *Submission
	defer func() {
		if submission == nil {
			reservation.Release()
			retired.Dispose()
		}
	}()
	submission = captureSubmission(retired, build)
	if submission != nil {
		submission.reservation.Release()
		submission.reservation = reservation
	}
	return submission, nil
}

// CaptureSubmission records brush primitives while leaving the frame batch intact.
func CaptureSubmission(build func()) *Submission {
	return captureSubmission(nil, build)
}

func captureSubmission(submission *Submission, build func()) *Submission {
	previous := batching
	captured := &Batching{}
	batching = captured
	defer func() { batching = previous }()

	build()
	captured.flush()
	if len(captured.data) == 0 {
		return nil
	}

	if submission == nil {
		submission = &Submission{}
	}
	submission.calls = append([]batchCall(nil), captured.calls...)
	submission.bytes = len(captured.data)*platform.FloatSize + len(captured.indices)*4
	fresh := submission.vao == 0
	if fresh {
		gl.GenVertexArrays(1, &submission.vao)
		gl.GenBuffers(1, &submission.vbo)
		gl.GenBuffers(1, &submission.ebo)
	}
	gl.BindVertexArray(submission.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, submission.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(captured.data)*platform.FloatSize, gl.Ptr(captured.data), gl.STATIC_DRAW)
	gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, submission.ebo)
	gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(captured.indices)*4, gl.Ptr(captured.indices), gl.STATIC_DRAW)
	if fresh {
		initAttributesFor(submission.vao, submission.vbo)
	} else {
		// Attribute pointers and the index-buffer binding belong to this VAO.
		gl.BindBuffer(gl.ARRAY_BUFFER, 0)
		gl.BindVertexArray(0)
	}
	return submission
}

func (s *Submission) ByteSize() int {
	if s == nil {
		return 0
	}
	return s.bytes
}

func (s *Submission) Dispose() {
	if s == nil {
		return
	}
	defer s.reservation.Release()
	if s.vao == 0 {
		return
	}
	gl.DeleteVertexArrays(1, &s.vao)
	gl.DeleteBuffers(1, &s.vbo)
	gl.DeleteBuffers(1, &s.ebo)
	s.vao, s.vbo, s.ebo = 0, 0, 0
}

// Draw flushes earlier stream geometry to preserve painter order.
func (s *Submission) Draw(w, h, x, y, z float32) {
	pass := NewDrawPass(w, h, x, y, z)
	defer pass.End()
	pass.DrawSubmission(s)
}

// APHELION EDIT ADDITION END - RETAINED SUBMISSIONS

// APHELION EDIT ADDITION START - SHARED BRUSH PASS
// DrawPass shares shader and material setup for one camera. Between its first
// draw and End, callers may queue primitives but must not change GL state, capture
// or dispose submissions, or invoke another draw pass. Flush any trailing stream
// geometry before End; an unused pass leaves GL state and queued geometry intact.
type DrawPass struct {
	width, height, shiftX, shiftY, scale float32
	bound                                bool
	hasTexture                           int32
	texture                              uint32
}

func NewDrawPass(w, h, x, y, z float32) DrawPass {
	return DrawPass{width: w, height: h, shiftX: x, shiftY: y, scale: z}
}

func (p *DrawPass) bind() {
	if p.bound {
		return
	}
	gl.UseProgram(program)
	mtxTransform := transformationMatrix(p.width, p.height, p.shiftX, p.shiftY, p.scale)
	gl.UniformMatrix4fv(uniformLocationTransform, 1, false, &mtxTransform[0])
	p.bound = true
	// Uniforms and texture bindings can belong to an earlier pass or renderer.
	p.hasTexture, p.texture = -1, 0
}

// DrawSubmission flushes earlier stream geometry to preserve painter order.
func (p *DrawPass) DrawSubmission(s *Submission) {
	if s == nil || s.vao == 0 || len(s.calls) == 0 {
		return
	}
	p.Flush()
	p.bind()
	gl.BindVertexArray(s.vao)
	p.drawCalls(s.calls)
}

// Flush draws and clears the queued stream geometry without ending the pass.
func (p *DrawPass) Flush() {
	batching.flush()
	if len(batching.data) == 0 {
		return
	}
	p.bind()
	gl.BindVertexArray(vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(batching.data)*platform.FloatSize, gl.Ptr(batching.data), gl.STREAM_DRAW)
	gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, ebo)
	gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(batching.indices)*platform.FloatSize, gl.Ptr(batching.indices), gl.STREAM_DRAW)
	p.drawCalls(batching.calls)
	// Detach only from the stream VAO: retained VAOs keep their index buffers.
	gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, 0)
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	batching.clear()
}

func (p *DrawPass) drawCalls(calls []batchCall) {
	for _, c := range calls {
		var hasTexture int32
		if c.texture != 0 {
			hasTexture = 1
		}
		if p.hasTexture != hasTexture {
			gl.Uniform1i(uniformLocationHasTexture, hasTexture)
			p.hasTexture = hasTexture
		}
		if c.texture != 0 && p.texture != c.texture {
			gl.BindTexture(gl.TEXTURE_2D, c.texture)
			p.texture = c.texture
		}
		switch c.mode {
		case mtRect:
			gl.DrawElementsWithOffset(gl.TRIANGLES, c.len, gl.UNSIGNED_INT, uintptr(c.offset))
		case mtLine:
			gl.DrawElementsWithOffset(gl.LINES, c.len, gl.UNSIGNED_INT, uintptr(c.offset))
		}
	}
}

func (p *DrawPass) End() {
	if !p.bound {
		return
	}
	gl.BindVertexArray(0)
	gl.UseProgram(0)
	p.bound = false
}

// APHELION EDIT ADDITION END

func Draw(w, h, x, y, z float32) {
	/* APHELION EDIT REMOVAL START - SHARED BRUSH PASS
	// Ensure that the latest batch state is persisted.
	batching.flush()

	// No data to draw.
	if len(batching.data) == 0 {
		return
	}

	gl.UseProgram(program)
	gl.BindVertexArray(vao)

	mtxTransform := transformationMatrix(w, h, x, y, z)
	gl.UniformMatrix4fv(uniformLocationTransform, 1, false, &mtxTransform[0])

	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(batching.data)*platform.FloatSize, gl.Ptr(batching.data), gl.STREAM_DRAW)
	gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, ebo)
	gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(batching.indices)*platform.FloatSize, gl.Ptr(batching.indices), gl.STREAM_DRAW)

	for _, c := range batching.calls {
		if c.texture != 0 {
			gl.Uniform1i(uniformLocationHasTexture, 1)
			gl.BindTexture(gl.TEXTURE_2D, c.texture)
		} else {
			gl.Uniform1i(uniformLocationHasTexture, 0)
		}

		switch c.mode {
		case mtRect:
			gl.DrawElementsWithOffset(gl.TRIANGLES, c.len, gl.UNSIGNED_INT, uintptr(c.offset))
		case mtLine:
			gl.DrawElementsWithOffset(gl.LINES, c.len, gl.UNSIGNED_INT, uintptr(c.offset))
		}
	}

	gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, 0)
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)
	gl.UseProgram(0)

	// Clear batch state.
	batching.clear()
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - SHARED BRUSH PASS
	pass := NewDrawPass(w, h, x, y, z)
	defer pass.End()
	pass.Flush()
	// APHELION EDIT ADDITION END
}

func transformationMatrix(w, h, x, y, z float32) mgl32.Mat4 {
	view := mgl32.Ortho(0, w, 0, h, -1, 1)
	scale := mgl32.Scale2D(z, z).Mat4()
	shift := mgl32.Translate2D(x, y).Mat4()
	return view.Mul4(scale).Mul4(shift)
}
