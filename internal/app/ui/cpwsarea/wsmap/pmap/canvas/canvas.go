package canvas

import (
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	"sdmm/internal/aphelion/diagnostics/uistage"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/render"
	"sdmm/internal/app/window"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/rs/zerolog/log"
)

type Color struct {
	R, G, B, A float32
}

type Canvas struct {
	render *render.Render

	width, height float32

	frameBuffer uint32
	texture     uint32
	// APHELION EDIT ADDITION START - CANVAS LIFETIME
	disposeQueued bool
	// APHELION EDIT ADDITION END

	ClearColor Color
}

func (c *Canvas) Texture() uint32 {
	return c.texture
}

func (c *Canvas) Render() *render.Render {
	return c.render
}

func (c *Canvas) ReadPixels() []byte {
	// APHELION EDIT ADDITION START - CANVAS LIFETIME
	if c.texture == 0 {
		return nil
	}
	// APHELION EDIT ADDITION END
	var pixels = make([]byte, int(c.width*c.height*4))
	gl.BindTexture(gl.TEXTURE_2D, c.Texture())
	gl.GetTexImage(gl.TEXTURE_2D, 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))
	gl.BindTexture(gl.TEXTURE_2D, 0)
	return pixels
}

func (c *Canvas) Dispose() {
	// APHELION EDIT ADDITION START - CANVAS LIFETIME
	// UI-thread ownership, like Process. Keep handles readable for screenshot
	// capture and already-built draw commands until the next frame drains them.
	if c.disposeQueued {
		return
	}
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	if c.render != nil {
		c.render.CancelLevelBuilds()
	}
	// APHELION EDIT ADDITION END
	c.disposeQueued = true
	frameBuffer, texture := c.frameBuffer, c.texture
	// APHELION EDIT ADDITION END
	// Run later, so it will be cleared in the next frame.
	// Otherwise, we will see graphics artifacts.
	window.RunLater(func() {
		// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
		if c.render != nil {
			c.render.ReleaseRetainedSubmissions()
		}
		// APHELION EDIT ADDITION END
		log.Print("disposing...")
		// APHELION EDIT CHANGE - CANVAS LIFETIME - ORIGINAL: gl.DeleteFramebuffers(1, &c.frameBuffer)
		gl.DeleteFramebuffers(1, &frameBuffer)
		// APHELION EDIT CHANGE - CANVAS LIFETIME - ORIGINAL: gl.DeleteTextures(1, &c.texture)
		gl.DeleteTextures(1, &texture)
		// APHELION EDIT ADDITION START - CANVAS LIFETIME
		c.frameBuffer, c.texture = 0, 0
		c.width, c.height = 0, 0
		// APHELION EDIT ADDITION END
		log.Print("disposed")
	})
}

func New() *Canvas {
	c := &Canvas{render: render.New(), ClearColor: Color{.25, .25, .5, 1}}
	gl.GenFramebuffers(1, &c.frameBuffer)
	return c
}

func (c *Canvas) Process(size imgui.Vec2) {
	// APHELION EDIT ADDITION START - CANVAS LIFETIME
	if c.disposeQueued {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	defer uistage.Begin(uistage.CanvasDraw).End()
	// APHELION EDIT ADDITION END
	c.updateCanvasTexture(size.X, size.Y)
	gl.BindFramebuffer(gl.FRAMEBUFFER, c.frameBuffer)
	gl.Viewport(0, 0, int32(size.X), int32(size.Y))
	gl.ClearColor(c.ClearColor.R, c.ClearColor.G, c.ClearColor.B, c.ClearColor.A)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	c.render.Draw(size.X, size.Y)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}

func (c *Canvas) updateCanvasTexture(width, height float32) {
	if c.width != width || c.height != height || c.texture == 0 {
		c.width, c.height = width, height
		c.createCanvasTexture()
	}
}

func (c *Canvas) createCanvasTexture() {
	gl.DeleteTextures(1, &c.texture)
	gl.GenTextures(1, &c.texture)
	gl.BindTexture(gl.TEXTURE_2D, c.texture)
	// APHELION EDIT ADDITION START - CANVAS RESIZE ALLOCATION
	// Process clears the full framebuffer before drawing or exposing its texture.
	// No pixel unpack buffer is bound by the editor's rendering pipeline.
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - CANVAS RESIZE ALLOCATION - ORIGINAL: gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGB, int32(c.width), int32(c.height), 0, gl.RGB, gl.UNSIGNED_BYTE, gl.Ptr(make([]float32, int(c.width*c.height))))
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGB, int32(c.width), int32(c.height), 0, gl.RGB, gl.UNSIGNED_BYTE, nil)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.BindTexture(gl.TEXTURE_2D, 0)

	gl.BindFramebuffer(gl.FRAMEBUFFER, c.frameBuffer)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, c.texture, 0)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}
