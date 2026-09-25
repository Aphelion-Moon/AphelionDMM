package mapping

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"sdmm/internal/aphelion/resources"
	"sdmm/internal/util"
)

type SpawnRule struct {
	Path, Desired            string
	Targets, Over, Blacklist []string
	Mode, Amount             int
	Optional, IsOver         bool
	Unresolved               []string
}
type ScoredCell struct {
	Point   util.Point
	Score   int
	Unknown bool
	Reason  string
}
type TargetScore struct {
	Path                        string
	Eligible, Rejected, Unknown int
	Cells                       []ScoredCell
}
type SpawnAssessment struct {
	Rule           SpawnRule
	Targets        []TargetScore
	SelectedTarget string
	Status         string
}
type Advisory struct {
	Spawns      []SpawnAssessment
	Diagnostics []Diagnostic
	lease       *resources.Reservation
}

func (a *Advisory) Close() {
	if a != nil {
		a.lease.Release()
	}
}

type spawnScoringRules struct{ restricted, overlap, diagonalAllowed, flip []string }

func staticList(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "null" {
		return nil, nil
	}
	if !strings.HasPrefix(value, "list(") || !strings.HasSuffix(value, ")") {
		return nil, fmt.Errorf("unresolved list")
	}
	body := value[5 : len(value)-1]
	var result []string
	start := 0
	quoted, escaped := false, false
	add := func(text string) error {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		if strings.HasPrefix(text, "\"") {
			v, err := strconv.Unquote(text)
			if err != nil {
				return err
			}
			result = append(result, v)
			return nil
		}
		if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, "=() \t\r\n") {
			return fmt.Errorf("list contains dynamic or associated value")
		}
		result = append(result, text)
		return nil
	}
	for i := range body {
		ch := body[i]
		if escaped {
			escaped = false
			continue
		}
		if quoted && ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			quoted = !quoted
			continue
		}
		if ch == ',' && !quoted {
			if err := add(body[start:i]); err != nil {
				return nil, err
			}
			start = i + 1
		}
	}
	if quoted {
		return nil, fmt.Errorf("unterminated list string")
	}
	if err := add(body[start:]); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *Source) inTypes(path string, types []string) bool {
	for _, base := range types {
		if s.isType(path, base) {
			return true
		}
	}
	return false
}
func (s *Source) number(atom Atom, name string) (float64, bool) {
	value, ok := s.effective(atom, name)
	if !ok {
		return 0, false
	}
	number, err := strconv.ParseFloat(value, 64)
	return number, err == nil
}
func (s *Source) turfIs(p util.Point, base string) (bool, bool) {
	atoms, ok := s.grid[p]
	if !ok {
		return false, false
	}
	for _, a := range s.dictionary[atoms] {
		if channel(a) == "turf" {
			if a.Path == "/turf/template_noop" || s.Identity.Environment.Objects[a.Path] == nil {
				return false, false
			}
			return s.isType(a.Path, base), true
		}
	}
	return false, false
}
func (s *Source) scoreSpawn(p util.Point, mode int, r spawnScoringRules) ScoredCell {
	result := ScoredCell{Point: p}
	unknown := func(reason string) ScoredCell { result.Unknown = true; result.Reason = reason; return result }
	floor, known := s.turfIs(p, "/turf/open/floor")
	if !known {
		return unknown("effective floor/substrate unresolved")
	}
	if !floor {
		result.Reason = "not an authored floor"
		return result
	}
	empty := true
	for _, a := range s.atomsAt(p) {
		if channel(a) == "objects" && s.Identity.Environment.Objects[a.Path] == nil {
			return unknown("object inheritance unresolved for " + a.Path)
		}
		if channel(a) != "objects" || s.isType(a.Path, "/obj/effect") {
			continue
		}
		density, known := s.number(a, "density")
		if !known {
			return unknown("density unresolved for " + a.Path)
		}
		restricted := s.inTypes(a.Path, r.restricted)
		if mode == 2 {
			flip := s.inTypes(a.Path, r.flip)
			if (density != 0) != flip || (!flip && restricted) {
				result.Reason = "wall-mount density/restricted object: " + a.Path
				return result
			}
		} else if density != 0 || restricted || s.inTypes(a.Path, r.overlap) {
			result.Reason = "occupied/restricted by " + a.Path
			return result
		}
		layer, known := s.number(a, "layer")
		if !known {
			return unknown("layer unresolved for " + a.Path)
		}
		if layer > 2.5 && (mode == 2 || layer < 4.1) {
			empty = false
		}
	}
	directions := []util.Point{{X: 0, Y: 1}, {X: 0, Y: -1}, {X: 1, Y: 0}, {X: -1, Y: 0}}
	walls, dense, veryOpen := 0, 0, 0
	objectBlocked := false
	for _, d := range directions {
		n := util.Point{X: p.X + d.X, Y: p.Y + d.Y, Z: p.Z}
		closed, known := s.turfIs(n, "/turf/closed")
		if !known {
			return unknown("neighbor substrate unresolved")
		}
		if closed {
			walls++
			dense++
			continue
		}
		if mode == 1 {
			open, known := s.turfIs(util.Point{X: n.X + d.X, Y: n.Y + d.Y, Z: p.Z}, "/turf/open")
			if !known {
				return unknown("second neighbor substrate unresolved")
			}
			if open {
				veryOpen++
			}
		}
		for _, a := range s.atomsAt(n) {
			if channel(a) == "objects" && s.Identity.Environment.Objects[a.Path] == nil {
				return unknown("neighbor object inheritance unresolved for " + a.Path)
			}
			if channel(a) != "objects" || s.isType(a.Path, "/obj/effect") {
				continue
			}
			value, known := s.number(a, "density")
			if !known {
				return unknown("neighbor object density unresolved")
			}
			if value != 0 || s.inTypes(a.Path, r.restricted) {
				dense++
				objectBlocked = true
				break
			}
		}
	}
	diagonal := 0
	if mode == 1 {
		for _, d := range []util.Point{{X: 1, Y: 1}, {X: 1, Y: -1}, {X: -1, Y: 1}, {X: -1, Y: -1}} {
			for _, a := range s.atomsAt(util.Point{X: p.X + d.X, Y: p.Y + d.Y, Z: p.Z}) {
				if channel(a) == "objects" && s.Identity.Environment.Objects[a.Path] == nil {
					return unknown("diagonal object inheritance unresolved for " + a.Path)
				}
				if channel(a) != "objects" || s.isType(a.Path, "/obj/effect") || s.inTypes(a.Path, r.diagonalAllowed) {
					continue
				}
				value, known := s.number(a, "density")
				if !known {
					return unknown("diagonal density unresolved")
				}
				if value != 0 || s.inTypes(a.Path, r.restricted) {
					diagonal++
					break
				}
			}
		}
	}
	switch mode {
	case 0:
		result.Score = 4 - dense
		if empty {
			result.Score += 10
		}
	case 1:
		if walls == 0 || walls == 4 || objectBlocked {
			result.Reason = "hug-wall requires 1–3 walls and unblocked approaches"
			return result
		}
		result.Score = 400 - 100*diagonal + 10*walls + veryOpen
		if empty {
			result.Score += 1000
		}
	case 2:
		if walls == 0 || walls == 4 {
			result.Reason = "mount-wall requires 1–3 walls"
			return result
		}
		result.Score = 4 - walls
		if empty {
			result.Score += 10
		}
	default:
		return unknown("unknown scoring mode")
	}
	result.Reason = fmt.Sprintf("authored score: empty=%t, walls=%d, dense directions=%d, diagonal obstacles=%d, second open floors=%d; runtime initialization/RNG not evaluated", empty, walls, dense, diagonal, veryOpen)
	return result
}
func (s *Source) scoreOver(p util.Point, r SpawnRule) ScoredCell {
	result := ScoredCell{Point: p, Reason: "no authored host match"}
	for _, a := range s.atomsAt(p) {
		if s.Identity.Environment.Objects[a.Path] == nil {
			result.Unknown = true
			result.Reason = "host/existing-object inheritance unresolved for " + a.Path
			return result
		}
	}
	for _, a := range s.atomsAt(p) {
		if s.isType(a.Path, r.Desired) {
			result.Reason = "desired type already present"
			return result
		}
	}
	for _, a := range s.atomsAt(p) {
		if s.inTypes(a.Path, r.Over) {
			result.Score = 1
			result.Reason = "authored host " + a.Path + "; preceding area spawns and initialization may change this"
			return result
		}
	}
	return result
}

// AnalyzeSpawns implements the explicit Meridian advisory scoring contract.
// It never consumes shared runtime buckets, chooses random winners or spawns.
func (s *Source) AnalyzeSpawns(ctx context.Context, mapName string) (*Advisory, error) {
	lease, err := resources.DefaultBudget().Reserve(uint64(len(s.grid))*128 + 32<<20)
	if err != nil {
		return nil, err
	}
	a := &Advisory{lease: lease}
	success := false
	defer func() {
		if !success {
			a.Close()
		}
	}()
	metadata := Atom{Path: "/datum/controller/subsystem/area_spawn"}
	if s.Identity.Environment.Objects[metadata.Path] == nil {
		a.Diagnostics = append(a.Diagnostics, Diagnostic{Severity: "warning", Code: "spawn-adapter", Message: "Project has no recognized Meridian area-spawn metadata"})
		success = true
		return a, nil
	}
	rules := spawnScoringRules{}
	for name, destination := range map[string]*[]string{"restricted_objects_list": &rules.restricted, "restricted_overlap_objects_list": &rules.overlap, "allowed_diagonal_objects_list": &rules.diagonalAllowed, "flip_density_wall_mount_objects_list": &rules.flip} {
		value, _ := s.effective(metadata, name)
		*destination, err = staticList(value)
		if err != nil {
			return nil, fmt.Errorf("area-spawn rule %s: %w", name, err)
		}
	}
	areas := make(map[string][]util.Point)
	for _, point := range s.Points() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, atom := range s.atomsAt(point) {
			if channel(atom) == "area" {
				areas[atom.Path] = append(areas[atom.Path], point)
			}
		}
	}
	paths := make([]string, 0)
	for path := range s.Identity.Environment.Objects {
		if path != "/datum/area_spawn" && path != "/datum/area_spawn_over" && (s.isType(path, "/datum/area_spawn") || s.isType(path, "/datum/area_spawn_over")) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	scores := make(map[string][]ScoredCell)
	scanned := 0
	for _, path := range paths {
		if len(a.Spawns) >= 512 {
			a.Diagnostics = append(a.Diagnostics, Diagnostic{Severity: "error", Code: "spawn-budget", Message: "Area-spawn rule budget reached"})
			break
		}
		rule := SpawnRule{Path: path, IsOver: s.isType(path, "/datum/area_spawn_over")}
		atom := Atom{Path: path}
		for name, destination := range map[string]*[]string{"target_areas": &rule.Targets, "blacklisted_stations": &rule.Blacklist} {
			value, _ := s.effective(atom, name)
			list, e := staticList(value)
			if e != nil {
				rule.Unresolved = append(rule.Unresolved, name)
			} else {
				*destination = list
			}
		}
		rule.Desired, _ = s.effective(atom, "desired_atom")
		if rule.Desired == "null" || s.Identity.Environment.Objects[rule.Desired] == nil {
			rule.Unresolved = append(rule.Unresolved, "desired_atom")
		}
		if rule.IsOver {
			value, _ := s.effective(atom, "over_atoms")
			rule.Over, err = staticList(value)
			if err != nil {
				rule.Unresolved = append(rule.Unresolved, "over_atoms")
			}
		} else {
			for name, destination := range map[string]*int{"mode": &rule.Mode, "amount_to_spawn": &rule.Amount} {
				value, ok := s.number(atom, name)
				if !ok || value != float64(int(value)) {
					rule.Unresolved = append(rule.Unresolved, name)
				} else {
					*destination = int(value)
				}
			}
			value, known := s.number(atom, "optional")
			rule.Optional = value != 0
			if !known {
				rule.Unresolved = append(rule.Unresolved, "optional")
			}
		}
		assessment := SpawnAssessment{Rule: rule}
		if len(rule.Unresolved) > 0 {
			assessment.Status = "Unresolved metadata: " + strings.Join(rule.Unresolved, ", ")
			a.Spawns = append(a.Spawns, assessment)
			continue
		}
		if mapName != "" && slices.Contains(rule.Blacklist, mapName) {
			assessment.Status = "Station is blacklisted"
			a.Spawns = append(a.Spawns, assessment)
			continue
		}
		uncertainTarget := false
		for _, target := range rule.Targets {
			key := fmt.Sprintf("%s|%d", target, rule.Mode)
			if rule.IsOver {
				key = path + "|" + target
			}
			cells, exists := scores[key]
			if !exists {
				for _, point := range areas[target] {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					if scanned >= 100000 {
						a.Diagnostics = append(a.Diagnostics, Diagnostic{Severity: "error", Code: "spawn-budget", Message: "100000 scored-cell budget reached; remaining assessments incomplete"})
						success = true
						return a, nil
					}
					scanned++
					var cell ScoredCell
					if rule.IsOver {
						cell = s.scoreOver(point, rule)
					} else {
						cell = s.scoreSpawn(point, rule.Mode, rules)
					}
					cells = append(cells, cell)
				}
				scores[key] = cells
			}
			summary := TargetScore{Path: target, Cells: cells}
			for _, cell := range cells {
				if cell.Unknown {
					summary.Unknown++
				} else if cell.Score > 0 {
					summary.Eligible++
				} else {
					summary.Rejected++
				}
			}
			assessment.Targets = append(assessment.Targets, summary)
			if rule.IsOver {
				continue
			}
			if summary.Eligible > 0 {
				if !uncertainTarget {
					assessment.SelectedTarget = target
				}
				break
			}
			if summary.Unknown > 0 {
				uncertainTarget = true
			}
		}
		assessment.Status = "Authored candidates only. Shared runtime cache, prior spawns, Initialize, amount fulfillment and RNG remain unresolved."
		if rule.IsOver {
			assessment.Status = "Separate host-match pass; preceding area-spawn outputs are not embedded or predicted."
		}
		if mapName == "" {
			assessment.Status += " Map blacklist is unresolved until a map configuration is selected."
		}
		a.Spawns = append(a.Spawns, assessment)
	}
	success = true
	return a, nil
}
