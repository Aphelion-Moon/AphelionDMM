package window_test

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-gl/gl/v3.3-core/gl"
	"sdmm/internal/aphelion/iconassets"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmicon"
)

func writeIconFixture(t *testing.T, root string, width int) {
	t.Helper()
	raster := image.NewNRGBA(image.Rect(0, 0, width, 64))
	for index := 0; index < len(raster.Pix); index += 4 {
		raster.Pix[index] = 91
		raster.Pix[index+3] = 255
	}
	var encoded, compressed bytes.Buffer
	if err := png.Encode(&encoded, raster); err != nil {
		t.Fatal(err)
	}
	zipper := zlib.NewWriter(&compressed)
	if _, err := zipper.Write([]byte("# BEGIN DMI\nversion = 4.0\n\twidth = 64\n\theight = 64\nstate = \"\"\n\tdirs = 1\n\tframes = 1\n# END DMI\n")); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("Description\x00\x00"), compressed.Bytes()...)
	var chunk bytes.Buffer
	_ = binary.Write(&chunk, binary.BigEndian, uint32(len(payload)))
	chunk.WriteString("zTXt")
	chunk.Write(payload)
	_ = binary.Write(&chunk, binary.BigEndian, crc32.ChecksumIEEE(chunk.Bytes()[4:]))
	data := encoded.Bytes()
	output := append([]byte{}, data[:33]...)
	output = append(output, chunk.Bytes()...)
	output = append(output, data[33:]...)
	if err := os.WriteFile(filepath.Join(root, "fixture.dmi"), output, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeAsyncIconResolvesCachedHandleAndCancelsOldRoot(t *testing.T) {
	newMouseNetworkWorkspace(t)
	root := t.TempDir()
	writeIconFixture(t, root, 64)
	dmicon.Cache.Free()
	dmicon.Cache.SetRootDirPath(root)
	t.Cleanup(func() { dmicon.Cache.Free(); window.DrainFrameJobsForTest() })
	sprite := dmicon.Cache.GetSpriteOrPlaceholder("fixture.dmi", "")
	if !dmicon.Cache.Loading() {
		t.Fatal("cold load did not queue")
	}
	deadline := time.Now().Add(5 * time.Second)
	for dmicon.Cache.Loading() && time.Now().Before(deadline) {
		dmicon.Cache.ProcessUploads()
		time.Sleep(time.Millisecond)
	}
	if dmicon.Cache.Loading() {
		t.Fatal("load did not finish")
	}
	if sprite.IconWidth() != 64 || sprite.IconHeight() != 64 || sprite.Image().At(0, 0) != (color.NRGBA{R: 91, A: 255}) {
		t.Fatal("cached pending handle did not resolve", sprite.IconWidth())
	}
	var pixel [4]byte
	gl.BindTexture(gl.TEXTURE_2D, sprite.Texture())
	pixels := make([]byte, 64*64*4)
	gl.GetTexImage(gl.TEXTURE_2D, 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))
	copy(pixel[:], pixels[:4])
	if pixel != [4]byte{91, 0, 0, 255} {
		t.Fatal("uploaded pixels differ", pixel)
	}
	// Same key in a replaced root must not adopt the old worker completion.
	dmicon.Cache.Free()
	dmicon.Cache.SetRootDirPath(root)
	_, _ = dmicon.Cache.Get("fixture.dmi")
	dmicon.Cache.ProcessUploads()
	dmicon.Cache.SetRootDirPath(t.TempDir())
	_, _ = dmicon.Cache.Get("fixture.dmi")
	deadline = time.Now().Add(5 * time.Second)
	for dmicon.Cache.Loading() && time.Now().Before(deadline) {
		dmicon.Cache.ProcessUploads()
		time.Sleep(time.Millisecond)
	}
	if icon, err := dmicon.Cache.Get("fixture.dmi"); err == nil || icon != nil {
		t.Fatal("old root icon published into replacement")
	}
}

func TestNativeIconUploadYieldsAndRestoresUnpackState(t *testing.T) {
	newMouseNetworkWorkspace(t)
	image := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 8)
	gl.PixelStorei(gl.UNPACK_ROW_LENGTH, 7)
	defer gl.PixelStorei(gl.UNPACK_ALIGNMENT, 4)
	defer gl.PixelStorei(gl.UNPACK_ROW_LENGTH, 0)
	upload, err := iconassets.NewUpload(image)
	if err != nil {
		t.Fatal(err)
	}
	defer upload.Cancel()
	done, err := upload.Step()
	if err != nil || done {
		t.Fatal("upload did not yield", done, err)
	}
	var alignment, rowLength int32
	gl.GetIntegerv(gl.UNPACK_ALIGNMENT, &alignment)
	gl.GetIntegerv(gl.UNPACK_ROW_LENGTH, &rowLength)
	if alignment != 8 || rowLength != 7 {
		t.Fatal("unpack state not restored", alignment, rowLength)
	}
}
