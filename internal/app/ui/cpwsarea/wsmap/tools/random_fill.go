// APHELION EDIT ADDITION START - DETERMINISTIC RANDOM FILL
package tools

import (
	"crypto/rand"
	"encoding/binary"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
	"strconv"
)

type randomFillOwner interface {
	RandomFillSettings() *editing.MapperSettings
	StartRandomFill(editing.Selection, editing.RandomPalette, uint64, float64, util.Point) error
}

func (t *ToolFill) beginRandomFill() bool {
	t.random = false
	owner, ok := ed.(randomFillOwner)
	if !ok {
		return true
	}
	settings := owner.RandomFillSettings()
	if settings == nil || !settings.RandomFill {
		return true
	}
	if _, err := settings.Palette.Compile(); err != nil {
		util.ShowErrorDialog(err.Error())
		return false
	}
	seed, err := strconv.ParseUint(settings.Seed, 10, 64)
	if !settings.SeedLock {
		var bytes [8]byte
		_, err = rand.Read(bytes[:])
		seed = binary.LittleEndian.Uint64(bytes[:])
		settings.Seed = strconv.FormatUint(seed, 10)
	}
	if err != nil {
		util.ShowErrorDialog("Seed must be an unsigned integer.")
		return false
	}
	t.random = true
	t.palette = settings.Palette.Clone()
	t.seed = seed
	t.density = settings.Density
	return true
}

// APHELION EDIT ADDITION END
