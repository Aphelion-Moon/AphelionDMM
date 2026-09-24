package dmmdata

import (
	// APHELION EDIT ADDITION START - CONTENT IDENTITY
	"sdmm/internal/aphelion/prefabidentity"
	// APHELION EDIT ADDITION END
	"sort"
	"strconv"
	"strings"

	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

type Prefabs []*dmmprefab.Prefab

func (p Prefabs) Copy() Prefabs {
	cpy := make(Prefabs, len(p))
	copy(cpy, p)
	return cpy
}

func (p Prefabs) Equals(prefabs Prefabs) bool {
	if len(p) != len(prefabs) {
		return false
	}

	for idx, prefab := range p {
		// APHELION EDIT CHANGE - CONTENT IDENTITY - ORIGINAL: if prefab.Id() != prefabs[idx].Id() {
		if !prefab.Equals(prefabs[idx]) {
			return false
		}
	}

	return true
}

func (p Prefabs) Hash() uint64 {
	sb := strings.Builder{}
	for _, prefab := range p {
		// APHELION EDIT CHANGE - CONTENT IDENTITY - ORIGINAL: sb.WriteString(strconv.FormatUint(prefab.Id(), 10))
		content := prefabidentity.Key(prefab.Path(), prefab.Vars())
		// APHELION EDIT ADDITION START - CONTENT IDENTITY
		sb.WriteString(strconv.Itoa(len(content)))
		sb.WriteByte(':')
		sb.WriteString(content)
		// APHELION EDIT ADDITION END
	}
	return util.Djb2(sb.String())
}

func (p Prefabs) Sorted() Prefabs {
	sorted := p.Copy()
	sort.SliceStable(sorted, func(i, j int) bool {
		return dm.PathWeight(sorted[i].Path()) < dm.PathWeight(sorted[j].Path())
	})
	return sorted
}
