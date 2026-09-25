package iconassets

import (
	"fmt"
	"github.com/go-gl/gl/v3.3-core/gl"
	"image"
	"sdmm/internal/aphelion/diagnostics/uistage"
)

// Upload is used exclusively by the graphics owner. A texture is not published
// until all rows and mipmaps are ready; cancellation can delete an unpublished ID.
type Upload struct {
	Texture uint32
	image   *image.NRGBA
	nextRow int
}

func NewUpload(image *image.NRGBA) (*Upload, error) {
	if image == nil || image.Bounds().Dx() < 1 || image.Bounds().Dy() < 1 {
		return nil, failure(FailureInvalid, "texture allocation", fmt.Errorf("empty icon image"))
	}
	restore := isolateUnpack()
	defer restore()
	var maxSize int32
	gl.GetIntegerv(gl.MAX_TEXTURE_SIZE, &maxSize)
	if image.Bounds().Dx() > int(maxSize) || image.Bounds().Dy() > int(maxSize) {
		return nil, failure(FailureOversized, "texture allocation", fmt.Errorf("icon exceeds graphics texture limit %d", maxSize))
	}
	u := &Upload{image: image}
	var previous int32
	gl.GetIntegerv(gl.TEXTURE_BINDING_2D, &previous)
	gl.GenTextures(1, &u.Texture)
	gl.BindTexture(gl.TEXTURE_2D, u.Texture)
	defer gl.BindTexture(gl.TEXTURE_2D, uint32(previous))
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST_MIPMAP_LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, int32(image.Bounds().Dx()), int32(image.Bounds().Dy()), 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	if code := gl.GetError(); code != gl.NO_ERROR {
		u.Cancel()
		return nil, failure(FailureTransient, "texture allocation", fmt.Errorf("texture allocation failed: 0x%x", code))
	}
	return u, nil
}
func (u *Upload) Step() (bool, error) {
	defer uistage.Begin(uistage.TextureUpload).End()
	restore := isolateUnpack()
	defer restore()
	var previous int32
	gl.GetIntegerv(gl.TEXTURE_BINDING_2D, &previous)
	gl.BindTexture(gl.TEXTURE_2D, u.Texture)
	defer gl.BindTexture(gl.TEXTURE_2D, uint32(previous))
	rows := max(1, (512<<10)/u.image.Stride)
	rows = min(rows, u.image.Bounds().Dy()-u.nextRow)
	if rows > 0 {
		pixels := u.image.Pix[u.nextRow*u.image.Stride:]
		gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, int32(u.nextRow), int32(u.image.Bounds().Dx()), int32(rows), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))
		u.nextRow += rows
	}
	if u.nextRow == u.image.Bounds().Dy() {
		gl.GenerateMipmap(gl.TEXTURE_2D)
	}
	if code := gl.GetError(); code != gl.NO_ERROR {
		return false, failure(FailureTransient, "texture upload", fmt.Errorf("texture upload failed: 0x%x", code))
	}
	return u.nextRow == u.image.Bounds().Dy(), nil
}

func isolateUnpack() func() {
	keys := [...]uint32{gl.UNPACK_ALIGNMENT, gl.UNPACK_ROW_LENGTH, gl.UNPACK_SKIP_ROWS, gl.UNPACK_SKIP_PIXELS}
	var saved [4]int32
	for index, key := range keys {
		gl.GetIntegerv(key, &saved[index])
		value := int32(0)
		if key == gl.UNPACK_ALIGNMENT {
			value = 1
		}
		gl.PixelStorei(key, value)
	}
	var buffer int32
	gl.GetIntegerv(gl.PIXEL_UNPACK_BUFFER_BINDING, &buffer)
	gl.BindBuffer(gl.PIXEL_UNPACK_BUFFER, 0)
	return func() {
		for index, key := range keys {
			gl.PixelStorei(key, saved[index])
		}
		gl.BindBuffer(gl.PIXEL_UNPACK_BUFFER, uint32(buffer))
	}
}
func (u *Upload) Cancel() {
	if u != nil && u.Texture != 0 {
		gl.DeleteTextures(1, &u.Texture)
		u.Texture = 0
	}
}
