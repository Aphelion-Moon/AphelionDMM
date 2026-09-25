package mapping

import (
	"context"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
	"strconv"
)

// OccurrenceLayer separates selected/nested contributions for a translated
// draft, or masks them out of an editable source's locked backdrop. It does not
// reconstruct unknown substrate or mutate the projected or accepted source.
func (p *Projection) OccurrenceLayer(ctx context.Context, id string, include bool) (*Source, error) {
	ids := map[string]bool{id: true}
	for _, root := range p.Roots {
		if ids[root.Parent] {
			ids[root.ID] = true
		}
	}
	var count uint64
	for _, key := range p.Source.grid {
		count += uint64(len(p.Source.dictionary[key]))
	}
	lease, err := resources.DefaultBudget().Reserve(uint64(len(p.Source.grid))*256 + count*128 + 1<<20)
	if err != nil {
		return nil, err
	}
	s := &Source{Identity: p.Source.Identity, Size: p.Source.Size, Origin: p.Source.Origin, grid: map[util.Point]dmmdata.Key{}, dictionary: map[dmmdata.Key][]Atom{}, lease: lease}
	for point, key := range p.Source.grid {
		if err := ctx.Err(); err != nil {
			s.Close()
			return nil, err
		}
		var atoms []Atom
		for _, a := range p.Source.dictionary[key] {
			if ids[a.Occurrence] == include {
				atoms = append(atoms, a)
			}
		}
		if len(atoms) == 0 {
			continue
		}
		k := dmmdata.Key(strconv.Itoa(len(s.dictionary)))
		s.grid[point] = k
		s.dictionary[k] = atoms
	}
	return s, nil
}
