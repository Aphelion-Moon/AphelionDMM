package iconassets

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sdmm/internal/aphelion/resources"
	"sdmm/third_party/sdmmparser"
	"testing"
	"time"
)

func TestDecodeClassifiesMissingIconAsPermanentAssetFailure(t *testing.T) {
	_, err := Decode(context.Background(), filepath.Join(t.TempDir(), "missing.dmi"))
	if err == nil {
		t.Fatal("missing icon unexpectedly decoded")
	}
	var failure interface {
		Category() string
		Stage() string
	}
	if !errors.As(err, &failure) {
		t.Fatalf("missing icon failure is not classified: %T: %v", err, err)
	}
	if failure.Category() != string(FailureInvalid) || failure.Stage() != "open" {
		t.Fatalf("missing icon classified as %q at %q, want invalid open failure", failure.Category(), failure.Stage())
	}
}

func TestDecodeClassifiesByteAdmissionAsTransient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.dmi")
	writeIconFixture(t, path)

	budget := resources.NewFixedBudget(8192)
	reservation, err := budget.Reserve(1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeWithBudget(context.Background(), path, budget)
	if err == nil {
		t.Fatal("decode unexpectedly passed while the icon budget was full")
	}
	var failure interface {
		Category() string
		Stage() string
	}
	if !errors.As(err, &failure) || failure.Category() != string(FailureTransient) || failure.Stage() != "memory admission" {
		t.Fatalf("admission failure was not classified as transient at memory admission: %T: %v", err, err)
	}
	reservation.Release()
	decoded, err := DecodeWithBudget(context.Background(), path, budget)
	if err != nil {
		t.Fatalf("decode did not recover after temporary admission pressure cleared: %v", err)
	}
	decoded.Release()
}

func TestDecodeClassifiesOversizedIconBeforeBudgetAdmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.dmi")
	writePNGHeader(t, path, 8192, 4097)
	_, err := DecodeWithBudget(context.Background(), path, resources.NewFixedBudget(1))
	if err == nil {
		t.Fatal("oversized icon unexpectedly reached decode admission")
	}
	var failure interface {
		Category() string
		Stage() string
	}
	if !errors.As(err, &failure) || failure.Category() != string(FailureOversized) || failure.Stage() != "memory admission" {
		t.Fatalf("oversized icon was not classified at memory admission: %T: %v", err, err)
	}
}

func writeIconFixture(t *testing.T, path string) {
	t.Helper()
	raster := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	var encoded, compressed bytes.Buffer
	if err := png.Encode(&encoded, raster); err != nil {
		t.Fatal(err)
	}
	zipper := zlib.NewWriter(&compressed)
	if _, err := zipper.Write([]byte("# BEGIN DMI\nversion = 4.0\n\twidth = 32\n\theight = 32\nstate = \"\"\n\tdirs = 1\n\tframes = 1\n# END DMI\n")); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("Description\x00\x00"), compressed.Bytes()...)
	var chunk bytes.Buffer
	if err := binary.Write(&chunk, binary.BigEndian, uint32(len(payload))); err != nil {
		t.Fatal(err)
	}
	chunk.WriteString("zTXt")
	chunk.Write(payload)
	if err := binary.Write(&chunk, binary.BigEndian, crc32.ChecksumIEEE(chunk.Bytes()[4:])); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	output := append([]byte{}, data[:33]...)
	output = append(output, chunk.Bytes()...)
	output = append(output, data[33:]...)
	if err := os.WriteFile(path, output, 0600); err != nil {
		t.Fatal(err)
	}
}

func writePNGHeader(t *testing.T, path string, width, height uint32) {
	t.Helper()
	var output bytes.Buffer
	output.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	writeChunk := func(kind string, payload []byte) {
		if err := binary.Write(&output, binary.BigEndian, uint32(len(payload))); err != nil {
			t.Fatal(err)
		}
		var chunk bytes.Buffer
		chunk.WriteString(kind)
		chunk.Write(payload)
		output.Write(chunk.Bytes())
		if err := binary.Write(&output, binary.BigEndian, crc32.ChecksumIEEE(chunk.Bytes())); err != nil {
			t.Fatal(err)
		}
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // RGBA
	writeChunk("IHDR", ihdr)
	if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledDecodedHandoffReleasesReservation(t *testing.T) {
	budget := resources.NewFixedBudget(1024)
	reservation, err := budget.Reserve(512)
	if err != nil {
		t.Fatal(err)
	}
	decoded := make(chan struct{})
	w := newWorker(func(context.Context, string) (*Decoded, error) {
		close(decoded)
		return &Decoded{Reservation: reservation}, nil
	})
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	if !w.Submit(Request{Context: ctx, Key: "old", Path: "old"}) {
		t.Fatal("not submitted")
	}
	<-decoded
	cancel()
	deadline := time.Now().Add(time.Second)
	for budget.Used() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if budget.Used() != 0 {
		t.Fatal("cancelled handoff retained decoded memory")
	}
}

func TestWorkerDoesNotBufferRequestsAheadOfVisibleDemand(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	w := newWorker(func(context.Context, string) (*Decoded, error) {
		close(started)
		<-release
		return nil, nil
	})
	defer w.Close()

	ctx := context.Background()
	if !w.Submit(Request{Context: ctx, Key: "active", Path: "active"}) {
		t.Fatal("worker did not accept the first request")
	}
	<-started
	if w.Submit(Request{Context: ctx, Key: "queued", Path: "queued"}) {
		t.Fatal("worker buffered a background request ahead of later visible demand")
	}

	close(release)
	if result := <-w.Results; result.Key != "active" {
		t.Fatalf("worker returned %q, want active request", result.Key)
	}
}

func TestMetadataCannotReadPastDecodedImage(t *testing.T) {
	for _, metadata := range []*sdmmparser.IconMetadata{
		nil,
		{Width: 32, Height: 32, States: []*sdmmparser.IconState{{Dirs: 8, Frames: 2}}},
		{Width: 32, Height: 32, States: []*sdmmparser.IconState{{Dirs: 3, Frames: 1}}},
	} {
		if validateMetadata(metadata, 32, 32) == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	if err := validateMetadata(&sdmmparser.IconMetadata{Width: 32, Height: 32, States: []*sdmmparser.IconState{{Dirs: 1, Frames: 1}}}, 32, 32); err != nil {
		t.Fatal(err)
	}
}
