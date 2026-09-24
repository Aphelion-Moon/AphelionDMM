// Package iconassets separates owned CPU icon preparation from UI texture work.
package iconassets

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	_ "image/png"
	"math"
	"os"

	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/aphelion/resources"
	"sdmm/third_party/sdmmparser"
)

type Decoded struct {
	Metadata    *sdmmparser.IconMetadata
	Image       *image.NRGBA
	Reservation *resources.Reservation
}

func (d *Decoded) Release() {
	if d != nil {
		d.Reservation.Release()
	}
}

func Decode(ctx context.Context, path string) (_ *Decoded, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	config, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if config.Width < 1 || config.Height < 1 || uint64(config.Width) > math.MaxUint64/uint64(config.Height)/8 {
		return nil, fmt.Errorf("invalid icon dimensions")
	}
	reservation, err := resources.DefaultBudget().Reserve(uint64(config.Width) * uint64(config.Height) * 8)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			reservation.Release()
		}
	}()
	metadataTrace := uistage.Begin(uistage.IconMetadata)
	metadata, err := sdmmparser.ParseIconMetadata(path)
	metadataTrace.End()
	if err != nil {
		return nil, err
	}
	if err = validateMetadata(metadata, config.Width, config.Height); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if _, err = f.Seek(0, 0); err != nil {
		return nil, err
	}
	decodeTrace := uistage.Begin(uistage.IconDecode)
	decoded, _, err := image.Decode(f)
	decodeTrace.End()
	if err != nil {
		return nil, err
	}
	if decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return nil, fmt.Errorf("icon dimensions changed during decoding")
	}
	rgba := image.NewNRGBA(image.Rect(0, 0, config.Width, config.Height))
	draw.Draw(rgba, rgba.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &Decoded{Metadata: metadata, Image: rgba, Reservation: reservation}, nil
}

func validateMetadata(metadata *sdmmparser.IconMetadata, width, height int) error {
	if metadata == nil || metadata.Width < 1 || metadata.Height < 1 || width%metadata.Width != 0 || height%metadata.Height != 0 {
		return fmt.Errorf("icon metadata does not match image dimensions")
	}
	available := int64(width/metadata.Width) * int64(height/metadata.Height)
	used := int64(0)
	for _, state := range metadata.States {
		if state == nil || state.Frames < 1 || (state.Dirs != 1 && state.Dirs != 4 && state.Dirs != 8) {
			return fmt.Errorf("invalid icon state dimensions")
		}
		if int64(state.Frames) > (available-used)/int64(state.Dirs) {
			return fmt.Errorf("icon state exceeds image frame count")
		}
		frames := int64(state.Frames) * int64(state.Dirs)
		used += frames
	}
	if used == 0 {
		return fmt.Errorf("icon has no frames")
	}
	return nil
}
