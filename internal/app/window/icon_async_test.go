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
	"runtime"
	"testing"
	"time"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/iconassets"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
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

// APHELION EDIT ADDITION START - ICON RECOVERY
func TestNativeVisibleIconFailureRetriesIntoSameHandle(t *testing.T) {
	newMouseNetworkWorkspace(t)
	root := t.TempDir()
	dmicon.Cache.Free()
	dmicon.Cache.SetRootDirPath(root)
	t.Cleanup(func() { dmicon.Cache.Free(); window.DrainFrameJobsForTest() })

	sprite, status := dmicon.Cache.RequestSpriteV("fixture.dmi", "", 0, dmicon.RequestVisible)
	if status.State != dmicon.SpritePending {
		t.Fatalf("missing icon state is %d, want pending", status.State)
	}
	deadline := time.Now().Add(5 * time.Second)
	for dmicon.Cache.Loading() && time.Now().Before(deadline) {
		dmicon.Cache.ProcessUploads()
		time.Sleep(time.Millisecond)
	}
	if dmicon.Cache.Loading() {
		t.Fatal("missing icon did not reach an actionable failure")
	}
	failedSprite, status := dmicon.Cache.RequestSpriteV("fixture.dmi", "", 0, dmicon.RequestVisible)
	if failedSprite != sprite || status.State != dmicon.SpriteFailed || status.Category != "invalid" {
		t.Fatalf("missing icon outcome = handle same:%t state:%d category:%q; want same handle and invalid failure", failedSprite == sprite, status.State, status.Category)
	}

	writeIconFixture(t, root, 64)
	if !dmicon.Cache.RetryIcon("fixture.dmi") {
		t.Fatal("explicit retry was not accepted")
	}
	retryingSprite, status := dmicon.Cache.RequestSpriteV("fixture.dmi", "", 0, dmicon.RequestVisible)
	if retryingSprite != sprite || status.State != dmicon.SpritePending {
		t.Fatalf("retry outcome = handle same:%t state:%d; want same handle and pending", retryingSprite == sprite, status.State)
	}
	deadline = time.Now().Add(5 * time.Second)
	for dmicon.Cache.Loading() && time.Now().Before(deadline) {
		dmicon.Cache.ProcessUploads()
		time.Sleep(time.Millisecond)
	}
	if dmicon.Cache.Loading() {
		t.Fatal("retried visible icon did not finish")
	}
	readySprite, status := dmicon.Cache.RequestSpriteV("fixture.dmi", "", 0, dmicon.RequestVisible)
	if readySprite != sprite || status.State != dmicon.SpriteReady || sprite.IconWidth() != 64 || sprite.Texture() == 0 {
		t.Fatalf("retried icon did not publish into its original handle: same=%t state=%d width=%d texture=%d", readySprite == sprite, status.State, sprite.IconWidth(), sprite.Texture())
	}
}

// APHELION EDIT ADDITION START - REAL CATALOGUE ICON
// This opt-in native smoke check creates no map or workspace: the environment
// icon is an independent foreground consumer of the shared upload scheduler.
func TestNativeCatalogueIconLoadsWithoutMapMembership(t *testing.T) {
	dmePath := os.Getenv("APHELION_ICON_DME")
	if dmePath == "" {
		t.Skip("set APHELION_ICON_DME to a readable project DME for the catalogue icon smoke check")
	}
	if lifecycleWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for native icon upload")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	lifecycleWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)
	t.Cleanup(window.DrainFrameJobsForTest)

	parseStart := time.Now()
	environment, err := dmenv.New(dmePath)
	if err != nil {
		t.Fatal("parse icon catalogue DME:", err)
	}
	t.Logf("catalogue_dme=%s parse_ms=%.3f", dmePath, float64(time.Since(parseStart).Microseconds())/1000)

	const objectPath = "/obj/machinery/door/airlock"
	object := environment.Objects[objectPath]
	if object == nil {
		t.Fatalf("catalogue object %s is missing from %s", objectPath, dmePath)
	}
	iconPath, ok := object.Vars.Text("icon")
	if !ok || iconPath == "" {
		t.Fatalf("catalogue object %s has no inherited icon path", objectPath)
	}
	state, _ := object.Vars.Text("icon_state")
	direction := object.Vars.IntV("dir", dm.DirDefault)

	dmicon.Cache.Free()
	dmicon.Cache.SetRootDirPath(environment.RootDir)
	defer func() {
		dmicon.Cache.Free()
		window.DrainFrameJobsForTest()
	}()
	started := time.Now()
	sprite, status := dmicon.Cache.RequestSpriteV(iconPath, state, direction, dmicon.RequestVisible)
	if sprite == nil {
		t.Fatal("visible icon request returned a nil handle")
	}
	deadline := time.Now().Add(90 * time.Second)
	for status.State != dmicon.SpriteReady && time.Now().Before(deadline) {
		dmicon.Cache.ProcessUploads()
		current, currentStatus := dmicon.Cache.RequestSpriteV(iconPath, state, direction, dmicon.RequestVisible)
		if current != sprite {
			t.Fatal("visible icon request replaced its stable handle")
		}
		status = currentStatus
		if status.State == dmicon.SpriteFailed {
			t.Fatalf("catalogue icon failed: category=%s stage=%s err=%v", status.Category, status.Stage, status.Err)
		}
		time.Sleep(time.Millisecond)
	}
	latency := time.Since(started)
	if status.State != dmicon.SpriteReady || sprite.Texture() == 0 || sprite.IconWidth() <= 0 || sprite.IconHeight() <= 0 {
		t.Fatalf("catalogue icon did not become ready: state=%d texture=%d dimensions=%dx%d", status.State, sprite.Texture(), sprite.IconWidth(), sprite.IconHeight())
	}
	t.Logf("object=%s icon=%s state=%q dir=%d latency_ms=%.3f cache_revision=%d dimensions=%dx%d", objectPath, iconPath, state, direction, float64(latency.Microseconds())/1000, dmicon.Cache.Revision(), sprite.IconWidth(), sprite.IconHeight())
}

// APHELION EDIT ADDITION END

// APHELION EDIT ADDITION END

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
