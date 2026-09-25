package mapping

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
)

const SelectorVersion = 1

type Choice struct {
	Slot          int
	CandidateHash string
}
type Scenario struct {
	Version                   int
	BaseHash, EnvironmentHash string
	Choices                   map[string]Choice
	Excluded                  map[string]bool
}
type Placement struct {
	Root            Root
	Slot            int
	Source          Identity
	Transform       Transform
	Phase, LoadMode string
}
type Contribution struct {
	Source                    string
	Local                     util.Point
	Occurrence, Phase, Reason string
}
type CellProvenance struct {
	Turf, Area *Contribution
	Objects    []Contribution
	Suppressed []Contribution
	ReservedBy string
}
type FixedPlacement struct {
	ID        string
	Source    *Source
	Transform Transform
}
type Projection struct {
	Source      *Source
	Placements  []Placement
	Roots       []Root
	Diagnostics []Diagnostic
	Provenance  map[util.Point]*CellProvenance
	Scenario    Scenario
}

func (p *Projection) Close() {
	if p != nil && p.Source != nil {
		p.Source.Close()
	}
}

func applyCell(base, incoming []Atom) []Atom {
	result := append([]Atom(nil), base...)
	for _, a := range incoming {
		c := channel(a)
		if a.Path == "/turf/template_noop" || a.Path == "/area/template_noop" {
			continue
		}
		if c != "objects" {
			result = slices.DeleteFunc(result, func(old Atom) bool { return channel(old) == c })
		}
		result = append(result, a)
	}
	return result
}

// Compose applies fixed reservations before discovering eligible base/modular
// roots. Ordering among independent asynchronous module loads is only an editor
// scenario; conflicts retain witnesses instead of claiming a runtime outcome.
func (c *Catalog) Compose(ctx context.Context, base *Source, scenario Scenario, fixed []FixedPlacement) (*Projection, error) {
	if scenario.Version != 0 && scenario.Version != SelectorVersion {
		return nil, fmt.Errorf("unsupported scenario selector version")
	}
	if scenario.BaseHash != "" && scenario.BaseHash != base.Identity.ContentHash {
		return nil, fmt.Errorf("scenario base content changed; choices require explicit revalidation")
	}
	if scenario.EnvironmentHash != "" && scenario.EnvironmentHash != base.Identity.EnvironmentHash {
		return nil, fmt.Errorf("scenario environment changed")
	}
	scenario.Version = SelectorVersion
	scenario.BaseHash = base.Identity.ContentHash
	scenario.EnvironmentHash = base.Identity.EnvironmentHash
	scenario.Choices = maps.Clone(scenario.Choices)
	p := &Projection{Scenario: scenario, Provenance: make(map[util.Point]*CellProvenance)}
	// Reserve the bounded result before allocation. Source/catalogue and graphics
	// reservations remain independent and live until their respective owners close.
	lease, err := resources.DefaultBudget().Reserve(uint64(len(base.grid))*768 + 128<<20)
	if err != nil {
		return nil, err
	}
	published := false
	defer func() {
		if !published {
			lease.Release()
		}
	}()
	cells := make(map[util.Point][]Atom, len(base.grid))
	reserved := make(map[util.Point]string)
	inside := func(point util.Point) bool {
		return point.X >= 1 && point.Y >= 1 && point.Z >= 1 && point.X <= base.Size.X && point.Y <= base.Size.Y && point.Z <= base.Size.Z
	}
	diagnosticKeys := make(map[string]int)
	diagnose := func(severity, code, message, source, occurrence string, local, destination util.Point) {
		key := severity + "|" + code + "|" + message + "|" + source + "|" + occurrence
		if index, ok := diagnosticKeys[key]; ok {
			p.Diagnostics[index].Count++
			return
		}
		if len(p.Diagnostics) >= 2047 {
			if len(p.Diagnostics) == 2047 {
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Severity: "error", Code: "diagnostic-budget", Message: "Diagnostic limit reached; this scenario is incomplete"})
			}
			return
		}
		diagnosticKeys[key] = len(p.Diagnostics)
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Severity: severity, Code: code, Message: message, Source: source, Occurrence: occurrence, Local: local, Destination: destination, Count: 1})
	}
	for _, f := range fixed {
		for _, local := range f.Source.Points() {
			destination := f.Transform.Apply(local)
			if !inside(destination) {
				diagnose("error", "bounds", "Fixed template extends outside selected map", f.Source.Identity.Path, f.ID, local, destination)
				continue
			}
			for _, atom := range f.Source.atomsAt(local) {
				if channel(atom) == "turf" && atom.Path != "/turf/template_noop" {
					if old := reserved[destination]; old != "" {
						diagnose("warning", "reservation-overlap", "Two selected fixed reservations overlap: "+old, f.Source.Identity.Path, f.ID, local, destination)
					}
					reserved[destination] = f.ID
					break
				}
			}
		}
	}
	provenance := func(point util.Point) *CellProvenance {
		value := p.Provenance[point]
		if value == nil {
			value = &CellProvenance{ReservedBy: reserved[point]}
			p.Provenance[point] = value
		}
		return value
	}
	for _, point := range base.Points() {
		record := provenance(point)
		origin := Contribution{Source: base.Identity.Path, Local: point, Phase: "base"}
		if reserved[point] != "" {
			origin.Reason = "Base cell skipped by fixed-template reservation"
			record.Suppressed = append(record.Suppressed, origin)
			continue
		}
		cells[point] = base.atomsAt(point)
		for _, atom := range cells[point] {
			switch channel(atom) {
			case "turf":
				copy := origin
				record.Turf = &copy
			case "area":
				copy := origin
				record.Area = &copy
			default:
				record.Objects = append(record.Objects, origin)
			}
		}
	}
	roots, diagnostics := c.Roots(ctx, base, Transform{}, "")
	for _, d := range diagnostics {
		diagnose(d.Severity, d.Code, d.Message, d.Source, d.Occurrence, d.Local, d.Destination)
	}
	type pending struct {
		root     Root
		ancestry []string
		depth    int
	}
	queue := make([]pending, 0, len(roots))
	for _, root := range roots {
		queue = append(queue, pending{root: root})
	}
	seenPins := make(map[string]bool)
	contributed := 0
	contributedAtoms := 0
	var applyErr error
	apply := func(source *Source, transform Transform, occurrence, phase string, blacklist bool) {
		for _, local := range source.Points() {
			if applyErr != nil {
				return
			}
			if applyErr = ctx.Err(); applyErr != nil {
				return
			}
			destination := transform.Apply(local)
			if !inside(destination) {
				diagnose("error", "bounds", "Template contribution outside selected map", source.Identity.Path, occurrence, local, destination)
				continue
			}
			record := provenance(destination)
			origin := Contribution{Source: source.Identity.Path, Local: local, Occurrence: occurrence, Phase: phase}
			if blacklist && reserved[destination] != "" {
				origin.Reason = "Cell skipped by fixed-template reservation"
				record.Suppressed = append(record.Suppressed, origin)
				continue
			}
			if contributed >= 1_000_000 {
				diagnose("error", "cell-budget", "Contributed-cell budget exhausted", source.Identity.Path, occurrence, local, destination)
				return
			}
			contributed++
			incoming := source.atomsAt(local)
			contributedAtoms += len(incoming)
			if contributedAtoms > 4_000_000 {
				applyErr = fmt.Errorf("composition exceeds four million contributed atoms")
				return
			}
			if contributed%256 == 1 {
				applyErr = lease.Resize(uint64(len(base.grid))*768 + 128<<20 + uint64(contributedAtoms)*160)
				if applyErr != nil {
					return
				}
			}
			for _, a := range incoming {
				if a.Path == "/turf/template_noop" || a.Path == "/area/template_noop" {
					continue
				}
				switch channel(a) {
				case "turf":
					if record.Turf != nil && record.Turf.Phase == phase && record.Turf.Occurrence != occurrence {
						diagnose("warning", "order-uncertain", "Jointly selected sibling turf loads overlap; asynchronous order is unresolved", source.Identity.Path, occurrence, local, destination)
					}
					copy := origin
					record.Turf = &copy
				case "area":
					if record.Area != nil && record.Area.Phase == phase && record.Area.Occurrence != occurrence {
						diagnose("warning", "order-uncertain", "Jointly selected sibling area loads overlap; asynchronous order is unresolved", source.Identity.Path, occurrence, local, destination)
					}
					copy := origin
					record.Area = &copy
				default:
					record.Objects = append(record.Objects, origin)
				}
			}
			cells[destination] = applyCell(cells[destination], incoming)
			if record.Area == nil && reserved[destination] != "" {
				diagnose("warning", "substrate-unknown", "Reserved cell has no authored area; area-noop does not inherit skipped base area", source.Identity.Path, occurrence, local, destination)
			}
		}
	}
	for len(queue) > 0 && len(p.Roots) < 4096 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := queue[0]
		queue[0] = pending{}
		queue = queue[1:]
		root := item.root
		p.Roots = append(p.Roots, root)
		if reserved[root.Destination] != "" {
			diagnose("info", "root-suppressed", "Root never loads because its cell is reserved", root.Source.Path, root.ID, root.Local, root.Destination)
			continue
		}
		if scenario.Excluded[root.ID] {
			diagnose("info", "what-if", "Root excluded by explicit editor what-if scenario", root.Source.Path, root.ID, root.Local, root.Destination)
			continue
		}
		binding := root.Config + "#" + root.Key
		if item.depth >= 32 || slices.Contains(item.ancestry, binding) {
			diagnose("error", "cycle", "Active modular ancestry cycles or exceeds 32 levels", root.Source.Path, root.ID, root.Local, root.Destination)
			continue
		}
		choice, pinned := scenario.Choices[root.ID]
		if pinned {
			seenPins[root.ID] = true
		}
		if choice.Slot < 0 || choice.Slot >= len(root.Candidates) {
			diagnose("error", "candidate", "Selected slot is absent or invalid", root.Source.Path, root.ID, root.Local, root.Destination)
			continue
		}
		candidate := root.Candidates[choice.Slot]
		if candidate.Error != "" {
			diagnose("error", "missing-source", candidate.Error, candidate.Path, root.ID, root.Local, root.Destination)
			continue
		}
		source, err := c.Load(ctx, candidate.Path)
		if err != nil {
			diagnose("error", "source", err.Error(), candidate.Path, root.ID, root.Local, root.Destination)
			continue
		}
		if choice.CandidateHash != "" && choice.CandidateHash != source.Identity.ContentHash {
			diagnose("error", "stale-pin", "Pinned source content changed", candidate.Path, root.ID, root.Local, root.Destination)
			continue
		}
		if pinned {
			choice.CandidateHash = source.Identity.ContentHash
			scenario.Choices[root.ID] = choice
		}
		transform, err := Anchor(source, root.Destination)
		if err != nil {
			diagnose("error", "anchor", err.Error(), candidate.Path, root.ID, root.Local, root.Destination)
			continue
		}
		p.Placements = append(p.Placements, Placement{Root: root, Slot: choice.Slot, Source: source.Identity, Transform: transform, Phase: "modular", LoadMode: "place-on-top"})
		apply(source, transform, root.ID, "modular", true)
		if applyErr != nil {
			return nil, applyErr
		}
		children, childDiagnostics := c.Roots(ctx, source, transform, root.ID)
		for _, d := range childDiagnostics {
			diagnose(d.Severity, d.Code, d.Message, d.Source, d.Occurrence, d.Local, d.Destination)
		}
		ancestry := append(append([]string(nil), item.ancestry...), binding)
		for _, child := range children {
			if len(queue)+len(p.Roots) >= 4096 {
				diagnose("error", "occurrence-budget", "Expansion exceeds 4096 root occurrences", source.Identity.Path, root.ID, root.Local, root.Destination)
				break
			}
			queue = append(queue, pending{root: child, ancestry: ancestry, depth: item.depth + 1})
		}
	}
	if len(queue) > 0 {
		diagnose("error", "occurrence-budget", "Expansion exceeds 4096 root occurrences", base.Identity.Path, "", util.Point{}, util.Point{})
	}
	for id := range scenario.Choices {
		if !seenPins[id] {
			diagnose("warning", "orphan-pin", "Explicit choice has no eligible occurrence in this scenario", base.Identity.Path, id, util.Point{}, util.Point{})
		}
	}
	for _, f := range fixed {
		apply(f.Source, f.Transform, f.ID, "fixed", false)
	}
	if applyErr != nil {
		return nil, applyErr
	}
	// Include every selected source and transform, including default choices.
	// Selector JSON alone does not identify the resulting authored cells.
	type fixedIdentity struct {
		ID, Hash  string
		Transform Transform
	}
	fixedIDs := make([]fixedIdentity, 0, len(fixed))
	for _, f := range fixed {
		fixedIDs = append(fixedIDs, fixedIdentity{f.ID, f.Source.Identity.ContentHash, f.Transform})
	}
	type placementIdentity struct {
		ID, Hash  string
		Transform Transform
	}
	placementIDs := make([]placementIdentity, 0, len(p.Placements))
	for _, placed := range p.Placements {
		placementIDs = append(placementIDs, placementIdentity{placed.Root.ID, placed.Source.ContentHash, placed.Transform})
	}
	encoded, _ := json.Marshal(struct {
		Scenario Scenario
		Fixed    []fixedIdentity
		Modules  []placementIdentity
	}{scenario, fixedIDs, placementIDs})
	identity := base.Identity
	identity.ContentHash = fmt.Sprintf("scenario:%x", sha256.Sum256(encoded))
	identity.DocumentID = "editor-scenario"
	identity.Path = ""
	source := &Source{Identity: identity, Size: base.Size, Origin: util.Point{X: 1, Y: 1, Z: 1}, grid: make(map[util.Point]dmmdata.Key, len(cells)), dictionary: make(map[dmmdata.Key][]Atom, len(cells)), lease: lease}
	for point, atoms := range cells {
		key := dmmdata.Key(strconv.Itoa(len(source.dictionary)))
		source.grid[point] = key
		source.dictionary[key] = atoms
	}
	p.Source = source
	p.Scenario = scenario
	diagnose("info", "static-projection", "Authored content only; baseturf stacks, Initialize effects, and async ordering are not reconstructed", base.Identity.Path, "", util.Point{}, util.Point{})
	published = true
	return p, nil
}
