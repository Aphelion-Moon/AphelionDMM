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
// Submission stores ordered immutable brush geometry in static GPU buffers.
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
	reservation, err := resources.DefaultBudget().Reserve(estimate)
	if err != nil {
		return nil, err
	}
	submission := CaptureSubmission(build)
	if submission == nil {
		reservation.Release()
	} else {
		submission.reservation = reservation
	}
	return submission, nil
}

// CaptureSubmission records brush primitives while leaving the frame batch intact.
func CaptureSubmission(build func()) *Submission {
	previous := batching
	captured := &Batching{}
	batching = captured
	defer func() { batching = previous }()

	build()
	captured.flush()
	if len(captured.data) == 0 {
		return nil
	}

	submission := &Submission{calls: append([]batchCall(nil), captured.calls...)}
	submission.bytes = len(captured.data)*platform.FloatSize + len(captured.indices)*4
	gl.GenVertexArrays(1, &submission.vao)
	gl.GenBuffers(1, &submission.vbo)
	gl.GenBuffers(1, &submission.ebo)
	gl.BindVertexArray(submission.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, submission.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(captured.data)*platform.FloatSize, gl.Ptr(captured.data), gl.STATIC_DRAW)
	gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, submission.ebo)
	gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(captured.indices)*4, gl.Ptr(captured.indices), gl.STATIC_DRAW)
	initAttributesFor(submission.vao, submission.vbo)
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
	if s == nil || s.vao == 0 || len(s.calls) == 0 {
		return
	}
	Draw(w, h, x, y, z)
	gl.UseProgram(program)
	gl.BindVertexArray(s.vao)
	mtxTransform := transformationMatrix(w, h, x, y, z)
	gl.UniformMatrix4fv(uniformLocationTransform, 1, false, &mtxTransform[0])
	for _, c := range s.calls {
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
	gl.BindVertexArray(0)
	gl.UseProgram(0)
}

// APHELION EDIT ADDITION END - RETAINED SUBMISSIONS

func Draw(w, h, x, y, z float32) {
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
}

func transformationMatrix(w, h, x, y, z float32) mgl32.Mat4 {
	view := mgl32.Ortho(0, w, 0, h, -1, 1)
	scale := mgl32.Scale2D(z, z).Mat4()
	shift := mgl32.Translate2D(x, y).Mat4()
	return view.Mul4(scale).Mul4(shift)
}
