package repath

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"sdmm/internal/aphelion/repath/updatepaths"
)

// Settings is serialized by the user preferences store. Nothing is written
// anywhere unless a person enables it or asks for it.
type Settings struct {
	OpenOnUnknown bool
	// Auto is one of AutoModes; see Level.
	Auto string
	// ApplyCertainOnOpen applies Certain selections as one undoable edit when
	// a map opens.
	ApplyCertainOnOpen  bool
	ReadCodebaseScripts bool
	// RememberDecisions offers to keep a person's decisions per environment.
	RememberDecisions bool
	// WriteCodebaseScripts allows saving scripts into the codebase's
	// tools/UpdatePaths/Scripts directory.
	WriteCodebaseScripts  bool
	RememberReferencePath bool
}

func DefaultSettings() Settings {
	return Settings{OpenOnUnknown: true, Auto: AutoModeCertain, ReadCodebaseScripts: true}
}

const (
	AutoModeOff     = "Off"
	AutoModeCertain = "Certain only"
	AutoModeHigh    = "Certain and High"
)

var AutoModes = []string{AutoModeOff, AutoModeCertain, AutoModeHigh}

// Level maps the stored mode to a policy; unknown values select Certain only.
func (s Settings) Level() AutoLevel {
	switch s.Auto {
	case AutoModeOff:
		return AutoOff
	case AutoModeHigh:
		return AutoHigh
	}
	return AutoCertain
}

const memoryVersion = 1

// Memory holds remembered decisions per environment, keyed by EnvironmentKey.
type Memory struct {
	Version      int
	Environments map[string]EnvironmentMemory
}

type EnvironmentMemory struct {
	Rules     []string // canonical rule lines
	Reference string   // last reference .dme, when remembered
}

func NewMemory() *Memory {
	return &Memory{Version: memoryVersion, Environments: map[string]EnvironmentMemory{}}
}

func EnvironmentKey(rootFile string) string {
	if absolute, err := filepath.Abs(rootFile); err == nil {
		rootFile = absolute
	}
	rootFile = filepath.Clean(rootFile)
	if runtime.GOOS == "windows" {
		rootFile = strings.ToLower(rootFile)
	}
	return rootFile
}

func DecodeMemory(data []byte) (*Memory, error) {
	memory := NewMemory()
	if len(data) == 0 {
		return memory, nil
	}
	if err := json.Unmarshal(data, memory); err != nil {
		return nil, err
	}
	if memory.Version != memoryVersion {
		return nil, fmt.Errorf("unsupported path migration memory version %d", memory.Version)
	}
	if memory.Environments == nil {
		memory.Environments = map[string]EnvironmentMemory{}
	}
	return memory, nil
}

func (m *Memory) Encode() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// Rules parses an environment's remembered rules; malformed lines are skipped
// and reported.
func (m *Memory) Rules(key string) ([]updatepaths.Rule, []error) {
	var rules []updatepaths.Rule
	var errs []error
	for n, line := range m.Environments[key].Rules {
		rule, err := updatepaths.ParseRule(line)
		if err != nil {
			errs = append(errs, fmt.Errorf("remembered rule %d: %w", n+1, err))
			continue
		}
		rule.Pos = updatepaths.Pos{File: "remembered", Line: n + 1}
		rules = append(rules, rule)
	}
	return rules, errs
}

// Remember stores a person's explicit rules from the plan. A new rule for the
// same old path and filters replaces the previous one.
func (m *Memory) Remember(key string, plan Plan) int {
	rules, _ := plan.Rules()
	environment := m.Environments[key]
	added := 0
	for _, rule := range rules {
		line := rule.String()
		match, _, _ := strings.Cut(line, " : ")
		environment.Rules = slices.DeleteFunc(environment.Rules, func(existing string) bool {
			old, _, _ := strings.Cut(existing, " : ")
			return old == match
		})
		environment.Rules = append(environment.Rules, line)
		added++
	}
	slices.Sort(environment.Rules)
	m.Environments[key] = environment
	return added
}

func (m *Memory) Forget(key string) {
	delete(m.Environments, key)
}
