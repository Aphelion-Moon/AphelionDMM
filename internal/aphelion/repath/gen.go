package repath

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"sdmm/internal/aphelion/repath/updatepaths"
)

type generator func(Entry, Sources) []Candidate

var generators = []generator{
	genScripts,
	genRemembered,
	genDirectional,
	genSuffix,
	genMetadata,
	genReferenceAncestor,
	genSimilarity,
	genAncestor,
}

func ruleCandidate(entry Entry, to string, vars map[string]string, tier Tier, score float64, generator string, reasons ...string) Candidate {
	return Candidate{
		Decision:  Decision{Kind: Apply, Via: ViaRule, Rule: RepathRule(entry.Path, to, vars), Summary: to},
		Tier:      tier,
		Score:     score,
		Generator: generator,
		Reasons:   reasons,
	}
}

func traceText(trace []updatepaths.Pos) string {
	parts := make([]string, 0, len(trace))
	for _, pos := range trace {
		parts = append(parts, pos.String())
	}
	return strings.Join(slices.Compact(parts), ", ")
}

func sample(entry Entry) updatepaths.Instance {
	instance := updatepaths.Instance{Path: entry.Path}
	if len(entry.Variants) != 0 {
		instance.Vars = entry.Variants[0].Vars
	}
	return instance
}

// genScripts follows UpdatePaths scripts. Stopping at the first path the
// environment knows is preferred; when running every rule ends elsewhere, both
// readings are offered and neither is certain.
func genScripts(entry Entry, s Sources) []Candidate {
	if s.Scripts.Len() == 0 {
		return nil
	}
	in := sample(entry)
	stop, trace := s.Scripts.Apply(in, s.Target.Exists)
	if len(trace) == 0 {
		return nil
	}
	full, fullTrace := s.Scripts.Apply(in, nil)
	if len(stop) == 0 {
		return []Candidate{{
			Decision:  Decision{Kind: Delete, Summary: "delete (script)"},
			Tier:      Medium,
			Score:     0.5,
			Generator: "scripts",
			Reasons:   []string{"UpdatePaths deletes this type: " + traceText(trace)},
		}}
	}
	same := len(stop) == len(full)
	for n := 0; same && n < len(stop); n++ {
		same = stop[n].Path == full[n].Path
	}
	reason := "UpdatePaths " + traceText(trace)
	if same {
		return []Candidate{{Decision: Decision{Kind: Apply, Via: ViaScripts, Summary: "codebase scripts"}, Tier: Certain, Score: 1, Generator: "scripts", Reasons: []string{reason}}}
	}
	result := []Candidate{{
		Decision: Decision{Kind: Apply, Via: ViaScripts, Summary: "codebase scripts (first known path)"}, Tier: High, Score: 0.9, Generator: "scripts",
		Reasons: []string{reason, "stops at the first path the environment defines; later scripts move it again"},
	}}
	if len(full) != 0 {
		result = append(result, Candidate{
			Decision: Decision{Kind: Apply, Via: ViaScriptsAll, Summary: "codebase scripts (every rule)"}, Tier: High, Score: 0.88, Generator: "scripts",
			Reasons: []string{"UpdatePaths " + traceText(fullTrace), "applies every later rule"},
		})
	}
	return result
}

func genRemembered(entry Entry, s Sources) []Candidate {
	if len(s.Remembered) == 0 {
		return nil
	}
	out, trace, err := updatepaths.ResolveChain(s.Remembered, sample(entry), s.Target.Exists)
	if err != nil {
		return []Candidate{{Decision: Decision{Kind: Apply, Via: ViaRemembered}, Tier: Low, Generator: "remembered", Reasons: []string{err.Error()}}}
	}
	if len(out) == 0 {
		return nil
	}
	return []Candidate{{
		Decision:  Decision{Kind: Apply, Via: ViaRemembered, Summary: "remembered decision"},
		Tier:      Certain,
		Score:     1,
		Generator: "remembered",
		Reasons:   []string{"remembered decision " + traceText(trace)},
	}}
}

var (
	directionalHelper = regexp.MustCompile(`^(/.+)/directional/(north|south|east|west|northeast|northwest|southeast|southwest)$`)
	directions        = map[string]string{"north": "1", "south": "2", "east": "4", "west": "8", "northeast": "5", "northwest": "9", "southeast": "6", "southwest": "10"}
	offsetVars        = []string{"pixel_x", "pixel_y", "pixel_w", "pixel_z"}
)

// genDirectional flattens /x/directional/<dir> mapping helpers to /x with an
// explicit dir. Helper offsets are known only from the source environment.
func genDirectional(entry Entry, s Sources) []Candidate {
	match := directionalHelper.FindStringSubmatch(entry.Path)
	if match == nil {
		return nil
	}
	family, dir := match[1], directions[match[2]]
	target, via, distant := family, "", false
	if !s.Target.Exists(family) {
		found := uniqueSuffix(family, s.Target)
		if found == "" {
			// A distinctive family name moved to another branch, e.g.
			// /obj/item/storage/secure/safe/caps_spare -> /obj/structure/secure_safe/caps_spare.
			if leaf := sameBaseOnly(family, s.Target.byLeaf[Leaf(family)], s.Target); len(leaf) == 1 {
				found, distant = leaf[0], true
			} else {
				return nil
			}
		}
		target, via = found, fmt.Sprintf("helper family %s matches %s; ", family, found)
	}
	vars := map[string]string{"dir": dir}
	tier, score, reason := Medium, 0.72, via+"directional helper flattened to dir "+dir+"; offsets are unknown without the source environment"
	if s.Reference.Exists(entry.Path) {
		for _, name := range append([]string{"dir"}, offsetVars...) {
			helper, ok := s.Reference.Value(entry.Path, name)
			base, _ := s.Reference.Value(family, name)
			if ok && helper != base {
				vars[name] = helper
			}
		}
		tier, score, reason = High, 0.9, via+"directional helper flattened with the source environment's dir and offsets"
		if via != "" {
			tier, score = Medium, 0.78
		}
	}
	if distant {
		tier, score = Low, 0.55
	}
	return []Candidate{ruleCandidate(entry, target, vars, tier, score, "directional", reason)}
}

func sameBaseOnly(path string, found []string, target *Index) []string {
	var result []string
	for _, candidate := range found {
		if candidate != path && SameBase(path, candidate) && target.Exists(candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

func uniqueSuffix(path string, target *Index) string {
	parts := segments(path)
	for _, length := range []int{3, 2} {
		if len(parts) > length {
			if found := sameBaseOnly(path, target.bySuffix[strings.Join(parts[len(parts)-length:], "/")], target); len(found) == 1 {
				return found[0]
			}
		}
	}
	return ""
}

// genSuffix finds types that were moved under a different parent but kept
// their trailing path segments.
func genSuffix(entry Entry, s Sources) []Candidate {
	parts := segments(entry.Path)
	var result []Candidate
	seen := map[string]bool{}
	add := func(found []string, tier Tier, score float64, reason string) {
		for _, path := range found {
			if !seen[path] {
				seen[path] = true
				result = append(result, ruleCandidate(entry, path, nil, tier, score+0.01*float64(commonPrefix(entry.Path, path)), "suffix", reason))
			}
		}
	}
	for _, length := range []int{3, 2} {
		if len(parts) <= length {
			continue
		}
		tail := strings.Join(parts[len(parts)-length:], "/")
		found := sameBaseOnly(entry.Path, s.Target.bySuffix[tail], s.Target)
		switch {
		case len(found) == 1:
			add(found, High, 0.86+0.02*float64(length-2), fmt.Sprintf("only type ending in %s", tail))
		case len(found) > 1 && len(found) <= 4:
			add(found, Medium, 0.6, fmt.Sprintf("one of %d types ending in %s", len(found), tail))
		}
	}
	found := sameBaseOnly(entry.Path, s.Target.byLeaf[parts[len(parts)-1]], s.Target)
	switch {
	case len(found) == 1 && commonPrefix(entry.Path, found[0]) >= len(parts)-1:
		// Moved within its own parent family, e.g. stamp/captain -> stamp/head/captain.
		add(found, Medium, 0.72, "only type named "+parts[len(parts)-1])
	case len(found) == 1 && commonPrefix(entry.Path, found[0]) >= 2:
		add(found, Low, 0.55, "only type named "+parts[len(parts)-1]+", in another branch")
	case len(found) > 1 && len(found) <= 5:
		add(found, Low, 0.5, fmt.Sprintf("one of %d types named %s", len(found), parts[len(parts)-1]))
	}
	return result
}

func commonPrefix(a, b string) int {
	x, y := segments(a), segments(b)
	n := 0
	for n < len(x) && n < len(y) && x[n] == y[n] {
		n++
	}
	return n
}

// genMetadata matches the source environment's appearance and name against
// the target environment.
func genMetadata(entry Entry, s Sources) []Candidate {
	if !s.Reference.Exists(entry.Path) {
		return nil
	}
	var result []Candidate
	name, hasName := s.Reference.text(entry.Path, "name")
	name = strings.ToLower(name)
	byName := map[string]bool{}
	if hasName && name != "" {
		for _, path := range sameBaseOnly(entry.Path, s.Target.byName[name], s.Target) {
			byName[path] = true
		}
	}
	if key, ok := s.Reference.iconKey(entry.Path); ok {
		found := sameBaseOnly(entry.Path, s.Target.byIcon[key], s.Target)
		if len(found) <= 6 {
			for _, path := range found {
				switch {
				case byName[path]:
					tier, score := High, 0.9
					if len(found) > 1 {
						score = 0.86
					}
					result = append(result, ruleCandidate(entry, path, nil, tier, score, "metadata", "same icon, icon_state and name in the source environment"))
				case len(found) == 1:
					result = append(result, ruleCandidate(entry, path, nil, Medium, 0.75, "metadata", "only type with the same icon and icon_state"))
				default:
					result = append(result, ruleCandidate(entry, path, nil, Low, 0.55, "metadata", fmt.Sprintf("one of %d types with the same icon and icon_state", len(found))))
				}
			}
		}
	}
	if len(byName) == 1 {
		for path := range byName {
			result = append(result, ruleCandidate(entry, path, nil, Low, 0.55, "metadata", "only type with the same name"))
		}
	}
	return result
}

var appearanceVars = []string{"name", "desc", "icon", "icon_state", "dir", "pixel_x", "pixel_y", "color"}

// genReferenceAncestor approximates a source type as its nearest target
// ancestor carrying the source type's appearance.
func genReferenceAncestor(entry Entry, s Sources) []Candidate {
	if !s.Reference.Exists(entry.Path) {
		return nil
	}
	ancestor := s.Target.NearestAncestor(entry.Path)
	if ancestor == "" || Parent(ancestor) == "" {
		return nil
	}
	vars := map[string]string{}
	for _, name := range appearanceVars {
		value, ok := s.Reference.Value(entry.Path, name)
		current, _ := s.Target.Value(ancestor, name)
		if ok && value != current {
			vars[name] = value
		}
	}
	return []Candidate{ruleCandidate(entry, ancestor, vars, Low, 0.5, "reference",
		"approximation: nearest defined parent with the source type's appearance; behaviour of the subtype is lost")}
}

// genSimilarity offers loosely related names with shared rare leaf tokens.
func genSimilarity(entry Entry, s Sources) []Candidate {
	leaf := Leaf(entry.Path)
	words := tokens(leaf)
	pool := map[string]bool{}
	for _, word := range words {
		if found := s.Target.byToken[word]; len(found) <= 200 {
			for _, path := range sameBaseOnly(entry.Path, found, s.Target) {
				pool[path] = true
			}
		}
	}
	type scored struct {
		path  string
		score float64
	}
	var ranked []scored
	for path := range pool {
		// A shared word across unrelated branches (machinery vs item) is noise.
		if commonPrefix(entry.Path, path) < 2 {
			continue
		}
		score := 0.35*jaccard(words, tokens(Leaf(path))) + 0.25*similarity(tail(entry.Path, 2), tail(path, 2))
		if score >= 0.3 {
			ranked = append(ranked, scored{path, score})
		}
	}
	slices.SortFunc(ranked, func(a, b scored) int {
		if a.score != b.score {
			if a.score > b.score {
				return -1
			}
			return 1
		}
		return strings.Compare(a.path, b.path)
	})
	var result []Candidate
	for n := 0; n < len(ranked) && n < 5; n++ {
		result = append(result, ruleCandidate(entry, ranked[n].path, nil, Low, ranked[n].score, "similar", "similar name"))
	}
	return result
}

func tail(path string, n int) string {
	parts := segments(path)
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return strings.Join(parts, "/")
}

func jaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for _, word := range a {
		if slices.Contains(b, word) {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}

// similarity is one minus the normalized Levenshtein distance.
func similarity(a, b string) float64 {
	if a == b {
		return 1
	}
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return 1 - float64(previous[len(b)])/float64(max(len(a), len(b)))
}

// genAncestor is the last resort: the nearest defined parent type.
func genAncestor(entry Entry, s Sources) []Candidate {
	ancestor := s.Target.NearestAncestor(entry.Path)
	if ancestor == "" {
		return nil
	}
	return []Candidate{ruleCandidate(entry, ancestor, nil, Lossy, 0.3, "parent", "nearest defined parent; the subtype is lost")}
}
