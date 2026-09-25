package mapping

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"sdmm/internal/aphelion/resources"
	"sdmm/internal/util"
)

var seamDirections = []struct{ dx, dy, bit, opposite int }{{0, 1, 1, 2}, {0, -1, 2, 1}, {1, 0, 4, 8}, {-1, 0, 8, 4}}

func contributionOwner(c *Contribution) string {
	if c == nil {
		return ""
	}
	return c.Source + "|" + c.Occurrence + "|" + c.Phase
}
func seamOwners(c *CellProvenance) string {
	if c == nil {
		return ""
	}
	owners := []string{contributionOwner(c.Turf), contributionOwner(c.Area), c.ReservedBy}
	seen := map[string]bool{}
	for i := range c.Objects {
		owner := contributionOwner(&c.Objects[i])
		if !seen[owner] {
			owners = append(owners, owner)
			seen[owner] = true
		}
	}
	sort.Strings(owners)
	return strings.Join(owners, "\n")
}

// AnalyzeSeams examines a one-cell border of actual selected contributions.
// Alternative candidates are never assumed to coexist. Every report carries
// a concrete authored-scenario witness, not a live network/pathfinding claim.
func (p *Projection) AnalyzeSeams(ctx context.Context) ([]Diagnostic, error) {
	lease, err := resources.DefaultBudget().Reserve(uint64(len(p.Provenance))*160 + 8<<20)
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	owners := make(map[util.Point]string, len(p.Provenance))
	for point, provenance := range p.Provenance {
		owners[point] = seamOwners(provenance)
	}
	border := map[util.Point]bool{}
	for point, owner := range owners {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, d := range seamDirections {
			n := util.Point{X: point.X + d.dx, Y: point.Y + d.dy, Z: point.Z}
			if other, present := owners[n]; present && other != owner {
				border[point] = true
				border[n] = true
			}
		}
		if len(border) > 100000 {
			return []Diagnostic{{Severity: "error", Code: "seam-budget", Message: "Selected boundary exceeds 100000 cells; seam assessment incomplete"}}, nil
		}
	}
	points := make([]util.Point, 0, len(border))
	for point := range border {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool {
		a, b := points[i], points[j]
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	var result []Diagnostic
	groups := map[string]int{}
	add := func(point util.Point, objectIndex int, severity, code, message string) {
		cell := p.Provenance[point]
		witness := cell.Turf
		if objectIndex >= 0 && objectIndex < len(cell.Objects) {
			witness = &cell.Objects[objectIndex]
		}
		if witness == nil && len(cell.Objects) > 0 {
			witness = &cell.Objects[0]
		}
		if witness == nil {
			witness = &Contribution{Source: p.Source.Identity.Path, Local: point}
		}
		key := code + "|" + witness.Source + "|" + witness.Occurrence + "|" + message
		if i, ok := groups[key]; ok {
			result[i].Count++
			return
		}
		if len(result) >= 511 {
			if len(result) == 511 {
				result = append(result, Diagnostic{Severity: "error", Code: "seam-budget", Message: "Seam report limit reached; remaining warnings omitted", Source: witness.Source, Local: witness.Local, Destination: point})
			}
			return
		}
		groups[key] = len(result)
		result = append(result, Diagnostic{Severity: severity, Code: code, Message: message, Count: 1, Source: witness.Source, Occurrence: witness.Occurrence, Local: witness.Local, Destination: point})
	}
	s := p.Source
	for _, point := range points {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		closed, known := s.turfIs(point, "/turf/closed")
		if known && closed {
			for _, d := range seamDirections {
				n := util.Point{X: point.X + d.dx, Y: point.Y + d.dy, Z: point.Z}
				if owners[n] == owners[point] {
					continue
				}
				floor, known := s.turfIs(n, "/turf/open/floor")
				if known && floor {
					add(point, -1, "info", "seam-substrate", "Authored wall/floor discontinuity across selected contributions; inspect approach intent.")
					break
				}
			}
		}
		objectIndex := -1
		for _, atom := range s.atomsAt(point) {
			if channel(atom) == "objects" {
				objectIndex++
			}
			if s.isType(atom.Path, "/obj/machinery/door") {
				floors, unknown := 0, false
				for _, d := range seamDirections {
					n := util.Point{X: point.X + d.dx, Y: point.Y + d.dy, Z: point.Z}
					floor, known := s.turfIs(n, "/turf/open/floor")
					if floor {
						floors++
					}
					unknown = unknown || !known
				}
				if floors < 2 {
					add(point, objectIndex, "warning", "seam-door", fmt.Sprintf("Door has %d authored floor approaches; unresolved neighbors=%t. Door initialization and passability are not simulated.", floors, unknown))
				}
			}
			if s.isType(atom.Path, "/obj/machinery/atmospherics") {
				mask, maskKnown := s.number(atom, "initialize_directions")
				layer, layerKnown := s.number(atom, "piping_layer")
				if !maskKnown || !layerKnown || mask < 0 || mask > 15 || mask != float64(int(mask)) {
					add(point, objectIndex, "info", "seam-pipe-unknown", "Atmos port initialization is unresolved from authored metadata; no network connection is inferred.")
				} else {
					for _, d := range seamDirections {
						if int(mask)&d.bit == 0 {
							continue
						}
						n := util.Point{X: point.X + d.dx, Y: point.Y + d.dy, Z: point.Z}
						reciprocal, uncertain := false, false
						for _, other := range s.atomsAt(n) {
							if !s.isType(other.Path, "/obj/machinery/atmospherics") {
								continue
							}
							otherMask, mk := s.number(other, "initialize_directions")
							otherLayer, lk := s.number(other, "piping_layer")
							if !mk || !lk || otherMask < 0 || otherMask > 15 || otherMask != float64(int(otherMask)) {
								uncertain = true
								continue
							}
							if int(otherMask)&d.opposite != 0 {
								reciprocal = true
								if otherLayer != layer {
									add(point, objectIndex, "warning", "seam-pipe-layer", "Reciprocal authored atmos ports have different piping layers; all-layer flags, colors and initialization require runtime confirmation.")
								}
							}
						}
						if !reciprocal {
							add(point, objectIndex, "warning", "seam-pipe", fmt.Sprintf("Authored atmos port %d lacks a resolved reciprocal neighbor; nearby unresolved ports=%t. Color/layer flags and runtime nodes are not simulated.", d.bit, uncertain))
						}
					}
				}
			}
			if s.isType(atom.Path, "/obj/structure/cable") {
				layer, known := s.number(atom, "cable_layer")
				banned, bk := s.number(atom, "banned_links")
				if !known || !bk || layer < 1 || layer > 7 {
					add(point, objectIndex, "info", "seam-cable-unknown", "Cable layer/link exclusions are unresolved; visual direction is not a live connection.")
				} else {
					compatible, unknown := false, false
					for _, d := range seamDirections {
						if int(banned)&d.bit != 0 {
							continue
						}
						n := util.Point{X: point.X + d.dx, Y: point.Y + d.dy, Z: point.Z}
						for _, other := range s.atomsAt(n) {
							if !s.isType(other.Path, "/obj/structure/cable") {
								continue
							}
							ol, ok := s.number(other, "cable_layer")
							ob, okb := s.number(other, "banned_links")
							if !ok || !okb {
								unknown = true
								continue
							}
							if int(layer)&int(ol) != 0 && int(ob)&d.opposite == 0 {
								compatible = true
							}
						}
					}
					if !compatible {
						add(point, objectIndex, "warning", "seam-cable", fmt.Sprintf("No authored neighboring cable shares an allowed layer; unresolved neighbors=%t. Terminals, vertical links and powernets require runtime confirmation.", unknown))
					}
				}
			}
			if s.inTypes(atom.Path, []string{"/obj/machinery/atmospherics/pipe/multiz", "/obj/structure/cable/multilayer/multiz", "/obj/structure/stairs", "/obj/structure/ladder", "/turf/open/openspace"}) {
				add(point, objectIndex, "info", "seam-deck-unknown", "Deck continuity is unresolved: local source Z and trait-relative placement do not establish adjacent runtime world decks.")
			}
		}
	}
	return result, nil
}
