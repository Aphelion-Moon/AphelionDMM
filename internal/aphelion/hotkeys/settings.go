package hotkeys

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// Settings owns custom chords. Missing actions retain every registered default;
// an explicitly empty list disables an action. Serialization can run on the
// application's preference-saving goroutine while the UI changes bindings.
type Settings struct {
	mu       sync.RWMutex
	bindings map[string][][][2]glfw.Key
}

func cloneChords(chords [][][2]glfw.Key) [][][2]glfw.Key {
	result := make([][][2]glfw.Key, len(chords))
	for i, chord := range chords {
		result[i] = append([][2]glfw.Key(nil), chord...)
	}
	return result
}

func (s *Settings) Lookup(name string) ([][][2]glfw.Key, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chords, ok := s.bindings[name]
	return cloneChords(chords), ok
}

// Match returns the length of the most specific matching chord, or -1. The
// dispatch path reads custom chords under the lock without copying each frame.
func (s *Settings) Match(name string, defaults [][2]glfw.Key, down, pressed func(glfw.Key) bool) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chords, ok := s.bindings[name]
	if !ok {
		if Pressed(defaults, down, pressed) {
			return len(defaults)
		}
		return -1
	}
	weight := -1
	for _, chord := range chords {
		if len(chord) > weight && Pressed(chord, down, pressed) {
			weight = len(chord)
		}
	}
	return weight
}

func (s *Settings) Set(name string, chords [][][2]glfw.Key) error {
	if name == "" {
		return fmt.Errorf("choose an action")
	}
	if err := validateChords(chords); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bindings == nil {
		s.bindings = make(map[string][][][2]glfw.Key)
	}
	s.bindings[name] = cloneChords(chords)
	return nil
}

func (s *Settings) Reset(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bindings, name)
}

func (s *Settings) ResetAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings = nil
}

func (s *Settings) MarshalJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return json.Marshal(s.bindings)
}

func (s *Settings) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		s.ResetAll()
		return nil
	}
	bindings := make(map[string][][][2]glfw.Key)
	for name, data := range raw {
		var chords [][][2]glfw.Key
		// Ignore only invalid entries so an old or hand-edited preference cannot
		// disable defaults. Keep unknown names: their panes may not be open yet.
		if name != "" && json.Unmarshal(data, &chords) == nil && validateChords(chords) == nil {
			bindings[name] = chords
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings = bindings
	return nil
}
