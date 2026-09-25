package dmmclip

import (
	"sort"

	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"

	"github.com/rs/zerolog/log"
)

type PasteData struct {
	// APHELION EDIT ADDITION START - CLIPBOARD PROVENANCE
	EnvironmentHash string
	// APHELION EDIT ADDITION END
	Filter dm.PathsFilter
	Buffer []dmmap.Tile
}

// Clipboard is a global storage for tiles to provide a copy/paste experience.
type Clipboard struct {
	pasteData PasteData
}

func New() *Clipboard {
	return &Clipboard{}
}

func (c *Clipboard) Free() {
	c.pasteData = PasteData{}
	log.Print("clipboard free")
}

func (c *Clipboard) Copy(pathsFilter *dm.PathsFilter, dmm *dmmap.Dmm, tiles []util.Point) {
	if len(tiles) == 0 {
		return
	}
	// APHELION EDIT ADDITION START - CLIPBOARD PROVENANCE
	c.pasteData.EnvironmentHash = ""
	// APHELION EDIT ADDITION END

	// APHELION EDIT CHANGE - BOUNDED CLIPBOARD LOGGING - ORIGINAL: log.Printf("copy tiles to the clipboard buffer: %v", tiles)
	log.Debug().Int("tiles", len(tiles)).Msg("copy tiles to the clipboard buffer")

	c.pasteData.Filter = pathsFilter.Copy()
	c.pasteData.Buffer = make([]dmmap.Tile, 0, len(tiles))

	for _, pos := range tiles {
		if !dmm.HasTile(pos) {
			continue
		}

		tile := dmm.GetTile(pos).Copy()

		var prefabs dmmdata.Prefabs
		for _, instance := range tile.Instances() {
			if pathsFilter.IsVisiblePath(instance.Prefab().Path()) {
				prefabs = append(prefabs, instance.Prefab())
			}
		}

		tile.InstancesSet(prefabs)

		c.pasteData.Buffer = append(c.pasteData.Buffer, tile)
	}

	sort.SliceStable(c.pasteData.Buffer, func(i, j int) bool {
		return c.pasteData.Buffer[i].Coord.Y < c.pasteData.Buffer[j].Coord.Y
	})
	sort.SliceStable(c.pasteData.Buffer, func(i, j int) bool {
		return c.pasteData.Buffer[i].Coord.X < c.pasteData.Buffer[j].Coord.X
	})
}

// APHELION EDIT ADDITION START - CLIPBOARD PROVENANCE
func (c *Clipboard) SetEnvironmentHash(hash string) { c.pasteData.EnvironmentHash = hash }

// APHELION EDIT ADDITION END

func (c *Clipboard) Buffer() PasteData {
	return c.pasteData
}

func (c *Clipboard) HasData() bool {
	return len(c.pasteData.Buffer) != 0
}
