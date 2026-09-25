package filterprofiles

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
)

const (
	StoreVersion    = 1
	DocumentVersion = 1
	DocumentFormat  = "aphelion-filter-profile"
	MaxDocumentSize = 1 << 20
)

type Scope string

const (
	ScopeExact   Scope = "exact"
	ScopeSubtree Scope = "subtree"
)

type Rule struct {
	Path    string `json:"path"`
	Scope   Scope  `json:"scope"`
	Visible bool   `json:"visible"`
}

type Profile struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DefaultVisible bool   `json:"default_visible"`
	Rules          []Rule `json:"rules"`
}

type Store struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

type Overrides struct {
	ReplaceProfile bool   `json:"-"`
	DefaultVisible *bool  `json:"-"`
	Rules          []Rule `json:"-"`
}

type Warning struct {
	Path   string
	Source string
	Reason string
}

type Compiled struct {
	hiddenDescendants map[string]int
	HiddenPaths       []string
	Warnings          []Warning
}

type document struct {
	Format  string  `json:"format"`
	Version int     `json:"version"`
	Profile Profile `json:"profile"`
}

func NewStore() Store { return Store{Version: StoreVersion} }

func NewID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func Clone(profile Profile) Profile {
	profile.Rules = append([]Rule(nil), profile.Rules...)
	return profile
}

func Builtins() []Profile {
	return []Profile{
		{ID: "builtin:piping-atmospherics", Name: "Piping and Atmospherics", DefaultVisible: false, Rules: []Rule{{Path: "/obj/machinery/atmospherics", Scope: ScopeSubtree, Visible: true}}},
		{ID: "builtin:wiring-power", Name: "Wiring and Power", DefaultVisible: false, Rules: []Rule{{Path: "/obj/structure/cable", Scope: ScopeSubtree, Visible: true}, {Path: "/obj/machinery/power", Scope: ScopeSubtree, Visible: true}}},
		{ID: "builtin:disposal", Name: "Disposal Systems", DefaultVisible: false, Rules: []Rule{{Path: "/obj/structure/disposalpipe", Scope: ScopeSubtree, Visible: true}, {Path: "/obj/machinery/disposal", Scope: ScopeSubtree, Visible: true}, {Path: "/obj/structure/disposaloutlet", Scope: ScopeSubtree, Visible: true}}},
	}
}

func Builtin(id string) (Profile, bool) {
	for _, profile := range Builtins() {
		if profile.ID == id {
			return profile, true
		}
	}
	return Profile{}, false
}

func IsBuiltin(profile Profile) bool { return strings.HasPrefix(profile.ID, "builtin:") }

func DefaultProfile() Profile {
	return Profile{ID: "session:visible", Name: "Current visibility", DefaultVisible: true}
}

func Validate(profile Profile) error {
	if strings.TrimSpace(profile.ID) == "" || len(profile.ID) > 128 {
		return errors.New("profile ID is empty or too long")
	}
	name := strings.TrimSpace(profile.Name)
	if name == "" || len(name) > 64 {
		return errors.New("profile name must contain 1 to 64 characters")
	}
	return validateRules(profile.Rules)
}

func validateRules(rules []Rule) error {
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		if rule.Path == "" || rule.Path[0] != '/' || strings.Contains(rule.Path, "//") || len(rule.Path) > 1024 {
			return fmt.Errorf("invalid type path %q", rule.Path)
		}
		if rule.Scope != ScopeExact && rule.Scope != ScopeSubtree {
			return fmt.Errorf("invalid rule scope %q for %s", rule.Scope, rule.Path)
		}
		key := rule.Path + "\x00" + string(rule.Scope)
		if seen[key] {
			return fmt.Errorf("duplicate %s rule for %s", rule.Scope, rule.Path)
		}
		seen[key] = true
	}
	return nil
}

func (s *Store) Add(profile Profile) error {
	if s.Version == 0 {
		s.Version = StoreVersion
	}
	if s.Version != StoreVersion {
		return fmt.Errorf("unsupported profile store version %d", s.Version)
	}
	if IsBuiltin(profile) || strings.HasPrefix(profile.ID, "session:") {
		return errors.New("built-in and session profiles cannot be saved")
	}
	profile.Name = strings.TrimSpace(profile.Name)
	if err := Validate(profile); err != nil {
		return err
	}
	for i, existing := range s.Profiles {
		if existing.ID == profile.ID {
			if err := s.validateName(profile.Name, profile.ID); err != nil {
				return err
			}
			s.Profiles[i] = Clone(profile)
			return nil
		}
	}
	if err := s.validateName(profile.Name, ""); err != nil {
		return err
	}
	s.Profiles = append(s.Profiles, Clone(profile))
	return nil
}

func (s *Store) validateName(name, exceptID string) error {
	for _, builtin := range Builtins() {
		if strings.EqualFold(strings.TrimSpace(name), builtin.Name) {
			return fmt.Errorf("%q is reserved for a built-in profile", name)
		}
	}
	for _, existing := range s.Profiles {
		if existing.ID != exceptID && strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(existing.Name)) {
			return fmt.Errorf("a profile named %q already exists", name)
		}
	}
	return nil
}

func (s *Store) Rename(id, name string) error {
	for i := range s.Profiles {
		if s.Profiles[i].ID == id {
			name = strings.TrimSpace(name)
			if name == "" || len(name) > 64 {
				return errors.New("profile name must contain 1 to 64 characters")
			}
			if err := s.validateName(name, id); err != nil {
				return err
			}
			s.Profiles[i].Name = name
			return nil
		}
	}
	return fmt.Errorf("custom profile %q was not found", id)
}

func (s *Store) Delete(id string) error {
	for i := range s.Profiles {
		if s.Profiles[i].ID == id {
			s.Profiles = append(s.Profiles[:i], s.Profiles[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("custom profile %q was not found", id)
}

func (s Store) Find(id string) (Profile, bool) {
	if profile, ok := Builtin(id); ok {
		return profile, true
	}
	for _, profile := range s.Profiles {
		if profile.ID == id {
			return Clone(profile), true
		}
	}
	return Profile{}, false
}

func (s Store) Validate() error {
	if s.Version != StoreVersion {
		return fmt.Errorf("unsupported profile store version %d", s.Version)
	}
	seenIDs := make(map[string]bool, len(s.Profiles))
	seenNames := make(map[string]bool, len(s.Profiles))
	for _, profile := range s.Profiles {
		if IsBuiltin(profile) || strings.HasPrefix(profile.ID, "session:") {
			return fmt.Errorf("reserved profile ID %q cannot be stored", profile.ID)
		}
		if err := Validate(profile); err != nil {
			return err
		}
		name := strings.ToLower(strings.TrimSpace(profile.Name))
		if seenIDs[profile.ID] || seenNames[name] {
			return errors.New("profile store contains duplicate IDs or names")
		}
		seenIDs[profile.ID], seenNames[name] = true, true
	}
	return nil
}

func Duplicate(source Profile, id, name string) (Profile, error) {
	clone := Clone(source)
	clone.ID, clone.Name = id, strings.TrimSpace(name)
	if IsBuiltin(clone) {
		return Profile{}, errors.New("duplicate must receive a custom ID")
	}
	if strings.HasPrefix(clone.ID, "session:") {
		return Profile{}, errors.New("duplicate must receive a custom ID")
	}
	if err := Validate(clone); err != nil {
		return Profile{}, err
	}
	return clone, nil
}

func Encode(profile Profile) ([]byte, error) {
	if err := Validate(profile); err != nil {
		return nil, err
	}
	return json.MarshalIndent(document{Format: DocumentFormat, Version: DocumentVersion, Profile: Clone(profile)}, "", "  ")
}

func Decode(data []byte) (Profile, error) {
	if len(data) == 0 || len(data) > MaxDocumentSize {
		return Profile{}, fmt.Errorf("profile document must be between 1 and %d bytes", MaxDocumentSize)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var doc document
	if err := decoder.Decode(&doc); err != nil {
		return Profile{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Profile{}, errors.New("profile document has trailing data")
	}
	if doc.Format != DocumentFormat || doc.Version != DocumentVersion {
		return Profile{}, fmt.Errorf("unsupported filter profile document %q version %d", doc.Format, doc.Version)
	}
	if err := Validate(doc.Profile); err != nil {
		return Profile{}, err
	}
	return Clone(doc.Profile), nil
}

func Compile(profile Profile, overrides Overrides, environment *dmenv.Dme) (Compiled, error) {
	catalog, err := NewCatalog(environment)
	if err != nil {
		return Compiled{}, err
	}
	return catalog.Compile(profile, overrides)
}

// Catalog captures immutable browse ancestry once per environment generation.
// It deliberately does not use effective parent_type inheritance for filtering.
type Catalog struct {
	parents     map[string]string
	paths       []string
	present     map[string]bool
	descendants map[string]int
}

func NewCatalog(environment *dmenv.Dme) (*Catalog, error) {
	if environment == nil {
		return nil, errors.New("no project type catalog is loaded")
	}

	parents := make(map[string]string, len(environment.Objects))
	for parentPath, parent := range environment.Objects {
		if parent == nil {
			continue
		}
		for _, childPath := range parent.DirectChildren {
			if child := environment.Objects[childPath]; child != nil {
				if previous, exists := parents[childPath]; exists && previous != parentPath {
					return nil, fmt.Errorf("type %s has multiple declared parents", childPath)
				}
				parents[childPath] = parentPath
			}
		}
	}
	catalog := &Catalog{parents: parents, present: make(map[string]bool, len(environment.Objects)), descendants: make(map[string]int)}
	for path, object := range environment.Objects {
		if object != nil {
			catalog.paths = append(catalog.paths, path)
			catalog.present[path] = true
			for ancestor := path; ; {
				separator := strings.LastIndexByte(ancestor, '/')
				if separator <= 0 {
					break
				}
				ancestor = ancestor[:separator]
				catalog.descendants[ancestor]++
			}
		}
	}
	sort.Strings(catalog.paths)
	return catalog, nil
}

func (c *Catalog) Compile(profile Profile, overrides Overrides) (Compiled, error) {
	if err := Validate(profile); err != nil {
		return Compiled{}, err
	}
	if err := validateRules(overrides.Rules); err != nil {
		return Compiled{}, fmt.Errorf("invalid temporary overrides: %w", err)
	}
	parents := c.parents

	baseRules := profile.Rules
	defaultVisible := profile.DefaultVisible
	if overrides.ReplaceProfile {
		baseRules = nil
		defaultVisible = true
	}
	if overrides.DefaultVisible != nil {
		defaultVisible = *overrides.DefaultVisible
	}
	warnings := make([]Warning, 0)
	for _, entry := range []struct {
		rules  []Rule
		source string
	}{{baseRules, "profile"}, {overrides.Rules, "temporary override"}} {
		for _, rule := range entry.rules {
			if !c.present[rule.Path] {
				warnings = append(warnings, Warning{Path: rule.Path, Source: entry.source, Reason: "type is not present in the loaded project; rule is retained"})
			}
		}
	}

	// Index rules once; a large saved policy must not require types x rules
	// comparisons or repeated ancestry allocations during compilation.
	exact, subtree := map[string]bool{}, map[string]bool{}
	for _, rules := range [][]Rule{baseRules, overrides.Rules} {
		for _, rule := range rules {
			if rule.Scope == ScopeExact {
				exact[rule.Path] = rule.Visible
			} else {
				subtree[rule.Path] = rule.Visible
			}
		}
	}
	hidden := make([]string, 0)
	for path, visible := range exact {
		if !visible && !c.present[path] {
			hidden = append(hidden, path)
		}
	}
	sort.Strings(hidden)
	for _, path := range c.paths {
		visible := defaultVisible
		if value, ok := exact[path]; ok {
			visible = value
		} else {
			for current, depth := path, 0; current != ""; current, depth = parents[current], depth+1 {
				if depth > len(parents) {
					return Compiled{}, errors.New("type catalog contains an ancestry cycle")
				}
				if value, ok := subtree[current]; ok {
					visible = value
					break
				}
			}
		}
		if !visible {
			hidden = append(hidden, path)
		}
	}
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].Path != warnings[j].Path {
			return warnings[i].Path < warnings[j].Path
		}
		return warnings[i].Source < warnings[j].Source
	})
	counts := make(map[string]int)
	for _, path := range hidden {
		if !c.present[path] {
			continue
		}
		for ancestor := path; ; {
			separator := strings.LastIndexByte(ancestor, '/')
			if separator <= 0 {
				break
			}
			ancestor = ancestor[:separator]
			counts[ancestor]++
		}
	}
	return Compiled{HiddenPaths: hidden, Warnings: warnings, hiddenDescendants: counts}, nil
}

func MergeOverrides(profile Profile, overrides Overrides) (Profile, error) {
	merged := Clone(profile)
	if overrides.ReplaceProfile {
		merged.Rules = nil
		merged.DefaultVisible = true
	}
	if overrides.DefaultVisible != nil {
		merged.DefaultVisible = *overrides.DefaultVisible
	}
	rules := make(map[string]Rule, len(merged.Rules)+len(overrides.Rules))
	for _, rule := range merged.Rules {
		rules[rule.Path+"\x00"+string(rule.Scope)] = rule
	}
	for _, rule := range overrides.Rules {
		rules[rule.Path+"\x00"+string(rule.Scope)] = rule
	}
	merged.Rules = merged.Rules[:0]
	for _, rule := range rules {
		merged.Rules = append(merged.Rules, rule)
	}
	sort.Slice(merged.Rules, func(i, j int) bool {
		if merged.Rules[i].Path != merged.Rules[j].Path {
			return merged.Rules[i].Path < merged.Rules[j].Path
		}
		return merged.Rules[i].Scope < merged.Rules[j].Scope
	})
	if err := Validate(merged); err != nil {
		return Profile{}, err
	}
	return merged, nil
}

func cloneOverrides(overrides Overrides) Overrides {
	copy := Overrides{ReplaceProfile: overrides.ReplaceProfile, Rules: append([]Rule(nil), overrides.Rules...)}
	if overrides.DefaultVisible != nil {
		visible := *overrides.DefaultVisible
		copy.DefaultVisible = &visible
	}
	return copy
}

func (o *Overrides) setRule(rule Rule) {
	for i, existing := range o.Rules {
		if existing.Path == rule.Path && existing.Scope == rule.Scope {
			o.Rules[i] = rule
			return
		}
	}
	o.Rules = append(o.Rules, rule)
}

type Session struct {
	active         *Profile
	overrides      Overrides
	lastHidden     *Rule
	warnings       []Warning
	catalog        *Catalog
	environment    *dmenv.Dme
	effective      Compiled
	history        []visibilityState
	historyCursor  int
	historyBytes   int
	historyTrimmed bool
}

// Fork captures owned session state for one unpublished worker. The catalogue
// is immutable; overrides and user-visible metadata must not alias a live owner.
func (s Session) Fork() Session {
	if s.active != nil {
		p := Clone(*s.active)
		s.active = &p
	}
	s.overrides = cloneOverrides(s.overrides)
	if s.lastHidden != nil {
		r := *s.lastHidden
		s.lastHidden = &r
	}
	s.warnings = append([]Warning(nil), s.warnings...)
	s.history = append([]visibilityState(nil), s.history...)
	return s
}

func (s *Session) compile(profile Profile, overrides Overrides, environment *dmenv.Dme) (Compiled, error) {
	if s.catalog == nil || s.environment != environment {
		catalog, err := NewCatalog(environment)
		if err != nil {
			return Compiled{}, err
		}
		s.catalog, s.environment = catalog, environment
	}
	return s.catalog.Compile(profile, overrides)
}

func (s *Session) Active() (Profile, bool) {
	if s.active == nil {
		return Profile{}, false
	}
	return Clone(*s.active), true
}

func (s *Session) OverridesDirty() bool {
	return s.active != nil && (s.overrides.ReplaceProfile || s.overrides.DefaultVisible != nil || len(s.overrides.Rules) != 0)
}

func (s *Session) Warnings() []Warning { return append([]Warning(nil), s.warnings...) }

func (s *Session) DescendantCount(path string) int {
	if s.catalog == nil {
		return 0
	}
	return s.catalog.descendants[path]
}

func (s *Session) HiddenDescendantCount(path string) int { return s.effective.hiddenDescendants[path] }

func (s *Session) LastHidden() (Rule, bool) {
	if s.lastHidden == nil {
		return Rule{}, false
	}
	return *s.lastHidden, true
}

func (s *Session) Apply(profile Profile, environment *dmenv.Dme, filter *dm.PathsFilter) error {
	compiled, err := s.compile(profile, Overrides{}, environment)
	if err != nil {
		return err
	}
	filter.ApplyHiddenPaths(compiled.HiddenPaths)
	copy := Clone(profile)
	s.active = &copy
	s.overrides = Overrides{}
	s.lastHidden = nil
	s.warnings = compiled.Warnings
	s.effective = compiled
	s.recordVisibility("Apply " + profile.Name)
	return nil
}

func (s *Session) SetVisibility(path string, scope Scope, visible bool, environment *dmenv.Dme, filter *dm.PathsFilter) error {
	profile := DefaultProfile()
	if s.active != nil {
		profile = Clone(*s.active)
	}
	next := cloneOverrides(s.overrides)
	rule := Rule{Path: path, Scope: scope, Visible: visible}
	next.setRule(rule)
	compiled, err := s.compile(profile, next, environment)
	if err != nil {
		return err
	}
	filter.ApplyHiddenPaths(compiled.HiddenPaths)
	s.overrides = next
	s.warnings = compiled.Warnings
	if !visible && !slices.Equal(s.effective.HiddenPaths, compiled.HiddenPaths) {
		last := rule
		s.lastHidden = &last
	}
	s.effective = compiled
	s.recordVisibility(fmt.Sprintf("%s %s: %s", map[bool]string{true: "Show", false: "Hide"}[visible], scope, path))
	return nil
}

func (s *Session) ShowAll(environment *dmenv.Dme, filter *dm.PathsFilter) error {
	profile := DefaultProfile()
	if s.active != nil {
		profile = Clone(*s.active)
	}
	next := Overrides{ReplaceProfile: true}
	visible := true
	next.DefaultVisible = &visible
	compiled, err := s.compile(profile, next, environment)
	if err != nil {
		return err
	}
	filter.ApplyHiddenPaths(compiled.HiddenPaths)
	s.overrides, s.warnings = next, compiled.Warnings
	s.effective = compiled
	s.recordVisibility("Show All")
	return nil
}

func (s *Session) Reset(environment *dmenv.Dme, filter *dm.PathsFilter) error {
	profile := DefaultProfile()
	if s.active != nil {
		profile = Clone(*s.active)
	}
	compiled, err := s.compile(profile, Overrides{}, environment)
	if err != nil {
		return err
	}
	filter.ApplyHiddenPaths(compiled.HiddenPaths)
	s.overrides, s.warnings, s.lastHidden = Overrides{}, compiled.Warnings, nil
	s.effective = compiled
	s.recordVisibility("Reset")
	return nil
}

func (s *Session) UnhideLast(environment *dmenv.Dme, filter *dm.PathsFilter) error {
	if s.lastHidden == nil {
		return errors.New("there is no recently hidden type")
	}
	return s.SetVisibility(s.lastHidden.Path, s.lastHidden.Scope, true, environment, filter)
}

func (s *Session) SaveCurrent(id, name string) (Profile, error) {
	base := DefaultProfile()
	if s.active != nil {
		base = Clone(*s.active)
	}
	merged, err := MergeOverrides(base, s.overrides)
	if err != nil {
		return Profile{}, err
	}
	merged.ID, merged.Name = id, strings.TrimSpace(name)
	if err := Validate(merged); err != nil {
		return Profile{}, err
	}
	return merged, nil
}
