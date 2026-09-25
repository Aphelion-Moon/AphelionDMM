package filterprofiles

import (
	"errors"
	"reflect"

	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
)

// History retains policy descriptions, never map snapshots or compiled catalogues.
// Both bounds apply so large imported profiles cannot multiply without limit.
const maxVisibilityHistoryEntries = 256
const maxVisibilityHistoryBytes = 4 << 20

type visibilityState struct {
	profile    Profile
	overrides  Overrides
	lastHidden *Rule
	label      string
	bytes      int
}

type HistoryStatus struct {
	Position, Count int
	Label           string
	Trimmed         bool
}

func (s *Session) HistoryStatus() HistoryStatus {
	status := HistoryStatus{Position: s.historyCursor + 1, Count: len(s.history), Trimmed: s.historyTrimmed}
	if len(s.history) != 0 {
		status.Label = s.history[s.historyCursor].label
	} else {
		status.Position = 0
	}
	return status
}

func (s *Session) recordVisibility(label string) {
	profile := DefaultProfile()
	if s.active != nil {
		profile = Clone(*s.active)
	}
	next := visibilityState{profile: profile, overrides: cloneOverrides(s.overrides), label: label}
	if s.lastHidden != nil {
		r := *s.lastHidden
		next.lastHidden = &r
	}
	if len(s.history) != 0 {
		current := s.history[s.historyCursor]
		if reflect.DeepEqual(current.profile, next.profile) && reflect.DeepEqual(current.overrides, next.overrides) {
			return
		}
		for _, state := range s.history[s.historyCursor+1:] {
			s.historyBytes -= state.bytes
		}
		clear(s.history[s.historyCursor+1:])
		s.history = s.history[:s.historyCursor+1]
	}
	next.bytes = 256 + len(profile.ID) + len(profile.Name) + len(label)
	for _, rules := range [][]Rule{profile.Rules, next.overrides.Rules} {
		for _, rule := range rules {
			next.bytes += 64 + len(rule.Path) + len(rule.Scope)
		}
	}
	s.history = append(s.history, next)
	s.historyBytes += next.bytes
	for len(s.history) > 1 && (len(s.history) > maxVisibilityHistoryEntries || s.historyBytes > maxVisibilityHistoryBytes) {
		s.historyBytes -= s.history[0].bytes
		s.history[0] = visibilityState{}
		s.history = s.history[1:]
		s.historyTrimmed = true
	}
	// A single oversized state is usable but not retained in navigation history.
	if s.historyBytes > maxVisibilityHistoryBytes {
		s.history = nil
		s.historyBytes = 0
		s.historyTrimmed = true
	}
	s.historyCursor = max(0, len(s.history)-1)
}

func (s *Session) UndoVisibility(environment *dmenv.Dme, filter *dm.PathsFilter) error {
	return s.restoreVisibility(s.historyCursor-1, environment, filter)
}
func (s *Session) RedoVisibility(environment *dmenv.Dme, filter *dm.PathsFilter) error {
	return s.restoreVisibility(s.historyCursor+1, environment, filter)
}
func (s *Session) restoreVisibility(index int, environment *dmenv.Dme, filter *dm.PathsFilter) error {
	if index < 0 || index >= len(s.history) {
		return errors.New("no further visibility history in that direction")
	}
	state := s.history[index]
	compiled, err := s.compile(state.profile, state.overrides, environment)
	if err != nil {
		return err
	}
	filter.ApplyHiddenPaths(compiled.HiddenPaths)
	profile := Clone(state.profile)
	s.active, s.overrides, s.lastHidden = &profile, cloneOverrides(state.overrides), state.lastHidden
	s.warnings, s.effective = compiled.Warnings, compiled
	s.historyCursor = index
	return nil
}
